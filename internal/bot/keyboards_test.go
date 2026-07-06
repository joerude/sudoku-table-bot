package bot

import (
	"reflect"
	"testing"
)

func TestSeasonNumbers(t *testing.T) {
	cases := []struct {
		name     string
		archived []int
		active   int
		want     []int
	}{
		{"archive plus active", []int{1, 2, 3}, 4, []int{1, 2, 3, 4}},
		{"no archive", nil, 1, []int{1}},
		{"active duplicated in archive", []int{1, 2}, 2, []int{1, 2}},
		{"unsorted archive", []int{3, 1, 2}, 4, []int{1, 2, 3, 4}},
	}
	for _, tc := range cases {
		if got := seasonNumbers(tc.archived, tc.active); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: seasonNumbers(%v, %d) = %v, want %v",
				tc.name, tc.archived, tc.active, got, tc.want)
		}
	}
}
