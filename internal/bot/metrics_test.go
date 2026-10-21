package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestParseMetricsDays(t *testing.T) {
	cases := []struct {
		args []string
		want int
		ok   bool
	}{
		{nil, 14, true}, {[]string{"28"}, 28, true}, {[]string{"1"}, 1, true}, {[]string{"90"}, 90, true},
		{[]string{"0"}, 0, false}, {[]string{"91"}, 0, false}, {[]string{"week"}, 0, false},
	}
	for _, c := range cases {
		got, ok := parseMetricsDays(c.args)
		if got != c.want || ok != c.ok {
			t.Errorf("parseMetricsDays(%v) = %d, %v; want %d, %v", c.args, got, ok, c.want, c.ok)
		}
	}
}

func TestMetricsText(t *testing.T) {
	v := metricsView{
		Days: 14,
		Cur: domain.Metrics{
			PerWeek: 13.5, Days: 14, DaysWithGame: 10,
			Chained: 7, ChainBase: 20, ChainHook: 4, ChainHookBase: 10, ChainPlain: 2, ChainPlainBase: 8,
			SeasonGames: 18, AllFinished: 6,
			DNF:              map[int64][2]int{1: {3, 10}, 2: {0, 9}},
			AbandonedPerWeek: 1, Nudges: 6, NudgesPlayed: 2,
		},
		Prev: domain.Metrics{
			PerWeek: 7.3, Days: 14, DaysWithGame: 8, Chained: 3, ChainBase: 12,
			SeasonGames: 10, AllFinished: 2, DNF: map[int64][2]int{1: {5, 10}},
			AbandonedPerWeek: 2.5,
		},
		Names: map[int64]string{1: "<b>Алекс</b>", 2: "Боря", 3: "Вика"},
		Order: []int64{1, 2, 3},
		Meets: 3, MeetsPlayed: 2, Pace: 5, HookNear: 4, HookStake: 14,
		Off: []string{"pace"},
	}
	got := metricsText(v)
	for _, want := range []string{
		"📈 <b>Метрики</b> · 14 полных дней, в скобках 14 дней до них",
		"🎮 Игр в неделю: <b>13,5</b> (7,3)",
		"📅 Дни с игрой: <b>71%</b> (57%)",
		"🔁 Следующая игра за 10 мин: <b>35%</b> (25%)",
		"с крючком 40% из 10 · без 25% из 8",
		"🏁 Доиграли все: <b>33%</b> (20%)",
		"🚪 DNF: &lt;b&gt;Алекс&lt;/b&gt; <b>30%</b> (50%) · Боря <b>0%</b> (н/д)",
		"🗑 Пустых комнат в неделю: <b>1,0</b> (2,5)",
		"🤝 «Буду через…»: 3, сыграно 2",
		"⏰ Напоминаний: 6, игра в течение 2 ч: 2",
		"🏃 Строк темпа: 5 · крючков: 18 (😤 4, ⏭ 14)",
		"🔕 Выключено: Личный темп",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("metrics text misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Вика") {
		t.Errorf("a player with no games in either window is left out:\n%s", got)
	}
	if strings.ContainsAny(got, "—–") {
		t.Errorf("no long dashes in bot texts:\n%s", got)
	}
	v.Off = nil
	if !strings.Contains(metricsText(v), "🔕 Выключено: ничего") {
		t.Errorf("all mechanics on must say so")
	}
}

func TestChatMetricsCountsRecentGames(t *testing.T) {
	b, st := titlesChat(t, -7801) // five games played just now
	// Windows end at local midnight: today is never half counted, so look
	// from two days ahead to see today's games.
	if view, _ := b.chatMetrics(-7801, time.Now(), 14); view.Cur.Games != 0 {
		t.Errorf("today is not a whole day yet, got %d games", view.Cur.Games)
	}
	view, err := b.chatMetrics(-7801, time.Now().Add(48*time.Hour), 14)
	if err != nil {
		t.Fatal(err)
	}
	if view.Cur.Games != 5 || view.Prev.Games != 0 || view.Cur.SeasonGames != 5 {
		t.Errorf("current/previous games = %d/%d, want 5/0", view.Cur.Games, view.Prev.Games)
	}
	if len(view.Order) != 2 || view.Names[view.Order[0]] == "" {
		t.Errorf("players = %v %v", view.Order, view.Names)
	}
	for i := 0; i < 2; i++ { // the same pace line logged twice (an edit re-renders it)
		st.LogEvent(storage.Event{ChatID: -7801, GameID: 1, PlayerID: 2, Kind: evPaceShown})
	}
	if view, _ := b.chatMetrics(-7801, time.Now().Add(48*time.Hour), 14); view.Pace != 1 {
		t.Errorf("pace lines = %d, want 1 (distinct game and player)", view.Pace)
	}
	st.SetFeature(-7801, featHook, false)
	if view, _ := b.chatMetrics(-7801, time.Now().Add(48*time.Hour), 14); len(view.Off) != 1 || view.Off[0] != featHook {
		t.Errorf("switched-off mechanics are listed: %v", view.Off)
	}
}
