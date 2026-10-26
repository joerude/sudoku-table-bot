package domain

import "testing"

func TestSeasonRatingScore(t *testing.T) {
	cases := []struct {
		name string
		in   SeasonRatingInput
		want int
	}{
		// win 6/15, finish 15.4/17, duel 2.5/5: 100*(0.5*0.4 + 0.25*0.906 + 0.25*0.5)
		{"regular", SeasonRatingInput{PlayerID: 1, SeasonGames: 12, SeasonWins: 5, Played: 14, Finished: 13, Duels: 2, DuelWins: 1}, 55},
		// a single win is pulled toward the prior, not 100
		{"one lucky game", SeasonRatingInput{PlayerID: 2, SeasonGames: 1, SeasonWins: 1, Played: 1, Finished: 1}, 59},
		{"veteran", SeasonRatingInput{PlayerID: 3, SeasonGames: 10, SeasonWins: 6, Played: 10, Finished: 10}, 63},
		// only duels: the season-game part stays at its prior
		{"duels only", SeasonRatingInput{PlayerID: 4, Played: 2, Finished: 2, Duels: 2, DuelWins: 2}, 56},
	}
	for _, tc := range cases {
		got := SeasonRating([]SeasonRatingInput{tc.in})
		if len(got) != 1 || got[0].Score != tc.want {
			t.Errorf("%s: score = %+v, want %d", tc.name, got, tc.want)
		}
		if len(got) == 1 && got[0].PlayerID != tc.in.PlayerID {
			t.Errorf("%s: row lost the input: %+v", tc.name, got[0])
		}
	}
}

func TestSeasonRatingWeightsSumToOne(t *testing.T) {
	if sum := SeasonWinWeight + SeasonFinishWeight + SeasonDuelWeight; sum < 0.999 || sum > 1.001 {
		t.Errorf("weights sum to %v, want 1", sum)
	}
}

func TestSeasonRatingLuckDoesNotTopTheTable(t *testing.T) {
	lucky := SeasonRatingInput{PlayerID: 1, SeasonGames: 1, SeasonWins: 1, Played: 1, Finished: 1}
	steady := SeasonRatingInput{PlayerID: 2, SeasonGames: 10, SeasonWins: 6, Played: 10, Finished: 10}
	got := SeasonRating([]SeasonRatingInput{lucky, steady})
	if got[0].PlayerID != 2 {
		t.Errorf("ten solid games must beat one win, got %+v", got)
	}
}

func TestSeasonRatingSkipsPlayersWithoutGames(t *testing.T) {
	got := SeasonRating([]SeasonRatingInput{
		{PlayerID: 1},
		{PlayerID: 2, SeasonGames: 1, Played: 1},
	})
	if len(got) != 1 || got[0].PlayerID != 2 {
		t.Errorf("want only player 2, got %+v", got)
	}
	if SeasonRating(nil) == nil {
		t.Error("empty input must give an empty, non-nil slice")
	}
}

func TestSeasonRatingTieBreaks(t *testing.T) {
	// Same score (35): player 7 has a win, player 3 has none. Wins decide
	// before the player id does.
	withWin := SeasonRatingInput{PlayerID: 7, SeasonGames: 3, SeasonWins: 1, Played: 4, Finished: 0, Duels: 1}
	noWin := SeasonRatingInput{PlayerID: 3, SeasonGames: 1, Played: 3, Finished: 0, Duels: 2, DuelWins: 1}
	got := SeasonRating([]SeasonRatingInput{noWin, withWin})
	if got[0].Score != 35 || got[1].Score != 35 {
		t.Fatalf("fixture drifted, want a 35/35 tie: %+v", got)
	}
	if got[0].PlayerID != 7 {
		t.Errorf("more wins first on equal score, got %+v", got)
	}

	// Identical numbers: lower player id first.
	a := SeasonRatingInput{PlayerID: 9, SeasonGames: 2, SeasonWins: 1, Played: 2, Finished: 2}
	b := a
	b.PlayerID = 4
	got = SeasonRating([]SeasonRatingInput{a, b})
	if got[0].PlayerID != 4 || got[1].PlayerID != 9 {
		t.Errorf("player id asc on a full tie, got %+v", got)
	}
}
