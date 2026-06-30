package domain

import "testing"

// helper: finishers in the given order (rank 1..n), then DNFs (rank 0).
func game(id int64, finishers []int64, dnf ...int64) RatingGame {
	g := RatingGame{ID: id}
	for i, pid := range finishers {
		g.Participants = append(g.Participants, RatingResult{PlayerID: pid, Rank: i + 1})
	}
	for _, pid := range dnf {
		g.Participants = append(g.Participants, RatingResult{PlayerID: pid, Rank: 0})
	}
	return g
}

func TestKFactor(t *testing.T) {
	for _, tc := range []struct {
		played int
		want   float64
	}{{0, 64}, {9, 64}, {10, 32}, {50, 32}} {
		if got := kFactor(tc.played); got != tc.want {
			t.Errorf("kFactor(%d)=%v want %v", tc.played, got, tc.want)
		}
	}
}

func TestTwoPlayerFirstGame(t *testing.T) {
	// Both start 1000, provisional K=64, E=0.5 -> winner +32, loser -32.
	r := ComputeRatings([]RatingGame{game(1, []int64{10, 20})})
	if r.Players[10].Rating != 1032 || r.Players[20].Rating != 968 {
		t.Fatalf("ratings: 10=%d 20=%d want 1032/968", r.Players[10].Rating, r.Players[20].Rating)
	}
	if r.Crown != 10 {
		t.Errorf("crown=%d want 10", r.Crown)
	}
	if len(r.PerGame) != 1 || r.PerGame[0].Delta[10] != 32 || r.PerGame[0].Delta[20] != -32 {
		t.Errorf("per-game delta wrong: %+v", r.PerGame)
	}
	if r.PerGame[0].CrownBefore != 0 || r.PerGame[0].CrownAfter != 10 {
		t.Errorf("crown change: before=%d after=%d want 0/10", r.PerGame[0].CrownBefore, r.PerGame[0].CrownAfter)
	}
}

func TestThreePlayerKSplit(t *testing.T) {
	// ranks 10>20>30, all start 1000, K=64 split by 2 opponents -> per-pair 32.
	// 10:+16+16=+32, 20:-16+16=0, 30:-16-16=-32. |delta| <= 64.
	r := ComputeRatings([]RatingGame{game(1, []int64{10, 20, 30})})
	if r.Players[10].Rating != 1032 || r.Players[20].Rating != 1000 || r.Players[30].Rating != 968 {
		t.Fatalf("ratings: 10=%d 20=%d 30=%d want 1032/1000/968",
			r.Players[10].Rating, r.Players[20].Rating, r.Players[30].Rating)
	}
}

func TestDNFIsLoss(t *testing.T) {
	// 10 finishes, 20 DNF -> 20 loses to 10 like a normal loss.
	r := ComputeRatings([]RatingGame{game(1, []int64{10}, 20)})
	if r.Players[10].Rating != 1032 || r.Players[20].Rating != 968 {
		t.Fatalf("ratings: 10=%d 20=%d want 1032/968", r.Players[10].Rating, r.Players[20].Rating)
	}
}

func TestDNFvsDNFIgnored(t *testing.T) {
	// 10 finishes; 20 and 30 both DNF. 20 and 30 don't play each other, so they
	// only lose to 10 and end up equal.
	r := ComputeRatings([]RatingGame{game(1, []int64{10}, 20, 30)})
	if r.Players[20].Rating != r.Players[30].Rating {
		t.Errorf("DNF-vs-DNF should not move them: 20=%d 30=%d", r.Players[20].Rating, r.Players[30].Rating)
	}
	if r.Players[10].Rating <= 1000 {
		t.Errorf("finisher should rise: 10=%d", r.Players[10].Rating)
	}
}

func TestPeakTracksHighWaterMark(t *testing.T) {
	// 10 wins (1032), then loses to 20 (who is now higher) — peak stays 1032.
	r := ComputeRatings([]RatingGame{
		game(1, []int64{10, 20}),
		game(2, []int64{20, 10}),
	})
	if r.Players[10].Peak != 1032 {
		t.Errorf("peak=%d want 1032", r.Players[10].Peak)
	}
	if r.Players[10].Rating >= 1032 {
		t.Errorf("current should have dropped below peak: %d", r.Players[10].Rating)
	}
}

func TestProvisionalFlag(t *testing.T) {
	var games []RatingGame
	for i := int64(1); i <= 10; i++ {
		games = append(games, game(i, []int64{10, 20}))
	}
	r := ComputeRatings(games)
	if r.Players[10].Games != 10 || r.Players[10].Provisional {
		t.Errorf("after 10 games: games=%d provisional=%v want 10/false",
			r.Players[10].Games, r.Players[10].Provisional)
	}
}

func TestTopPlayerIncumbentKeepsOnTie(t *testing.T) {
	// Exact tie: incumbent 20 keeps the crown; without an incumbent it breaks to
	// the lowest id (10).
	tie := map[int64]int{10: 1000, 20: 1000}
	if got := topPlayer(tie, 20); got != 20 {
		t.Errorf("incumbent should keep crown on tie: got %d want 20", got)
	}
	if got := topPlayer(tie, 0); got != 10 {
		t.Errorf("no incumbent -> lowest id wins tie: got %d want 10", got)
	}
	// A strictly higher rating always takes the crown regardless of incumbency.
	if got := topPlayer(map[int64]int{10: 1100, 20: 1000}, 20); got != 10 {
		t.Errorf("higher rating must take crown: got %d want 10", got)
	}
}

func TestDeterministic(t *testing.T) {
	games := []RatingGame{game(1, []int64{10, 20, 30}), game(2, []int64{30, 10}, 20)}
	a := ComputeRatings(games)
	b := ComputeRatings(games)
	for id := range a.Players {
		if a.Players[id] != b.Players[id] {
			t.Errorf("non-deterministic for %d: %+v vs %+v", id, a.Players[id], b.Players[id])
		}
	}
}

func TestSoloGameNoImpact(t *testing.T) {
	r := ComputeRatings([]RatingGame{game(1, []int64{10})}) // single finisher, no opponent
	if len(r.PerGame) != 0 {
		t.Errorf("solo game must produce no rating impact, got %+v", r.PerGame)
	}
	if len(r.Players) != 0 {
		t.Errorf("solo game must not register players, got %+v", r.Players)
	}
}
