// Package mqtt - the ledger: readings that may not have reached Home Assistant.
package mqtt

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/vcontrold-go/internal/logging"
)

// A reading published over MQTT is timestamped by Home Assistant when it
// arrives, not when it was measured. Queuing messages during an outage and
// sending them on reconnection would therefore stack hours of readings onto a
// single instant: worse than a gap, because it looks like data.
//
// What survives an outage is a timestamped record kept here, from which the
// hourly long-term statistics can be rebuilt afterwards
// (scripts/ha_backfill_ledger.py). The ledger costs nothing while delivery
// works: readings only sit in memory. It writes to disk only once delivery has
// failed for a while.
const (
	// ledgerWindow is how far back the memory reaches. A connection can be
	// dead before anyone notices: MQTT only detects it after the keep-alive
	// and ping timeouts, and QoS 0 publishes into a dead socket report
	// success. When the failure is finally seen, the last hour is written out
	// too, so those silently lost readings are not lost.
	ledgerWindow = time.Hour

	// ledgerGrace is how long delivery must fail before the ledger spills to
	// disk. It keeps the start-up seconds before the first connection, and a
	// reconnection blip, from leaving a file behind every time.
	ledgerGrace = time.Minute

	// ledgerRetention bounds how long spilled files are kept.
	ledgerRetention = 90 * 24 * time.Hour
)

// ledgerEntry is one successful reading.
type ledgerEntry struct {
	Time    time.Time
	Address uint16 // zero for values computed by the daemon, such as run time
	Alias   string
	Value   string // as published: cleaned of units and annotations
}

// ledger keeps the last hour of readings in memory and writes them to disk
// while they cannot be delivered.
type ledger struct {
	mutex sync.Mutex
	dir   string

	recent []ledgerEntry

	// undeliveredSince is when delivery started failing, zero while it works.
	undeliveredSince time.Time
	spilling         bool
	spilled          int

	// lastWritten is the time of the newest entry on disk, so a second outage
	// within the hour does not write the same readings twice.
	lastWritten time.Time

	file    *os.File
	fileDay string
}

func newLedger(dir string) *ledger {
	return &ledger{dir: dir}
}

// record adds a reading. delivered says whether it reached Home Assistant, as
// far as the daemon can tell.
func (l *ledger) record(e ledgerEntry, delivered bool) {
	if l == nil {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.recent = append(l.recent, e)
	l.prune(e.Time)

	if delivered {
		if l.spilling {
			logging.Warn("Ledger: delivery is back; %d reading(s) since %s are in %s, run scripts/ha_backfill_ledger.py",
				l.spilled, l.undeliveredSince.Format("2006-01-02 15:04:05"), l.dir)
			l.closeFile()
		}
		l.undeliveredSince = time.Time{}
		l.spilling = false
		l.spilled = 0
		return
	}

	if l.undeliveredSince.IsZero() {
		l.undeliveredSince = e.Time
	}
	if !l.spilling {
		if e.Time.Sub(l.undeliveredSince) < ledgerGrace {
			return
		}
		l.spilling = true
		logging.Warn("Ledger: readings not delivered since %s; writing them to %s, with the hour before",
			l.undeliveredSince.Format("2006-01-02 15:04:05"), l.dir)
		l.removeExpired(e.Time)
		for _, past := range l.recent {
			l.write(past)
		}
		return
	}
	l.write(e)
}

// prune drops what has left the window.
func (l *ledger) prune(now time.Time) {
	cut := 0
	for cut < len(l.recent) && now.Sub(l.recent[cut].Time) >= ledgerWindow {
		cut++
	}
	if cut > 0 {
		l.recent = append(l.recent[:0], l.recent[cut:]...)
	}
}

// write appends one entry to the day's file, unless it is already on disk.
func (l *ledger) write(e ledgerEntry) {
	if !e.Time.After(l.lastWritten) {
		return
	}

	day := e.Time.Format("2006-01-02")
	if l.file == nil || l.fileDay != day {
		l.closeFile()
		if err := os.MkdirAll(l.dir, 0o755); err != nil {
			logging.Error("Ledger: cannot create %s: %v", l.dir, err)
			return
		}
		path := filepath.Join(l.dir, day+".csv")
		_, statErr := os.Stat(path)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			logging.Error("Ledger: cannot open %s: %v", path, err)
			return
		}
		l.file, l.fileDay = f, day
		if os.IsNotExist(statErr) {
			l.writeRow([]string{"time", "address", "alias", "value"})
		}
	}

	address := ""
	if e.Address != 0 {
		address = fmt.Sprintf("0x%04X", e.Address)
	}
	if l.writeRow([]string{e.Time.Format(time.RFC3339), address, e.Alias, e.Value}) {
		l.lastWritten = e.Time
		l.spilled++
	}
}

// writeRow writes one CSV line straight to the file. No buffering: a power cut
// during an outage should cost seconds of readings, not minutes.
func (l *ledger) writeRow(fields []string) bool {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write(fields); err != nil {
		logging.Error("Ledger: cannot encode a row: %v", err)
		return false
	}
	w.Flush()
	if _, err := l.file.WriteString(b.String()); err != nil {
		logging.Error("Ledger: cannot write to %s: %v", l.file.Name(), err)
		return false
	}
	return true
}

func (l *ledger) closeFile() {
	if l.file != nil {
		l.file.Close()
		l.file, l.fileDay = nil, ""
	}
}

// removeExpired deletes day files older than the retention.
func (l *ledger) removeExpired(now time.Time) {
	files, err := filepath.Glob(filepath.Join(l.dir, "????-??-??.csv"))
	if err != nil {
		return
	}
	sort.Strings(files)
	limit := now.Add(-ledgerRetention).Format("2006-01-02")
	for _, f := range files {
		if strings.TrimSuffix(filepath.Base(f), ".csv") < limit {
			if err := os.Remove(f); err == nil {
				logging.Info("Ledger: removed %s, older than %s", f, ledgerRetention)
			}
		}
	}
}

// close releases the file, if one is open.
func (l *ledger) close() {
	if l == nil {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.closeFile()
}

// status reports the ledger's state for the web API.
func (l *ledger) status() map[string]interface{} {
	if l == nil {
		return nil
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	s := map[string]interface{}{
		"dir":      l.dir,
		"inMemory": len(l.recent),
		"spilling": l.spilling,
	}
	if !l.undeliveredSince.IsZero() {
		s["undeliveredSince"] = l.undeliveredSince.Format(time.RFC3339)
	}
	return s
}

// DefaultLedgerDir places the ledger beside the configuration, like the
// run-time state file.
func DefaultLedgerDir(configPath string) string {
	if configPath == "" {
		return "ledger"
	}
	return filepath.Join(filepath.Dir(configPath), "ledger")
}
