package tre

import "testing"

// FuzzCompileExec checks that no pattern/subject/flag combination panics or
// runs away: every outcome is a match, no match, or an error. The corpus in
// testdata/fuzz is replayed by every `go test`.
func FuzzCompileExec(f *testing.F) {
	for _, s := range []struct {
		p, s string
		fl   uint8
	}{
		{"a+b", "aab", 1}, {"(a|ab)(c|bcd)(d*)", "abcd", 1}, {`(a*)\1`, "aaaa", 1},
		{"a+?", "aaa", 1}, {"[[:alpha:]]+", "héllo", 1}, {`\<w\>`, "a w b", 1},
		{"((a{3}){3}){3}", "a", 1}, {"(?i)ab", "AB", 1}, {"[^a-c]{1,3}", "xyz", 1},
		{"a{,3}", "aaaa", 1}, {"\\Qa.b\\E", "a.b", 1}, {"(", "", 1}, {"[", "", 1},
		{"a|b|", "", 1}, {"^$", "\n", 5}, {"x*", "", 8},
	} {
		f.Add(s.p, s.s, s.fl)
	}
	lim := Limits{MaxPattern: 4096, MaxNodes: 1 << 15, MaxStates: 1 << 13, MaxTransitions: 1 << 18, MaxSetItems: 1 << 18, MaxSteps: 1 << 20}
	f.Fuzz(func(t *testing.T, pattern, subject string, fl uint8) {
		if len(subject) > 512 {
			return
		}
		flags := Flags(fl) & (Extended | ICase | Newline | NoSub | Literal | RightAssoc | Ungreedy | UseBytes)
		for _, rc := range []bool{false, true} {
			re, err := CompileOptions(pattern, Options{Flags: flags, RCompat: rc, Limits: lim})
			if err != nil {
				continue
			}
			for _, e := range []EFlags{0, NotBOL | NotEOL, BacktrackingMatcher} {
				pm, err := re.ExecString(subject, 4, e)
				if err != nil {
					continue
				}
				for _, m := range pm {
					if m.So > m.Eo || (m.So < -1) {
						t.Fatalf("bad match %v for %q on %q", m, pattern, subject)
					}
				}
			}
		}
	})
}
