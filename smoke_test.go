package tre

import "testing"

func TestSmoke(t *testing.T) {
	cases := []struct {
		pat, in string
		want    []Match
	}{
		{"a+", "baac", []Match{{1, 3}}},
		{"(a|ab)(c|bcd)(d*)", "abcd", []Match{{0, 4}, {0, 1}, {1, 4}, {4, 4}}},
		{"a+?", "aaa", []Match{{0, 1}}},
		{"(a*)b\\1", "xaabaay", []Match{{1, 6}, {1, 3}}},
		{"\\<foo\\>", "a foo b", []Match{{2, 5}}},
	}
	for _, c := range cases {
		re, err := Compile(c.pat, Extended)
		if err != nil {
			t.Fatalf("%q: %v", c.pat, err)
		}
		got, err := re.ExecString(c.in, len(c.want), 0)
		if err != nil {
			t.Fatalf("%q on %q: %v", c.pat, c.in, err)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%q on %q: got %v want %v", c.pat, c.in, got, c.want)
				break
			}
		}
	}
}
