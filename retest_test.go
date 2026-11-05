package tre

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// parseUnits decodes the recorder's comma separated hex code points.
func parseUnits(s string) []rune {
	if s == "-" {
		return nil
	}
	var r []rune
	for _, h := range strings.Split(s, ",") {
		v, _ := strconv.ParseUint(h, 16, 32)
		r = append(r, rune(v))
	}
	return r
}

// TestRetest replays the calls upstream TRE's own regression suite
// (tests/retest.c, narrow and wide) makes, recorded from the C library by
// cmd/crefbuild, and requires the same return codes and byte-for-byte equal
// match offsets.
func TestRetest(t *testing.T) {
	for _, name := range []string{"retest-narrow.txt", "retest-wide.txt"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var re *Regexp
			var compLine string
			comps, execs, bad, excludedComps, excludedExecs := 0, 0, 0, 0, 0
			fail := func(format string, a ...any) {
				bad++
				if bad <= 20 {
					t.Errorf(format, a...)
				}
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<24)
			for sc.Scan() {
				fl := strings.Fields(sc.Text())
				switch fl[0] {
				case "C":
					comps++
					compLine = sc.Text()
					cflags, _ := strconv.Atoi(fl[2])
					want, _ := strconv.Atoi(fl[4])
					flags := Flags(cflags)
					if fl[1] == "n" {
						flags |= UseBytes
						// retest.c's EUC-JP section runs the narrow API in a
						// multibyte locale, which the port does not have:
						// excluded and counted.
						if hasHigh(parseUnits(fl[3])) {
							re = nil
							excludedComps++
							continue
						}
					}
					var err error
					re, err = CompileRunes(parseUnits(fl[3]), Options{Flags: flags})
					got := 0
					if err != nil {
						got = int(err.(ErrCode))
					}
					if got != want {
						fail("compile %s: got code %d, C %d", compLine, got, want)
						re = nil
					}
				case "E":
					execs++
					if re == nil {
						if excludedComps > 0 {
							excludedExecs++
						}
						continue
					}
					eflags, _ := strconv.Atoi(fl[2])
					nmatch, _ := strconv.Atoi(fl[3])
					want, _ := strconv.Atoi(fl[5])
					pm, err := re.Exec(parseUnits(fl[4]), nmatch, EFlags(eflags))
					got := 0
					if err != nil {
						got = int(err.(ErrCode))
					}
					if got != want {
						fail("%s\n  exec %s: got code %d, C %d", compLine, sc.Text(), got, want)
						continue
					}
					if want != 0 {
						continue
					}
					for i := 0; i < nmatch; i++ {
						so, eo, _ := strings.Cut(fl[6+i], ",")
						a, _ := strconv.Atoi(so)
						b, _ := strconv.Atoi(eo)
						if pm[i].So != a || pm[i].Eo != b {
							fail("%s\n  exec %s: group %d got %s, C %s", compLine, sc.Text(), i,
								fmt.Sprint(pm[i]), fl[6+i])
							break
						}
					}
				}
			}
			t.Logf("%s: %d compiles, %d executions, %d mismatches; excluded %d compiles / %d executions (EUC-JP multibyte locale)",
				name, comps, execs, bad, excludedComps, excludedExecs)
			wantComps, wantExecs := 0, 0
			if name == "retest-narrow.txt" {
				wantComps, wantExecs = 4, 20
			}
			if excludedComps != wantComps || excludedExecs != wantExecs {
				t.Errorf("excluded %d compiles / %d executions, expected exactly %d / %d",
					excludedComps, excludedExecs, wantComps, wantExecs)
			}
			if bad > 0 {
				t.Fail()
			}
		})
	}
}

func hasHigh(r []rune) bool {
	for _, c := range r {
		if c >= 0x80 {
			return true
		}
	}
	return false
}
