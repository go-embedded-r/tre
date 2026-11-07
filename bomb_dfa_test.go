package tre

import (
	"math/rand"
	"testing"
)

// TestBombDFA: (a|b)*a(a|b){20}c has a DFA of 2^20 states. The lazy DFA must
// stay within its caps (dfaMaxStates, dfaMaxItems) and hand over to the NFA
// matcher with the answer unchanged. CI runs it with the other bombs under
// GOMEMLIMIT and ulimit -v.
func TestBombDFA(t *testing.T) {
	re, err := Compile(`(a|b)*a(a|b){20}c`, Extended)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(1))
	buf := make([]byte, 6000)
	for i := range buf {
		buf[i] = "ab"[r.Intn(2)]
	}
	if re.MatchString(string(buf)) {
		t.Fatal("matched a subject without c")
	}
	if !re.MatchString(string(buf) + "abbbbbbbbbbbbbbbbbbbbc") {
		t.Fatal("missed a match")
	}
	d := &re.t.dfa
	if len(d.states) > dfaMaxStates || d.items > dfaMaxItems {
		t.Fatalf("DFA cache unbounded: %d states, %d items", len(d.states), d.items)
	}
	if !d.full.Load() {
		t.Fatal("the cap was not reached: the test no longer exercises the bound")
	}
	for i := 0; i < 50; i++ {
		x := []rune(string(buf[i : i+40]))
		a, _ := re.Exec(x, 0, 0)
		b, _ := re.Exec(x, 0, BacktrackingMatcher)
		if (a == nil) != (b == nil) {
			t.Fatalf("DFA/NFA disagree on %q", string(x))
		}
	}
}
