package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featCoop is the season's shared goal: the chat collects clean boards (a
// season game where everyone who joined finished) toward three stars set at
// 20%, 35% and 50% of the games floor. Nobody competes with anybody, and a
// quit costs only the board, never a player's name.
const featCoop = "coop"

var (
	coopOrdinals    = [...]string{"первая", "вторая", "третья"}
	coopOrdinalsGen = [...]string{"первой", "второй", "третьей"}
)

// coopBoards is "N досок" in the right form.
func coopBoards(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "доска", "доски", "досок"))
}

func coopStarsText(k int) string { return strings.Repeat("⭐", k) }

// coopOrdinal picks the word for star index i (0-based), "" past the table.
func coopOrdinal(words [3]string, i int) string {
	if i < 0 || i >= len(words) {
		return ""
	}
	return words[i]
}

// coopResultLine is the line under a result; n counts clean boards including
// this one. A tier hit is celebrated, anything else shows the way to the next star.
func coopResultLine(n int, tiers []int) string {
	if len(tiers) == 0 {
		return ""
	}
	for k, t := range tiers {
		if n == t {
			return fmt.Sprintf("🌟 Общая цель: %s звезда %s · %s без брошенных",
				coopOrdinal(coopOrdinals, k), coopStarsText(k+1), coopBoards(n))
		}
	}
	stars := domain.CoopStars(n, tiers)
	if stars == len(tiers) {
		return fmt.Sprintf("🌟 Общая цель: %s %s", coopBoards(n), coopStarsText(stars))
	}
	line := fmt.Sprintf("🌟 Общая цель: %d/%d", n, tiers[stars])
	if stars > 0 {
		line += " " + coopStarsText(stars)
	}
	return line
}

// coopLiveLine is the card's promise while a game is still being solved; n
// counts the clean boards before this one.
func coopLiveLine(n int, tiers []int) string {
	if len(tiers) == 0 {
		return ""
	}
	stars := domain.CoopStars(n, tiers)
	if stars == len(tiers) {
		return fmt.Sprintf("🌟 Доиграют все: доска пойдёт в общую цель (%s)", coopBoards(n))
	}
	return fmt.Sprintf("🌟 Доиграют все: доска пойдёт в общую цель (%d/%d)", n, tiers[stars])
}

// coopPanelLine is the line under the season panels.
func coopPanelLine(n int, tiers []int) string {
	if len(tiers) == 0 {
		return ""
	}
	stars := domain.CoopStars(n, tiers)
	line := "🌟 Общая цель: " + coopBoards(n) + " без брошенных"
	switch {
	case stars == len(tiers):
		return line + " " + coopStarsText(stars)
	case stars == 0:
		return fmt.Sprintf("%s · %s звезда на %d", line, coopOrdinal(coopOrdinals, 0), tiers[0])
	}
	return fmt.Sprintf("%s %s · следующая звезда на %d", line, coopStarsText(stars), tiers[stars])
}

// coopNudgeLine is the line in the daily nudge: how far the next star is.
func coopNudgeLine(n int, tiers []int) string {
	if len(tiers) == 0 {
		return ""
	}
	stars := domain.CoopStars(n, tiers)
	if stars == len(tiers) {
		return fmt.Sprintf("🌟 Общая цель: %s %s", coopBoards(n), coopStarsText(stars))
	}
	line := fmt.Sprintf("🌟 Общая цель: %d/%d", n, tiers[stars])
	if stars > 0 {
		line += " " + coopStarsText(stars)
	}
	gap := tiers[stars] - n
	return fmt.Sprintf("%s · до %s звезды %s", line, coopOrdinal(coopOrdinalsGen, stars), coopBoards(gap))
}

// coopSeasonResultLine is the closed season's final tally for its end post.
func coopSeasonResultLine(number, n int, tiers []int) string {
	if len(tiers) == 0 {
		return ""
	}
	line := fmt.Sprintf("🌟 Общая цель сезона %d: %s", number, coopBoards(n))
	if stars := domain.CoopStars(n, tiers); stars > 0 {
		line += " " + coopStarsText(stars)
	}
	return line
}

// coopNextSeasonLine announces the new season's star thresholds.
func coopNextSeasonLine(number int, tiers []int) string {
	if len(tiers) == 0 {
		return ""
	}
	nums := make([]string, len(tiers))
	for i, t := range tiers {
		nums[i] = strconv.Itoa(t)
	}
	list := nums[0]
	if len(nums) > 1 {
		list = strings.Join(nums[:len(nums)-1], ", ") + " и " + nums[len(nums)-1]
	}
	last := tiers[len(tiers)-1]
	return fmt.Sprintf("🌟 В сезоне %d звёзды на %s %s без брошенных",
		number, list, plural(last, "доску", "доски", "досок"))
}

