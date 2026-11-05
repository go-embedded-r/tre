package tre

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-embedded-r/tre/internal/cref"
)

// startHarness returns a running differential harness (upstream TRE built as
// C), or skips: the binary is built by cmd/crefbuild and located by
// TRE_C_HARNESS. TRE_REQUIRE_ORACLE=1 turns the skip into a failure.
func startHarness(t testing.TB) *cref.Harness {
	t.Helper()
	bin := os.Getenv("TRE_C_HARNESS")
	if bin == "" {
		if os.Getenv("TRE_REQUIRE_ORACLE") != "" {
			t.Fatal("TRE_C_HARNESS is not set")
		}
		t.Skip("TRE_C_HARNESS not set (build it with go run ./cmd/crefbuild)")
	}
	h, err := cref.Start(filepath.Clean(bin))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// genState builds random patterns.
type genState struct{ r *rand.Rand }

func (g *genState) pick(s ...string) string { return s[g.r.Intn(len(s))] }

func (g *genState) atom(depth int, ngroups *int) string {
	switch k := g.r.Intn(24); {
	case k < 6:
		return g.pick("a", "b", "c", "a", "b", "x", "é", "A", "_", " ", "-", "1", "\n")
	case k < 7:
		return "."
	case k < 10:
		if g.r.Intn(12) == 0 {
			return g.pick("[b-a]", "[[:foo:]]", "[a", "[[.a.]]", "[[=a=]]", "[a-c-e]")
		}
		return g.pick("[ab]", "[^a]", "[a-c]", "[[:alpha:]]", "[^[:space:]]", "[[:upper:][:digit:]]",
			"[]a]", "[^]a]", "[a-]", "[[:alnum:]_]", "[^a-c]", "[ab-]", "[é]", "[[:lower:]a]", "[^[:alpha:]b]")
	case k < 13 && depth > 0:
		*ngroups++
		return "(" + g.expr(depth-1, ngroups) + ")"
	case k < 14 && depth > 0:
		return "(?:" + g.expr(depth-1, ngroups) + ")"
	case k < 15:
		return g.pick("^", "$", "\\<", "\\>", "\\b", "\\B")
	case k < 17:
		return g.pick("\\w", "\\W", "\\d", "\\D", "\\s", "\\S", "\\t", "\\n", "\\.", "\\*", "\\x61", "\\x{e9}")
	case k < 19:
		return g.pick("\\1", "\\2", "\\3", "\\1", "\\1")
	case k < 20:
		return g.pick("(?i)", "(?n)", "(?U)", "(?-i)", "(?i:", "(?#c)", "\\Qa.b\\E", "\\Q*")
	default:
		return g.pick("a", "b", "c", "ab", "abc")
	}
}

func (g *genState) piece(depth int, ngroups *int) string {
	a := g.atom(depth, ngroups)
	for g.r.Intn(4) == 0 {
		if g.r.Intn(12) == 0 {
			a += g.pick("{2,1}", "{", "{a}", "{300}", "{1,256}", "{2", "{,}", "**", "+*", "{-}", "{~1}", "{1i<2}")
			continue
		}
		a += g.pick("*", "+", "?", "*?", "+?", "??", "{2}", "{1,}", "{0,2}", "{1,3}", "{2,3}?", "{,2}", "{0}",
			"{1,2}{2}", "{0,1}", "{3}")
	}
	return a
}

func (g *genState) branch(depth int, ngroups *int) string {
	n := 1 + g.r.Intn(3)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(g.piece(depth, ngroups))
	}
	return sb.String()
}

func (g *genState) expr(depth int, ngroups *int) string {
	s := g.branch(depth, ngroups)
	for g.r.Intn(4) == 0 {
		s += "|" + g.branch(depth, ngroups)
	}
	return s
}

func (g *genState) pattern() string {
	if g.r.Intn(10) == 0 {
		// raw noise
		n := g.r.Intn(8)
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(g.pick("(", ")", "[", "]", "{", "}", "\\", "*", "?", "+", "|", "^", "$", "a", "b", ".", "-", ":", "1", ","))
		}
		return sb.String()
	}
	n := 0
	return g.expr(3, &n)
}

func (g *genState) subject() string {
	n := g.r.Intn(11)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(g.pick("a", "b", "c", "a", "b", "A", "B", " ", "_", "-", "1", "é", "\n", "x", "ab", "aa"))
	}
	return sb.String()
}

