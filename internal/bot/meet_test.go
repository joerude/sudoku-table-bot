package bot

import (
	"strings"
	"testing"
	"time"
)

func TestMeetCardView(t *testing.T) {
	loc := loadLoc("Asia/Bishkek") // UTC+6
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	created := func(ago time.Duration) string { return fmtDBTime(now.Add(-ago)) }
	cases := []struct {
		name            string
		createdAt       string
		meetAt          string
		status          string
		joined, min     int
		wantOffer       bool
		wantLineContain string
	}{
		{"fresh lobby waits", created(5 * time.Minute), "", "pending", 1, 2, false, ""},
		{"ten minutes alone: offer", created(10 * time.Minute), "", "pending", 1, 2, true, "⏳ Соперника пока нет"},
		{"empty room counts too", created(30 * time.Minute), "", "pending", 0, 2, true, "⏳"},
		{"enough players", created(30 * time.Minute), "", "pending", 2, 2, false, ""},
		{"three needed", created(30 * time.Minute), "", "pending", 2, 3, true, "⏳"},
		{"round started", created(30 * time.Minute), "", "running", 1, 2, false, ""},
		{"agreed: local time", created(30 * time.Minute), "2026-10-21 10:30:00", "pending", 1, 2, false,
			"🤝 <b>Вика</b> будет в 16:30"},
		{"agreed, then the round started", created(30 * time.Minute), "2026-10-21 10:30:00", "running", 2, 2, false, ""},
		{"unreadable created_at", "junk", "", "pending", 1, 2, false, ""},
		{"promiser unknown", created(30 * time.Minute), "2026-10-21 10:30:00", "pending", 1, 2, false,
			"🤝 <b>Соперник</b> будет в 16:30"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			by := "Вика"
			if c.name == "promiser unknown" {
				by = ""
			}
			offer, line := meetCardView(now, c.createdAt, c.meetAt, c.status, c.joined, c.min, by, loc)
			if offer != c.wantOffer {
				t.Errorf("offer = %v, want %v", offer, c.wantOffer)
			}
			if c.wantLineContain == "" && line != "" {
				t.Errorf("line = %q, want none", line)
			}
			if c.wantLineContain != "" && !strings.Contains(line, c.wantLineContain) {
				t.Errorf("line = %q, want it to contain %q", line, c.wantLineContain)
			}
			if strings.ContainsAny(line, "—–") {
				t.Errorf("no long dashes in bot texts: %q", line)
			}
		})
	}
	if _, line := meetCardView(now, created(time.Hour), "2026-10-21 10:30:00", "pending", 1, 2, "<b>", loc); !strings.Contains(line, "&lt;b&gt;") {
		t.Errorf("name must be escaped: %q", line)
	}
}

func TestMeetTimeRoundsUpToFiveMinutes(t *testing.T) {
	cases := []struct {
		now     string
		minutes int
		want    string
	}{
		{"2026-10-21 10:12:30", 15, "2026-10-21 10:30:00"},
		{"2026-10-21 10:15:00", 15, "2026-10-21 10:30:00"},
		{"2026-10-21 10:15:01", 15, "2026-10-21 10:35:00"},
		{"2026-10-21 10:02:00", 60, "2026-10-21 11:05:00"},
	}
	for _, c := range cases {
		now, _ := parseDBTime(c.now)
		if got := fmtDBTime(meetTime(now, c.minutes)); got != c.want {
			t.Errorf("meetTime(%s, %d) = %s, want %s", c.now, c.minutes, got, c.want)
		}
	}
}

func TestLiveKeyboardMeetRow(t *testing.T) {
	lp := &livePost{}
	if hasButton(lp.keyboard(7, 0), "🕒 Буду через 15 мин") {
		t.Errorf("no meet buttons without an offer")
	}
	lp.meetOffer = true
	kb := lp.keyboard(7, 0)
	first := kb.InlineKeyboard[0]
	if len(first) != 2 || first[0].Text != "🕒 Буду через 15 мин" || first[1].Text != "🕐 Через час" {
		t.Fatalf("meet buttons must lead the card: %+v", first)
	}
	if first[0].Unique != cbMeet || first[0].Data != "7:15" || first[1].Data != "7:60" {
		t.Errorf("payloads = %q / %q", first[0].Data, first[1].Data)
	}
	if !hasButton(kb, "📝 Записать результат") {
		t.Errorf("the record button stays")
	}
}

func TestMeetAgreementIsSticky(t *testing.T) {
	lp := &livePost{}
	lp.setMeet(true, meetOfferText)
	lp.agreeMeet("🤝 <b>Вика</b> будет в 16:30")
	// The watcher read the game just before the promise was saved.
	lp.setMeet(true, meetOfferText)
	if lp.meetOffer || lp.meetLine != "🤝 <b>Вика</b> будет в 16:30" {
		t.Errorf("a stale offer must not replace the agreement: %v %q", lp.meetOffer, lp.meetLine)
	}
	lp.setMeet(false, "") // the round started
	if lp.meetOffer || lp.meetLine != "" {
		t.Errorf("the round start clears the meet: %v %q", lp.meetOffer, lp.meetLine)
	}
}

func TestParseMeet(t *testing.T) {
	if id, m, ok := parseMeet("12:60"); !ok || id != 12 || m != 60 {
		t.Errorf("parseMeet(12:60) = %d, %d, %v", id, m, ok)
	}
	for _, bad := range []string{"12:30", "12", "x:15", ""} {
		if _, _, ok := parseMeet(bad); ok {
			t.Errorf("parseMeet(%q) must fail", bad)
		}
	}
}

func TestMeetPingText(t *testing.T) {
	loc := loadLoc("Asia/Bishkek")
	at, _ := parseDBTime("2026-10-21 10:30:00")
	got := meetPingText([]string{"@alex", "Вика"}, at, loc, "LOVI")
	want := "⏰ @alex, Вика: договаривались на 16:30. Комната ждёт: https://www.usdoku.com/LOVI"
	if got != want {
		t.Errorf("meetPingText = %q\nwant          %q", got, want)
	}
	if got := meetPingText([]string{"Вика"}, at, loc, ""); strings.Contains(got, "usdoku") {
		t.Errorf("no room, no link: %q", got)
	}
}

func TestMeetEligible(t *testing.T) {
	b, st, g := hookFixture(t, -7402, 300, 400) // any game; eligibility looks at the pending one
	se, _ := st.ActiveSeason(g.ChatID)
	gid, _ := st.CreatePendingGame(g.ChatID, se.ID, 1, "medium", "hardcore")
	pending, _ := st.GameByID(gid)
	if !b.meetEligible(pending) {
		t.Errorf("a regular game card gets the offer")
	}
	st.SetPostCard(gid, 1, "base", cardKindInvite)
	invite, _ := st.GameByID(gid)
	if b.meetEligible(invite) {
		t.Errorf("the invite card has its own RSVP roster")
	}
	st.SetPostCard(gid, 1, "base", "")
	players, _ := st.ListPlayers(g.ChatID)
	st.SetDuelTarget(gid, players[1].ID)
	duel, _ := st.GameByID(gid)
	if b.meetEligible(duel) {
		t.Errorf("a duel has its own accept and expiry")
	}
	gid2, _ := st.CreatePendingGame(g.ChatID, se.ID, 1, "medium", "hardcore")
	regular, _ := st.GameByID(gid2)
	st.SetFeature(g.ChatID, featMeet, false)
	if b.meetEligible(regular) {
		t.Errorf("switched off: no offer")
	}
}
