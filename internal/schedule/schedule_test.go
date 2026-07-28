package schedule

import (
	"fmt"
	"testing"
	"time"
)

// tick returns now at hour:minute on a fixed day.
func tick(hour, min int) time.Time {
	return time.Date(2026, 7, 28, hour, min, 0, 0, time.Local)
}

func TestDueEmpty(t *testing.T) {
	due, err := Due("", tick(3, 0), 5*time.Minute)
	if err != nil || !due {
		t.Fatalf("empty schedule must always be due, got due=%v err=%v", due, err)
	}
}

// Regression: an HH:MM job must fire exactly once — on the tick at (or just
// after) the scheduled time, never one tick early and never on the next tick.
func TestDueHHMMRunsExactlyOnce(t *testing.T) {
	window := 5 * time.Minute
	var runs []string
	for m := 2*60 + 40; m <= 3*60+20; m += 5 { // ticks 02:40 .. 03:20
		now := tick(m/60, m%60)
		due, err := Due("03:00", now, window)
		if err != nil {
			t.Fatal(err)
		}
		if due {
			runs = append(runs, now.Format("15:04"))
		}
	}
	if len(runs) != 1 || runs[0] != "03:00" {
		t.Fatalf("schedule 03:00 must fire exactly once at 03:00, fired at %v", runs)
	}
}

func TestDueHHMMWindow(t *testing.T) {
	window := 5 * time.Minute
	cases := []struct {
		now  string
		want bool
	}{
		{"02:55", false}, // one tick early — must NOT run
		{"03:00", true},  // on time
		{"03:01", true},  // late tick (systemd accuracy) still inside window
		{"03:04", true},  // last minute of the window
		{"03:05", false}, // next tick — activation belongs to the 03:00 tick
		{"03:10", false},
	}
	for _, c := range cases {
		var h, m int
		if _, err := fmt.Sscanf(c.now, "%d:%d", &h, &m); err != nil {
			t.Fatal(err)
		}
		due, err := Due("03:00", tick(h, m), window)
		if err != nil {
			t.Fatal(err)
		}
		if due != c.want {
			t.Errorf("Due(03:00) at %s = %v, want %v", c.now, due, c.want)
		}
	}
}

// Midnight wrap-around: a job scheduled at 23:55 is due on the 23:55 tick,
// and a job at 00:00 on the midnight tick — without double-firing.
func TestDueHHMMMidnight(t *testing.T) {
	window := 5 * time.Minute
	at := func(day, h, m int) time.Time { return time.Date(2026, 7, day, h, m, 0, 0, time.Local) }

	due, _ := Due("23:55", at(28, 23, 55), window)
	if !due {
		t.Error("23:55 job must be due at 23:55")
	}
	due, _ = Due("23:55", at(29, 0, 0), window)
	if due {
		t.Error("23:55 job must NOT fire again at 00:00 (previous window boundary)")
	}
	due, _ = Due("00:00", at(29, 0, 0), window)
	if !due {
		t.Error("00:00 job must be due at midnight")
	}
	due, _ = Due("00:00", at(28, 23, 55), window)
	if due {
		t.Error("00:00 job must NOT fire one tick early at 23:55")
	}
}

// Regression: a cron schedule must fire on exactly one tick as well.
func TestDueCronRunsExactlyOnce(t *testing.T) {
	window := 5 * time.Minute
	var runs []string
	for m := 2*60 + 40; m <= 3*60+20; m += 5 {
		now := tick(m/60, m%60)
		due, err := Due("0 3 * * *", now, window)
		if err != nil {
			t.Fatal(err)
		}
		if due {
			runs = append(runs, now.Format("15:04"))
		}
	}
	if len(runs) != 1 || runs[0] != "03:00" {
		t.Fatalf("cron 0 3 * * * must fire exactly once at 03:00, fired at %v", runs)
	}
}

func TestDueCronLateTick(t *testing.T) {
	// systemd timers may fire up to a minute late (default AccuracySec=1min).
	due, err := Due("0 3 * * *", tick(3, 1), 5*time.Minute)
	if err != nil || !due {
		t.Fatalf("late tick at 03:01 must still see the 03:00 activation, got due=%v err=%v", due, err)
	}
	due, err = Due("0 3 * * *", tick(3, 5), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Error("03:05 tick must not re-run the 03:00 activation")
	}
}

func TestDueInvalid(t *testing.T) {
	if _, err := Due("not a schedule", tick(3, 0), 5*time.Minute); err == nil {
		t.Fatal("invalid schedule must return an error")
	}
	if Valid("not a schedule") {
		t.Fatal("Valid must reject garbage")
	}
	if !Valid("03:00") || !Valid("0 3 * * 0") || !Valid("") {
		t.Fatal("Valid must accept HH:MM, 5-field cron and empty")
	}
}
