package bot

import (
	"strings"
	"testing"
)

// The assembled profile, not just its parts: the phrase sits right under the
// name line, the record against the opponent is there, and a player without a
// phrase gets no 💬 line.
func TestProfileTextShowsPhrase(t *testing.T) {
	const chat = int64(-7701)
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	vova, _, _ := st.RegisterPlayer(chat, 1, "Вова")
	dima, _, _ := st.RegisterPlayer(chat, 2, "Дима")
	gid, err := st.CreatePendingGame(chat, se.ID, vova.ID, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	st.AddPick(gid, vova.ID)
	st.AddPick(gid, dima.ID)
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPhrase(vova.ID, "Нагнул тебя брат"); err != nil {
		t.Fatal(err)
	}
	b := &Bot{st: st}

	out, err := b.profileText(chat, se, vova)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "Вова") || lines[1] != "💬 «Нагнул тебя брат»" {
		t.Errorf("want the phrase right under the name line, got:\n%s", out)
	}
	if !strings.Contains(out, "Дима · игры 1:0") {
		t.Errorf("want the record against Дима, got:\n%s", out)
	}

	other, err := b.profileText(chat, se, dima)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(other, "💬") {
		t.Errorf("no phrase set, want no 💬 line, got:\n%s", other)
	}
	if !strings.Contains(other, "Вова · игры 0:1") {
		t.Errorf("want the mirrored record, got:\n%s", other)
	}
}
