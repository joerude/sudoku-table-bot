package bot

// plural picks the Russian noun form for n: one (1 игра), few (2 игры) or
// many (5 игр, 0 игр). 11-14 are always "many", so 21 is "one" and 22 "few".
func plural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	if m := n % 100; m >= 11 && m <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	}
	return many
}
