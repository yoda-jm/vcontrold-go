// Package mqtt - weekly schedules as a single entity per circuit.
package mqtt

import (
	"strings"
	"sync"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
)

// dayNames labels the seven addresses of a weekly schedule, in the order the
// appliance stores them.
var dayNames = [7]string{"Lu", "Ma", "Me", "Je", "Ve", "Sa", "Di"}

// scheduleState holds the per-day values of each configured schedule.
//
// A circuit's programme is seven addresses, and on this installation all seven
// carry the same period. Publishing them as seven entities would put seven
// identical values in front of someone who wants to know one thing: when the
// circuit is allowed to run.
type scheduleState struct {
	mutex sync.Mutex

	// days[alias][index] is the decoded schedule for one weekday.
	days map[string][7]string

	// published holds the last summary sent per alias.
	published map[string]string
}

func newScheduleState() *scheduleState {
	return &scheduleState{
		days:      make(map[string][7]string),
		published: make(map[string]string),
	}
}

// record stores one day's schedule and reports whether the week is complete.
func (s *scheduleState) record(alias string, index int, value string) ([7]string, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	week := s.days[alias]
	week[index] = value
	s.days[alias] = week

	for _, day := range week {
		if day == "" {
			return week, false
		}
	}
	return week, true
}

// changed reports whether a summary differs from the last published one.
func (s *scheduleState) changed(alias, summary string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if previous, ok := s.published[alias]; ok && previous == summary {
		return false
	}
	s.published[alias] = summary
	return true
}

// forget drops what was published, so everything goes out again after a
// reconnection.
func (s *scheduleState) forget() {
	s.mutex.Lock()
	s.published = make(map[string]string)
	s.mutex.Unlock()
}

// completeWeeks returns the schedules whose seven days are all known.
func (s *scheduleState) completeWeeks() map[string][7]string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	weeks := make(map[string][7]string)
	for alias, week := range s.days {
		complete := true
		for _, day := range week {
			complete = complete && day != ""
		}
		if complete {
			weeks[alias] = week
		}
	}
	return weeks
}

// summariseWeek renders seven daily schedules as one line.
//
// Consecutive days sharing a programme are collapsed into a range, so a week
// that is the same throughout reads as one period rather than seven repetitions
// — while a week where the weekend differs still says so.
func summariseWeek(week [7]string) string {
	var parts []string
	start := 0

	for day := 1; day <= 7; day++ {
		if day < 7 && week[day] == week[start] {
			continue
		}

		label := dayNames[start]
		if day-1 > start {
			label += "-" + dayNames[day-1]
		}
		parts = append(parts, label+" "+week[start])
		start = day
	}

	// A single group means the whole week shares one programme.
	if len(parts) == 1 {
		return week[0]
	}
	return strings.Join(parts, " · ")
}

// PublishScheduleDiscovery announces one entity per weekly schedule.
func (p *Publisher) PublishScheduleDiscovery(schedules []config.MQTTScheduleConfig) error {
	if len(schedules) == 0 {
		return nil
	}

	// Configured whether or not the broker answers, so the days read while it
	// does not are not lost.
	p.scheduleConfigs = schedules
	if p.schedules == nil {
		p.schedules = newScheduleState()
	}

	// Index the addresses so a reading can be attributed to its weekday.
	index := make(map[uint16]scheduleSlot)
	for _, sched := range schedules {
		for day, address := range sched.Addresses {
			if day >= len(dayNames) {
				logging.Warn("Schedule %q lists more than seven days; the extra ones are ignored", sched.Alias)
				break
			}
			hexAddr, err := config.ParseHexAddress(address)
			if err != nil {
				logging.Warn("Schedule %q: invalid address %q: %v", sched.Alias, address, err)
				continue
			}
			index[hexAddr] = scheduleSlot{alias: sched.Alias, day: day}
		}
	}
	p.scheduleIndex = index

	if err := p.readyToDiscover(); err != nil {
		if err == errDiscoveryDisabled || err == errNotConnected {
			return nil
		}
		return err
	}

	for _, sched := range schedules {
		objectID := sanitizeObjectID(sched.Alias)
		if objectID == "" {
			continue
		}
		displayName := sched.FriendlyName
		if displayName == "" {
			displayName = sched.Alias
		}

		payload := sensorPayload{
			entityBase: newEntityBase(p, objectID, displayName, sched.Icon),
			StateTopic: p.stateTopic(sched.Alias),
			// Text: no state_class. The historisable number is the measured run
			// time, which is worth more than the programmed hours.
		}
		p.publishDiscovery("sensor", objectID, payload)
	}

	return nil
}

// scheduleSlot ties an address to its place in a weekly schedule.
type scheduleSlot struct {
	alias string
	day   int
}

// handleSchedule records one day's programme and publishes the week once every
// day is known.
func (p *Publisher) handleSchedule(address uint16, value string) {
	if p.schedules == nil || len(p.scheduleIndex) == 0 {
		return
	}

	slot, tracked := p.scheduleIndex[address]
	if !tracked {
		return
	}

	week, complete := p.schedules.record(slot.alias, slot.day, value)
	if !complete {
		return
	}
	p.publishSchedule(slot.alias, week)
}

// republishSchedules sends every complete week again after a reconnection.
func (p *Publisher) republishSchedules() {
	if p.schedules == nil {
		return
	}
	p.schedules.forget()
	for alias, week := range p.schedules.completeWeeks() {
		p.publishSchedule(alias, week)
	}
}

// publishSchedule sends one week's summary if it changed since it was last
// sent.
func (p *Publisher) publishSchedule(alias string, week [7]string) {
	// Checked before changed(), which would otherwise record as sent a
	// summary that never left.
	if !p.online() {
		return
	}

	summary := summariseWeek(week)
	if !p.schedules.changed(alias, summary) {
		return
	}

	topic := p.stateTopic(alias)
	if token := p.client.Publish(topic, 0, true, summary); token.Wait() && token.Error() != nil {
		logging.Error("Failed to publish schedule %s: %v", topic, token.Error())
		return
	}
	logging.Info("SCHEDULE: %s = %s", topic, summary)
}
