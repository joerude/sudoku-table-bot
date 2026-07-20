package domain

import "time"

// minSeasonDays is the shortest a fresh season may be. A season opened in the
// tail of a month would otherwise be decided in a couple of days, so its
// deadline skips to the end of the following month instead.
const minSeasonDays = 7

// nextMonthStart returns midnight starting the calendar month after t, in loc.
func nextMonthStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
}

// SeasonDeadline returns (in UTC) when a season created at now must end: the
// midnight that starts the next calendar month in loc, pushed one month further
// when that is less than minSeasonDays away.
func SeasonDeadline(now time.Time, loc *time.Location) time.Time {
	d := nextMonthStart(now, loc)
	if d.Sub(now) < minSeasonDays*24*time.Hour {
		d = nextMonthStart(d, loc)
	}
	return d.UTC()
}

// ExtendSeasonDeadline moves an expired deadline forward by whole months until
// it is in the future. Used when a season's month ended with nothing played,
// and to recover after an outage that swallowed several deadlines.
func ExtendSeasonDeadline(deadline, now time.Time, loc *time.Location) time.Time {
	d := deadline
	for !d.After(now) {
		d = nextMonthStart(d, loc)
	}
	return d.UTC()
}
