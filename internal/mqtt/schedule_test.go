package mqtt

import "testing"

// A week where every day carries the same programme should read as one period,
// not as seven repetitions. That is the case on this installation for all three
// circuits, and publishing seven identical entities would be noise.
func TestSummariseWeek_UniformWeek(t *testing.T) {
	var week [7]string
	for i := range week {
		week[i] = "05:30-22:00"
	}
	if got := summariseWeek(week); got != "05:30-22:00" {
		t.Errorf("summariseWeek = %q, want the bare period", got)
	}
}

// A week that is not uniform must say so, or collapsing would hide a weekend
// programme.
func TestSummariseWeek_WeekendDiffers(t *testing.T) {
	week := [7]string{
		"06:00-22:00", "06:00-22:00", "06:00-22:00", "06:00-22:00", "06:00-22:00",
		"08:00-23:00", "08:00-23:00",
	}
	want := "Lu-Ve 06:00-22:00 · Sa-Di 08:00-23:00"
	if got := summariseWeek(week); got != want {
		t.Errorf("summariseWeek = %q, want %q", got, want)
	}
}

func TestSummariseWeek_SingleDayDiffers(t *testing.T) {
	week := [7]string{
		"06:00-22:00", "06:00-22:00", "off all day", "06:00-22:00", "06:00-22:00",
		"06:00-22:00", "06:00-22:00",
	}
	want := "Lu-Ma 06:00-22:00 · Me off all day · Je-Di 06:00-22:00"
	if got := summariseWeek(week); got != want {
		t.Errorf("summariseWeek = %q, want %q", got, want)
	}
}

// The summary must wait until every day is known: publishing a partial week
// would show a programme the appliance does not have.
func TestScheduleState_WaitsForTheWholeWeek(t *testing.T) {
	state := newScheduleState()

	for day := 0; day < 6; day++ {
		if _, complete := state.record("scheduleZirku", day, "05:30-22:00"); complete {
			t.Fatalf("reported complete after %d days", day+1)
		}
	}
	week, complete := state.record("scheduleZirku", 6, "05:30-22:00")
	if !complete {
		t.Fatal("should be complete once all seven days are known")
	}
	if got := summariseWeek(week); got != "05:30-22:00" {
		t.Errorf("summary = %q", got)
	}
}

// A schedule changes twice a year; republishing it on every six-hourly read
// would be noise.
func TestScheduleState_PublishesOnlyOnChange(t *testing.T) {
	state := newScheduleState()

	if !state.changed("scheduleZirku", "05:30-22:00") {
		t.Error("the first summary must be published")
	}
	if state.changed("scheduleZirku", "05:30-22:00") {
		t.Error("an unchanged summary must not be republished")
	}
	if !state.changed("scheduleZirku", "06:30-21:00") {
		t.Error("a changed summary must be published")
	}
}