// coopSeasonEnd joins the two season-end lines (the closed season's tally, the
// new season's stars) into one block, "" when both are empty.
func coopSeasonEnd(closedNumber, clean int, closedTiers []int, nextNumber int, nextTiers []int) string {
	var lines []string
	for _, l := range []string{
		coopSeasonResultLine(closedNumber, clean, closedTiers),
		coopNextSeasonLine(nextNumber, nextTiers),
	} {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// coopTiers returns the season's star thresholds, nil while the mechanic is off
// or the season has no games floor.
func (b *Bot) coopTiers(season *storage.Season) []int {
	if season == nil || !b.featureOn(season.ChatID, featCoop) {
		return nil
	}
	return domain.CoopTiers(season.MinGames)
}

// coopClean counts the season's clean boards. False when the count can't be
// read: every coop line is best-effort and stays out instead.
func (b *Bot) coopClean(season *storage.Season, until string) (int, bool) {
	n, err := b.st.SeasonCleanGames(season.ChatID, season.ID, until)
	if err != nil {
		log.Printf("coopClean: %v", err)
		return 0, false
	}
	return n, true
}

// coopFooter is the result post's shared-goal line ("" for duels, when the
// mechanic is off, or when the game was not clean). The tally is cut at the
// game's own completion time, so redrawing an old result after an edit shows
// the count as it stood then.
func (b *Bot) coopFooter(game *storage.Game) string {
	if !b.featureOn(game.ChatID, featCoop) {
		return ""
	}
	if _, isDuel, err := b.st.DuelTargetID(game.ID); err != nil || isDuel {
		return ""
	}
	season, err := b.st.SeasonByID(game.SeasonID)
	if err != nil || season == nil {
		log.Printf("coopFooter.season: %v", err)
		return ""
	}
	tiers := domain.CoopTiers(season.MinGames)
	if len(tiers) == 0 {
		return ""
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("coopFooter.results: %v", err)
		return ""
	}
	for _, r := range rows {
		if r.Rank == 0 {
			return ""
		}
	}
	until := ""
	// The game in hand predates its own scoring, so read the stamp back.
	if g, err := b.st.GameByID(game.ID); err == nil && g != nil && g.CompletedAt.Valid {
		until = g.CompletedAt.String
	}
	n, ok := b.coopClean(season, until)
	if !ok {
		return ""
	}
	return "\n\n" + coopResultLine(n, tiers)
}

// coopLive is the live card's line for a game still being solved: "" for duels,
// when the mechanic is off, or when the count can't be read.
func (b *Bot) coopLive(game *storage.Game) string {
	if !b.featureOn(game.ChatID, featCoop) {
		return ""
	}
	if _, isDuel, err := b.st.DuelTargetID(game.ID); err != nil || isDuel {
		return ""
	}
	season, err := b.st.SeasonByID(game.SeasonID)
	if err != nil || season == nil {
		return ""
	}
	tiers := domain.CoopTiers(season.MinGames)
	if len(tiers) == 0 {
		return ""
	}
	n, ok := b.coopClean(season, "")
	if !ok {
		return ""
	}
	return coopLiveLine(n, tiers)
}

// coopPanel is the season panels' line, "" while off or when unreadable.
func (b *Bot) coopPanel(season *storage.Season) string {
	tiers := b.coopTiers(season)
	if len(tiers) == 0 {
		return ""
	}
	n, ok := b.coopClean(season, "")
	if !ok {
		return ""
	}
	return coopPanelLine(n, tiers)
}

// coopSeasonEndBlock is the shared-goal text for a season-end post: the closed
// season's tally and the new season's stars. "" while the mechanic is off.
func (b *Bot) coopSeasonEndBlock(closed, next *storage.Season) string {
	closedTiers, nextTiers := b.coopTiers(closed), b.coopTiers(next)
	n := 0
	if len(closedTiers) > 0 {
		var ok bool
		if n, ok = b.coopClean(closed, ""); !ok {
			closedTiers = nil // no tally to show, the new season's stars still go out
		}
	}
	return coopSeasonEnd(closed.Number, n, closedTiers, next.Number, nextTiers)
}
