package tre

import (
	"sort"
	"sync"
	"sync/atomic"
)

// A lazily built DFA for the yes/no question (Exec with nmatch 0), which is
// what R's grepl asks of TRE. It is a subset construction over the TNFA that
// the parallel matcher walks: the state of the DFA is the set of NFA states the
// parallel matcher would hold at a position, with the initial states added
// (they are added at every position until a match is found, and the question
// ends at the first match). It applies when no transition depends on the
// position (no ^ $ \< \> \b \B assertion): the character class assertions only
// look at the consumed character. Characters above 255 are not tabled; a
// subject that has one is answered by the NFA matcher.

const dfaMaxStates = 2048

type dstate struct {
	set    []int
	accept bool
	next   [256]atomic.Pointer[dstate]
}

type dfaCache struct {
	mu     sync.Mutex
	ok     bool
	start  *dstate
	states map[string]*dstate
}

// dfaUsable reports whether the TNFA has no position dependent assertion.
func (t *tnfa) dfaUsable() bool {
	const positional = assertAtBOL | assertAtEOL | assertAtBOW | assertAtEOW | assertAtWB | assertAtWBNeg | assertBackref
	if t.haveBackrefs {
		return false
	}
	for i := range t.initial {
		if t.initial[i].assertions != 0 {
			return false
		}
	}
	for i := range t.transitions {
		if t.transitions[i].next != 0 && t.transitions[i].assertions&positional != 0 {
			return false
		}
	}
	return true
}

func (t *tnfa) dfaInit() {
	t.dfaOnce.Do(func() {
		if !t.dfaUsable() {
			return
		}
		t.dfa.ok = true
		t.dfa.states = map[string]*dstate{}
		var set []int
		for i := range t.initial {
			set = append(set, t.initial[i].next-1)
		}
		t.dfa.start = t.dfaIntern(set)
	})
}

// dfaIntern returns the state for set (caller holds the lock or is the
// initialiser). A nil result means the table is full.
func (t *tnfa) dfaIntern(set []int) *dstate {
	sort.Ints(set)
	n := 0
	for i, v := range set {
		if i == 0 || v != set[i-1] {
			set[n] = v
			n++
		}
	}
	set = set[:n]
	key := make([]byte, 0, 4*n)
	for _, v := range set {
		key = append(key, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	if d, ok := t.dfa.states[string(key)]; ok {
		return d
	}
	if len(t.dfa.states) >= dfaMaxStates {
		return nil
	}
	d := &dstate{set: set}
	for _, v := range set {
		if v == t.final {
			d.accept = true
		}
	}
	t.dfa.states[string(key)] = d
	return d
}

// dfaStep computes (and caches) the successor of d on code c < 256.
func (t *tnfa) dfaStep(d *dstate, c rune) *dstate {
	t.dfa.mu.Lock()
	defer t.dfa.mu.Unlock()
	if nx := d.next[c].Load(); nx != nil {
		return nx
	}
	var set []int
	for _, st := range d.set {
		for j := st; t.transitions[j].next != 0; j++ {
			tr := &t.transitions[j]
			if tr.codeMin <= c && tr.codeMax >= c {
				if tr.assertions != 0 && t.failsCharClasses(tr, c) {
					continue
				}
				set = append(set, tr.next-1)
			}
		}
	}
	for i := range t.initial {
		set = append(set, t.initial[i].next-1)
	}
	nx := t.dfaIntern(set)
	if nx != nil {
		d.next[c].Store(nx)
	}
	return nx
}

// dfaMatch answers whether the subject matches. done is false when the DFA
// cannot answer (not usable, table full, or a character above 255): the caller
// then runs the NFA matcher.
func (t *tnfa) dfaMatchRunes(s []rune) (match, done bool) {
	t.dfaInit()
	if !t.dfa.ok {
		return false, false
	}
	d := t.dfa.start
	if d.accept {
		return true, true
	}
	for _, c := range s {
		if c >= 256 || c < 0 {
			return false, false
		}
		nx := d.next[c].Load()
		if nx == nil {
			if nx = t.dfaStep(d, c); nx == nil {
				return false, false
			}
		}
		d = nx
		if d.accept {
			return true, true
		}
	}
	return false, true
}

// dfaMatchBytes is dfaMatchRunes over the bytes of s (byte mode, or an ASCII
// subject in either mode).
func (t *tnfa) dfaMatchBytes(s string) (match, done bool) {
	t.dfaInit()
	if !t.dfa.ok {
		return false, false
	}
	d := t.dfa.start
	if d.accept {
		return true, true
	}
	for i := 0; i < len(s); i++ {
		nx := d.next[s[i]].Load()
		if nx == nil {
			if nx = t.dfaStep(d, rune(s[i])); nx == nil {
				return false, false
			}
		}
		d = nx
		if d.accept {
			return true, true
		}
	}
	return false, true
}
