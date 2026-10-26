package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featTitles adds the week's titles to the Monday digest.
const featTitles = "titles"

// titleLookbackDays of games before the week feed the win streaks a title
// can be earned by stopping.
const titleLookbackDays = 60

// titleLine renders one title, "" for an unknown kind.
func titleLine(a domain.TitleAward, name string) string {
	n := "<b>" + esc(name) + "</b>"
	switch a.Kind {
	case domain.TitleGrowth:
		return fmt.Sprintf("🏃 Рост недели: %s (%d %s быстрее своей нормы)", n, a.Score, gamesWord(a.Score))
	case domain.TitleFinisher:
		return fmt.Sprintf("🧱 Железный финиш: %s (доиграно %d из %d)", n, a.Score, a.Total)
	case domain.TitleHunter:
		return fmt.Sprintf("🎯 Охотник: %s (впереди лидера в %d %s)", n, a.Score, plural(a.Score, "игре", "играх", "играх"))
	case domain.TitleBreaker:
		if a.Score == 1 {
			return fmt.Sprintf("🧯 Стоп-кран: %s (прервана серия побед)", n)
		}
		return fmt.Sprintf("🧯 Стоп-кран: %s (прервано серий побед: %d)", n, a.Score)
	case domain.TitleOwl:
		return fmt.Sprintf("🦉 Сова недели: %s (%d %s после 22:00)", n, a.Score, gamesWord(a.Score))
	case domain.TitleSprinter:
		return fmt.Sprintf("⚡ Спринтер недели: %s (%s на %s)", n, fmtDuration(a.Score), titleCase(a.Difficulty))
	}
	return ""
}

// groupTitleGames folds flat result rows (game order, finish order) into games.
func groupTitleGames(rows []storage.ResultRowSince) []domain.TitleGame {
	var out []domain.TitleGame
	var cur int64
	skip := false // the current game's time is unreadable: drop its rows
	for i, r := range rows {
		if i == 0 || r.GameID != cur {
			cur = r.GameID
			at, err := parseDBTime(r.CompletedAt)
			if skip = err != nil; skip {
				continue
			}
			out = append(out, domain.TitleGame{At: at, SeasonID: r.SeasonID, Duel: r.Duel,
				Difficulty: r.Difficulty, Mode: r.Mode})
		}
		if skip {
			continue
		}
		last := &out[len(out)-1]
		last.Results = append(last.Results, domain.TitleResult{PlayerID: r.PlayerID, Rank: r.Rank, Secs: r.Secs})
	}
	return out
}

// weekTitlesBlock is the digest's titles section, "" when switched off or when
// nobody earned a title this week.
func (b *Bot) weekTitlesBlock(chatID int64, since time.Time, season *storage.Season, standings []storage.Standing) string {
	if !b.featureOn(chatID, featTitles) {
		return ""
	}
	rows, err := b.st.ResultsSince(chatID, fmtDBTime(since.AddDate(0, 0, -titleLookbackDays)))
	if err != nil {
		log.Printf("weekTitles.results: %v", err)
		return ""
	}
	pace, err := b.st.EventGamesByPlayerSince(chatID, evPaceShown, fmtDBTime(since))
	if err != nil {
		log.Printf("weekTitles.pace: %v", err)
	}
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		log.Printf("weekTitles.players: %v", err)
		return ""
	}
	active := make(map[int64]bool, len(players))
	names := make(map[int64]string, len(players))
	for _, p := range players {
		active[p.ID], names[p.ID] = true, p.Name
	}
	var leader int64
	if len(standings) > 0 && standings[0].Points > 0 {
		leader = standings[0].PlayerID
	}
	// A season closed by its target starts the next one in the same second as
	// its last game, so the window alone would let that game into the week.
	var games []domain.TitleGame
	for _, g := range groupTitleGames(rows) {
		if g.At.Before(since) || g.SeasonID == season.ID {
			games = append(games, g)
		}
	}
	awards := domain.WeekTitles(domain.TitleInput{
		Games: games, Since: since, Loc: loadLoc(b.chatTZ(chatID)),
		LeaderID: leader, SeasonID: season.ID, PaceWins: pace, Active: active,
	})
	var lines []string
	for _, a := range awards {
		if line := titleLine(a, names[a.PlayerID]); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n🏷 <b>Титулы недели</b>\n" + strings.Join(lines, "\n")
}
