package mqtt

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readLedger(t *testing.T, dir, day string) [][]string {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, day+".csv"))
	if err != nil {
		t.Fatalf("ledger file for %s: %v", day, err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("ledger file for %s is not valid CSV: %v", day, err)
	}
	return rows
}

func entryAt(at time.Time, value string) ledgerEntry {
	return ledgerEntry{Time: at, Address: 0x0802, Alias: "getTempKist", Value: value}
}

// While delivery works the ledger must not touch the disk: the target runs on
// an SD card, and the normal case is that Home Assistant has everything.
func TestLedger_WritesNothingWhileDelivered(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)

	for i := 0; i < 120; i++ {
		l.record(entryAt(start.Add(time.Duration(i)*time.Minute), "55.0"), true)
	}

	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 0 {
		t.Errorf("delivered readings were written to disk: %v", files)
	}
	if len(l.recent) > 61 {
		t.Errorf("memory holds %d entries, want at most an hour's worth", len(l.recent))
	}
}

// A connection can be dead for a minute or two before MQTT notices, and QoS 0
// publishes into it report success. When the failure is seen, the hour before
// must reach the disk too, or those readings are silently lost.
func TestLedger_SpillsTheHourBeforeOnceDeliveryFails(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)

	// Ninety minutes delivered: only the last hour stays in memory.
	for i := 0; i < 90; i++ {
		l.record(entryAt(start.Add(time.Duration(i)*time.Minute), "55.0"), true)
	}
	// Then three minutes undelivered.
	for i := 90; i < 93; i++ {
		l.record(entryAt(start.Add(time.Duration(i)*time.Minute), "56.0"), false)
	}

	rows := readLedger(t, dir, "2026-11-17")
	if got := rows[0]; got[0] != "time" || got[3] != "value" {
		t.Fatalf("missing header, first row is %v", got)
	}
	data := rows[1:]
	first, _ := time.Parse(time.RFC3339, data[0][0])
	if want := start.Add(32 * time.Minute); !first.Equal(want) {
		t.Errorf("first spilled reading at %s, want %s (one hour before the last)", first, want)
	}
	if len(data) != 61 {
		t.Errorf("spilled %d readings, want 61 (the hour before plus the outage)", len(data))
	}
	if data[0][1] != "0x0802" || data[0][2] != "getTempKist" {
		t.Errorf("row %v does not carry the address and alias", data[0])
	}
}

// Start-up and a reconnection blip both leave a few undelivered readings.
// Writing a file for each would bury the real outages.
func TestLedger_ShortFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)

	l.record(entryAt(start, "55.0"), false)
	l.record(entryAt(start.Add(30*time.Second), "55.1"), false)
	l.record(entryAt(start.Add(time.Minute-time.Second), "55.2"), false)
	l.record(entryAt(start.Add(time.Minute), "55.3"), true)

	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 0 {
		t.Errorf("a failure shorter than the grace period left %v", files)
	}
}

// Two outages less than an hour apart share part of the window. The second
// must not write the readings the first already put on disk.
func TestLedger_SecondOutageDoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)
	minute := func(i int) time.Time { return start.Add(time.Duration(i) * time.Minute) }

	for i := 0; i < 5; i++ {
		l.record(entryAt(minute(i), "55.0"), false)
	}
	for i := 5; i < 10; i++ {
		l.record(entryAt(minute(i), "55.0"), true)
	}
	for i := 10; i < 15; i++ {
		l.record(entryAt(minute(i), "55.0"), false)
	}

	seen := map[string]bool{}
	for _, row := range readLedger(t, dir, "2026-11-17")[1:] {
		if seen[row[0]] {
			t.Errorf("reading at %s written twice", row[0])
		}
		seen[row[0]] = true
	}
	if len(seen) != 15 {
		t.Errorf("%d distinct readings on disk, want all 15", len(seen))
	}
}

// One file per day, so a backfill can be run over just the days concerned.
func TestLedger_RollsOverAtMidnight(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 23, 58, 0, 0, time.Local)

	for i := 0; i < 5; i++ {
		l.record(entryAt(start.Add(time.Duration(i)*time.Minute), "55.0"), false)
	}

	if rows := readLedger(t, dir, "2026-11-17"); len(rows) != 3 {
		t.Errorf("2026-11-17 holds %d rows, want header + 2", len(rows))
	}
	if rows := readLedger(t, dir, "2026-11-18"); len(rows) != 4 || rows[0][0] != "time" {
		t.Errorf("2026-11-18 holds %v, want its own header + 3 rows", rows)
	}
}

// Values computed by the daemon, such as pump run time, have no address.
func TestLedger_ComputedValueHasEmptyAddress(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)

	for i := 0; i < 3; i++ {
		l.record(ledgerEntry{Time: start.Add(time.Duration(i) * time.Minute), Alias: "pumpM2Hours", Value: "657.4"}, false)
	}

	row := readLedger(t, dir, "2026-11-17")[1]
	if row[1] != "" || row[2] != "pumpM2Hours" {
		t.Errorf("computed value written as %v, want an empty address", row)
	}
}

func TestLedger_RemovesFilesPastRetention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "2026-01-01.csv")
	recent := filepath.Join(dir, "2026-11-01.csv")
	for _, f := range []string{old, recent} {
		if err := os.WriteFile(f, []byte("time,address,alias,value\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	l := newLedger(dir)
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		l.record(entryAt(start.Add(time.Duration(i)*time.Minute), "55.0"), false)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a file older than the retention was kept")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("a recent file was removed: %v", err)
	}
}

// A publisher built without a ledger must still work.
func TestLedger_NilIsANoOp(t *testing.T) {
	var l *ledger
	l.record(entryAt(time.Now(), "55.0"), false)
	l.close()
	if l.status() != nil {
		t.Error("a nil ledger reported a status")
	}
}
