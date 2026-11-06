package tre

import (
	"strings"
	"testing"
	"time"
)

// The bomb tests assert a REFUSAL: a hostile pattern or subject must make the
// engine return an error within the limits, not exhaust memory or time. CI
// runs them under GOMEMLIMIT and ulimit -v.

func mustRefuse(t *testing.T, name, pattern string, flags Flags, want ErrCode) {
	t.Helper()
	start := time.Now()
	_, err := CompileOptions(pattern, Options{Flags: flags})
	if err != want {
		t.Errorf("%s: compile error %v, want refusal %v", name, err, want)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("%s: refusal took %v", name, d)
	}
}

func TestBombPatternTooLong(t *testing.T) {
	mustRefuse(t, "70000 literals", strings.Repeat("a", 70000), Extended, ErrESpace)
}

func TestBombNestedRepetition(t *testing.T) {
	// ((((a{255}){255}){255}){255}) expands to 255^4 copies.
	mustRefuse(t, "nested counts", "((((a{255}){255}){255}){255})", Extended, ErrESpace)
	mustRefuse(t, "union of nested counts", "(((a{255}|b){255}|c){255}|d){255}", Extended, ErrESpace)
}

func TestBombQuadraticSets(t *testing.T) {
	// a?^n a^n: the firstpos/lastpos sets grow quadratically.
	n := 30000
	mustRefuse(t, "a?^n", strings.Repeat("a?", n), Extended, ErrESpace)
	mustRefuse(t, "alternation squares", "("+strings.Repeat("a|", 20000)+"b)("+strings.Repeat("c|", 20000)+"d)", Extended, ErrESpace)
}

func TestBombDeepNesting(t *testing.T) {
	// 30000 nested groups: the parser's explicit stack must hold, the
	// result is either a program or a refusal, never a stack overflow.
	p := strings.Repeat("(", 30000) + "a" + strings.Repeat(")", 30000)
	_, err := CompileOptions(p, Options{Flags: Extended})
	if err != nil && err != ErrESpace {
		t.Errorf("deep nesting: %v", err)
	}
}

func TestBombBacktrackingSteps(t *testing.T) {
	// A back reference forces the exhaustive backtracking matcher: on this
	// input it is exponential. The step limit must refuse it.
	re, err := CompileOptions(`(a*)*\1b`, Options{Flags: Extended, Limits: Limits{MaxSteps: 1 << 22}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = re.ExecString(strings.Repeat("a", 60), 1, 0)
	if err != ErrSteps {
		t.Errorf("exec error %v, want %v", err, ErrSteps)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("refusal took %v", d)
	}
}

func TestBombDefaultStepLimit(t *testing.T) {
	// With default limits the same input is refused too (bounded time).
	re, err := Compile(`(a|aa)+\1$`, Extended)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = re.ExecString(strings.Repeat("a", 200)+"b", 1, 0)
	if err != ErrSteps && err != ErrNoMatch {
		t.Errorf("exec error %v", err)
	}
	if d := time.Since(start); d > 60*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestBombLargeSubject(t *testing.T) {
	// A large subject is linear for the parallel matcher.
	re, err := Compile(`(a|b)*c`, Extended)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Repeat("ab", 500000)
	start := time.Now()
	_, err = re.ExecString(s, 2, 0)
	if err != ErrNoMatch {
		t.Errorf("exec error %v", err)
	}
	if d := time.Since(start); d > 60*time.Second {
		t.Errorf("took %v", d)
	}
}