func (g *genState) flags() int {
	f := 0
	if g.r.Intn(8) != 0 {
		f |= int(Extended)
	}
	if g.r.Intn(4) == 0 {
		f |= int(ICase)
	}
	if g.r.Intn(4) == 0 {
		f |= int(Newline)
	}
	if g.r.Intn(12) == 0 {
		f |= int(NoSub)
	}
	if g.r.Intn(20) == 0 {
		f |= int(Literal)
	}
	if g.r.Intn(12) == 0 {
		f |= int(RightAssoc)
	}
	if g.r.Intn(8) == 0 {
		f |= int(Ungreedy)
	}
	return f
}

func (g *genState) eflags() int {
	e := 0
	if g.r.Intn(8) == 0 {
		e |= int(NotBOL)
	}
	if g.r.Intn(8) == 0 {
		e |= int(NotEOL)
	}
	if g.r.Intn(6) == 0 {
		e |= int(BacktrackingMatcher)
	}
	return e
}

// goResult runs the port the way the harness does, in the harness' format.
func goResult(mode byte, cflags, eflags, nmatch int, pattern, subject string) cref.Result {
	flags := Flags(cflags)
	if mode == 'b' {
		flags |= UseBytes
	}
	re, err := CompileOptions(pattern, Options{Flags: flags, Limits: Limits{MaxSteps: 1 << 24}})
	if err != nil {
		return cref.Result{Err: int(err.(ErrCode))}
	}
	pm, err := re.ExecString(subject, nmatch, EFlags(eflags))
	if err != nil {
		if err == ErrNoMatch {
			return cref.Result{NoMatch: true}
		}
		return cref.Result{Err: -int(err.(ErrCode))}
	}
	r := cref.Result{Nsub: re.NumSubexp()}
	for _, m := range pm {
		r.Match = append(r.Match, [2]int{m.So, m.Eo})
	}
	return r
}

func sameResult(a, b cref.Result) bool {
	if a.Err != b.Err || a.NoMatch != b.NoMatch || a.Nsub != b.Nsub || len(a.Match) != len(b.Match) {
		return false
	}
	for i := range a.Match {
		if a.Match[i] != b.Match[i] {
			return false
		}
	}
	return true
}

// TestDifferential compares the port with upstream TRE (C) on random
// patterns, subjects and flags, in wide and byte mode. Every disagreement is
// a failure; cases on which the C harness itself died (undefined behaviour in
// the C code) are counted and reported, never silently dropped.
func TestDifferential(t *testing.T) {
	h := startHarness(t)
	defer func() { h.Close() }()
	n := 20000
	if s := os.Getenv("TRE_FUZZ_N"); s != "" {
		n, _ = strconv.Atoi(s)
	}
	seed := int64(1)
	if s := os.Getenv("TRE_FUZZ_SEED"); s != "" {
		seed, _ = strconv.ParseInt(s, 10, 64)
	}
	g := &genState{r: rand.New(rand.NewSource(seed))}
	bad, died, compared, compErrs, matches, approx, steps := 0, 0, 0, 0, 0, 0, 0
	for i := 0; i < n; i++ {
		mode := byte('w')
		if g.r.Intn(3) == 0 {
			mode = 'b'
		}
		cflags, eflags := g.flags(), g.eflags()
		nmatch := g.r.Intn(6)
		pat, sub := g.pattern(), g.subject()
		want, err := h.Do(mode, cflags, eflags, nmatch, []byte(pat), []byte(sub))
		if err != nil {
			died++
			h.Close()
			var e error
			h, e = cref.Start(os.Getenv("TRE_C_HARNESS"))
			if e != nil {
				t.Fatal(e)
			}
			continue
		}
		got := goResult(mode, cflags, eflags, nmatch, pat, sub)
		if got.Err == int(ErrApprox) {
			approx++ // approximate matching syntax: not ported, counted
			continue
		}
		if got.Err == -int(ErrSteps) {
			steps++ // exponential backtracking refused by the step limit: counted
			continue
		}
		compared++
		if want.Err != 0 {
			compErrs++
		} else if !want.NoMatch {
			matches++
		}
		if !sameResult(got, want) {
			bad++
			if bad <= 25 {
				t.Errorf("mode %c cflags %d eflags %d nmatch %d pattern %q subject %q:\n  go %+v\n  C  %+v",
					mode, cflags, eflags, nmatch, pat, sub, got, want)
			}
		}
	}
	t.Log(fmt.Sprintf("differential: %d compared (%d compile errors, %d matches), %d mismatches; excluded: %d approximate-matching syntax, %d step limit, %d C harness died",
		compared, compErrs, matches, bad, approx, steps, died))
	if bad > 0 {
		t.Fail()
	}
}
