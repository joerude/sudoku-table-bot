package domain

import "time"

// SeasonMonths is how long a season runs by the calendar before its deadline
// can end it. The deadline is a backstop, not the finish line: it only closes a
// season that has actually been played (see DefaultMinGames).
const SeasonMonths = 2

// addMonths returns local midnight n calendar months after t. Day overflow
// normalises forward (Dec 31 + 2mo → Mar 3), which only ever lengthens a season.
func addMonths(t time.Time, n int, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month()+time.Month(n), t.Day(), 0, 0, 0, 0, loc)
}

// SeasonDeadline returns (in UTC) when a season started at start must end:
// local midnight opening the day SeasonMonths months later. The instant is
// exclusive — the last playable day is the one before it.
func SeasonDeadline(start time.Time, loc *time.Location) time.Time {
	return addMonths(start, SeasonMonths, loc).UTC()
}

// ExtendSeasonDeadline moves an expired deadline forward one whole month at a
// time until it is in the future. Used when the season hasn't been played
// enough to crown anyone yet, and to recover after an outage that swallowed
// several deadlines.
func ExtendSeasonDeadline(deadline, now time.Time, loc *time.Location) time.Time {
	d := deadline
	for !d.After(now) {
		d = addMonths(d, 1, loc)
	}
	return d.UTC()
}
