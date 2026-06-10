package engine

import "testing"

func TestCalendarShapeFallsBackToDefault(t *testing.T) {
	// shapeFor consults gametime.GetCalendar; unknown names fall back to the
	// "default" calendar, and a world with no calendars yields (0, 0) so the
	// caller disables seasons.
	months, days := shapeFor("no-such-calendar")
	defMonths, defDays := shapeFor("default")
	if months != defMonths || days != defDays {
		t.Errorf("unknown calendar should fall back to default: got (%d,%d) want (%d,%d)",
			months, days, defMonths, defDays)
	}
}
