package tre

import "testing"

// TestRCompat pins the behaviours of R 4.6.2's copy of TRE that differ from
// upstream; each expectation was read from R (grepl/regexpr) in the oracle.
func TestRCompat(t *testing.T) {
	type tc struct {
		pat, in string
		flags   Flags
		upMatch bool // upstream TRE
		rMatch  bool // R
		upErr   ErrCode
		rErr    ErrCode
	}
	cases := []tc{
		{pat: `^a{,3}$`, in: "aaaa", flags: Extended, upMatch: false, rMatch: true},
		{pat: `^a{1,99999999999}$`, in: "aaa", flags: Extended, upErr: ErrBadMax, rMatch: true},
		{pat: `a{256}`, in: "a", flags: Extended, upErr: ErrBadMax, rErr: ErrBadBR},
		{pat: `b[^[:space:]]{1,3}`, in: "ab ab", flags: Extended, upMatch: false, rMatch: true},
		{pat: `([abc]+)[[:alpha:]]*\1`, in: "banana", flags: Extended | UseBytes, upMatch: true, rMatch: false},
	}
	for _, c := range cases {
		for _, r := range []bool{false, true} {
			re, err := CompileOptions(c.pat, Options{Flags: c.flags, RCompat: r})
			wantErr, wantMatch := c.upErr, c.upMatch
			if r {
				wantErr, wantMatch = c.rErr, c.rMatch
			}
			if err != nil {
				if err != wantErr {
					t.Errorf("%q rcompat=%v: error %v, want %v", c.pat, r, err, wantErr)
				}
				continue
			}
			if wantErr != RegOK {
				t.Errorf("%q rcompat=%v: compiled, want error %v", c.pat, r, wantErr)
				continue
			}
			if got := re.MatchString(c.in); got != wantMatch {
				t.Errorf("%q on %q rcompat=%v: match %v, want %v", c.pat, c.in, r, got, wantMatch)
			}
		}
	}
}
