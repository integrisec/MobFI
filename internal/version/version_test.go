package version

import "testing"

func TestString(t *testing.T) {
	orig := Version
	origC, origD := Commit, Date
	t.Cleanup(func() { Version, Commit, Date = orig, origC, origD })

	Version = "1.2.3"
	cases := []struct {
		commit, date, want string
	}{
		{"", "", "v1.2.3"},
		{"abc1234", "", "v1.2.3 (abc1234)"},
		{"", "2026-07-24", "v1.2.3 (2026-07-24)"},
		{"abc1234", "2026-07-24", "v1.2.3 (abc1234, 2026-07-24)"},
	}
	for _, c := range cases {
		Commit, Date = c.commit, c.date
		if got := String(); got != c.want {
			t.Errorf("commit=%q date=%q: String()=%q, want %q", c.commit, c.date, got, c.want)
		}
	}
}
