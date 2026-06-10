package engine

import (
	"github.com/GoMudEngine/GoMud/internal/gametime"
	"github.com/GoMudEngine/GoMud/modules/weather/seasons"
)

// shapeFor returns (monthsPerYear, daysPerYear) for a named calendar, falling
// back to the "default" calendar, then to (0, 0) — the caller treats a zero
// shape as "no usable calendar" and disables seasons (fail-soft).
func shapeFor(name string) (int, int) {
	if cfg, ok := gametime.GetCalendar(name); ok && len(cfg.Months) > 0 && cfg.DaysPerYear > 0 {
		return len(cfg.Months), cfg.DaysPerYear
	}
	if name != `default` {
		if cfg, ok := gametime.GetCalendar(`default`); ok && len(cfg.Months) > 0 && cfg.DaysPerYear > 0 {
			return len(cfg.Months), cfg.DaysPerYear
		}
	}
	return 0, 0
}

// CalendarShape reports the active calendar's (monthsPerYear, daysPerYear) —
// the shape seasons.Load validates track files against.
func CalendarShape() (monthsPerYear, daysPerYear int) {
	return shapeFor(gametime.GetDate().Calendar)
}

// CalendarNow is the current calendar position for season resolution.
// GameDate.Day is the day-of-year (1-based; the engine subtracts whole years).
func CalendarNow() seasons.CalendarPos {
	gd := gametime.GetDate()
	_, days := shapeFor(gd.Calendar)
	return seasons.CalendarPos{DayOfYear: gd.Day, DaysPerYear: days}
}
