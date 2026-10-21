package bot

import (
	"fmt"
	"log"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featHook is the post-game hook: one line under a seasonal result that gives
// a reason to play the next game right away (a near miss or what is at stake).
const featHook = "hook"

// evHookShown logs a shown hook line; detail is "near" or "stake".
const evHookShown = "hook_shown"

// nearMissPair is the closest finish between neighbours: Name lost Place by Gap seconds.
type nearMissPair struct {
	Place int
	Name  string
	Gap   int
}

// nearMissMax is how close a finish must be to count as a near miss: 15 s, or
// 3% of the faster time on long games.
func nearMissMax(faster int) int {
	if m := faster * 3 / 100; m > 15 {
		return m
	}
	return 15
}

// nearMiss finds the closest pair of neighbouring finishers (rank order, both
// with a solve time) within nearMissMax; on equal gaps the higher place wins.
// Times that contradict the finish order (a hand-made order) are skipped.
func nearMiss(rows []storage.ResultRow) (nearMissPair, bool) {
	var best nearMissPair
	found := false
	for i := 1; i < len(rows); i++ {
		hi, lo := rows[i-1], rows[i]
		if hi.Rank < 1 || lo.Rank != hi.Rank+1 || hi.Duration <= 0 || lo.Duration <= 0 {
			continue
		}
		gap := lo.Duration - hi.Duration
		if gap < 0 || gap > nearMissMax(hi.Duration) {
			continue
		}
		if !found || gap < best.Gap {
			best, found = nearMissPair{Place: hi.Rank, Name: lo.Name, Gap: gap}, true
		}
	}
	return best, found
}

// nearMissLine renders a near miss. The name stays in the nominative: the bot
// cannot decline arbitrary names.
func nearMissLine(nm nearMissPair) string {
	gap := "меньше секунды"
	if nm.Gap > 0 {
		gap = secsPhrase(nm.Gap)
	}
	if nm.Place == 1 {
		return fmt.Sprintf("😤 До победы не хватило %s: <b>%s</b>. Реванш?", gap, esc(nm.Name))
	}
	return fmt.Sprintf("😤 До %d-го места не хватило %s: <b>%s</b>. Ещё одну?", nm.Place, gap, esc(nm.Name))
}

// secsPhrase renders a short time gap: "7 с" under a minute, "1:05" above.
func secsPhrase(secs int) string {
	if secs < 60 {
		return fmt.Sprintf("%d с", secs)
	}
	return fmtDuration(secs)
}

// stakeLine says what the next game or two can change in the season table, ""
// when nothing is in reach: the leader closing the season within one or two
// wins, or an overtake a single win guarantees (it must hold even if the
// player ahead takes 2nd in the same game). Two-win overtakes are left out:
// they are true most of the season and would turn the line into wallpaper.
// Closing the season comes first; overtakes are checked from the bottom up,
// so the trailing player's chance is the one shown.
func stakeLine(standings []storage.Standing, table []int, target int) string {
	if len(standings) < 2 || len(table) == 0 {
		return ""
	}
	win, second := table[0], 0
	if len(table) > 1 {
		second = table[1]
	}
	if win <= second {
		return ""
	}
	for n := 1; n <= 2; n++ {
		if lead := standings[0]; target > 0 && lead.Points < target && lead.Points+n*win >= target {
			return closeSeasonLine(n, lead, target)
		}
		if n > 1 {
			continue
		}
		for i := len(standings) - 1; i >= 1; i-- {
			lo, hi := standings[i], standings[i-1]
			if lo.Points+win > hi.Points+second {
				return overtakeLine(lo, hi, i)
			}
		}
	}
	return ""
}

func closeSeasonLine(wins int, lead storage.Standing, target int) string {
	phrase := "Ещё одна победа"
	if wins == 2 {
		phrase = "Две победы"
	}
	return fmt.Sprintf("⏭ %s, и <b>%s</b> закрывает сезон (%d/%d)", phrase, esc(lead.Name), lead.Points, target)
}

// overtakeLine: lo trails hi, who holds place, and one win settles it.
func overtakeLine(lo, hi storage.Standing, place int) string {
	gap := hi.Points - lo.Points
	if gap == 0 {
		return fmt.Sprintf("⏭ <b>%s</b> и <b>%s</b> идут вровень за %d-е место: решит следующая игра",
			esc(hi.Name), esc(lo.Name), place)
	}
	return fmt.Sprintf("⏭ <b>%s</b> в %d %s от %d-го места: хватит одной победы",
		esc(lo.Name), gap, plural(gap, "очке", "очках", "очках"), place)
}

// hookFooter is the hook line under a seasonal result ("" for duels, when the
// mechanic is off, or when there is nothing to say). A near miss wins over the
// stakes; stakes are skipped once the season is over (closed by this game).
func (b *Bot) hookFooter(game *storage.Game) string {
	if !b.featureOn(game.ChatID, featHook) {
		return ""
	}
	if _, isDuel, err := b.st.DuelTargetID(game.ID); err != nil || isDuel {
		return ""
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("hookFooter.results: %v", err)
		return ""
	}
	line, kind := "", "near"
	if nm, ok := nearMiss(rows); ok {
		line = nearMissLine(nm)
	} else if season, err := b.st.SeasonByID(game.SeasonID); err == nil && season.Status == "active" {
		standings, err := b.st.Standings(game.ChatID, season.ID)
		if err != nil {
			log.Printf("hookFooter.standings: %v", err)
			return ""
		}
		line, kind = stakeLine(standings, season.PointsTable, season.Target), "stake"
	}
	if line == "" {
		return ""
	}
	b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, Kind: evHookShown, Detail: kind})
	return "\n\n" + line
}

// hasNearMiss reports whether a recorded regular game's win was lost by a near
// miss with the hook on: its «Ещё одну» button then reads «Реванш». A near
// miss for a lower place keeps «Ещё одну», as its line does.
func (b *Bot) hasNearMiss(game *storage.Game) bool {
	if !b.featureOn(game.ChatID, featHook) {
		return false
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		return false
	}
	nm, ok := nearMiss(rows)
	return ok && nm.Place == 1
}
