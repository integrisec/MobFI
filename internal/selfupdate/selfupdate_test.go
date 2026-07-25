package selfupdate

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.9.0", 1}, // numeric, not lexical
		{"2.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0}, // missing component counts as 0
		{"1.2.0", "1.2", 0},
		{"1.0.0", "1.0.0-rc1", 0}, // pre-release suffix dropped
		{"1.1.0", "1.0.0", 1},
		{"", "1.0.0", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
