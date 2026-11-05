package tre

// Port of tre-match-backtrack.c: the backtracking TNFA matcher, used only
// when the regexp has back references (or on request, REG_BACKTRACKING_MATCHER).
// It explores every path, so it is exponential on some patterns: maxSteps
// bounds the work.

type btItem struct {
	pos     int
	state   int
	stateID int
	nextC   rune
	tags    []int
}

const maxBacktrackStack = 1 << 22

func (t *tnfa) runBacktrack(s []rune, matchTags []int, eflags EFlags, maxSteps int) (int, ErrCode) {
	n := len(s)
	ms := matchState{notBOL: eflags&NotBOL != 0, notEOL: eflags&NotEOL != 0, newline: t.cflags&Newline != 0}
	trans := t.transitions
	numTags := t.numTags
	pos := 0
	steps := 0

	var tags []int
	if numTags > 0 {
		tags = make([]int, numTags)
	}
	var pmatch []Match
	if t.numSubmatches > 0 {
		pmatch = make([]Match, t.numSubmatches)
	}
	statesSeen := make([]bool, t.numStates)

	// The backtracking stack. stack[0] is the sentinel (stack->prev == NULL).
	stack := make([]btItem, 1, 16)
	sp := 0
	push := func(state, stateID int, nextC rune) ErrCode {
		sp++
		if sp >= maxBacktrackStack {
			return ErrESpace
		}
		if sp >= len(stack) {
			stack = append(stack, btItem{tags: make([]int, numTags)})
		}
		it := &stack[sp]
		it.pos = pos
		it.state = state
		it.stateID = stateID
		it.nextC = nextC
		copy(it.tags, tags)
		return RegOK
	}

	getNext := func() {
		ms.prevC = ms.nextC
		pos++
		if pos >= n {
			ms.nextC = 0
		} else {
			ms.nextC = s[pos]
		}
	}

	matchEO := -1
	posStart := -1
	var nextCStart rune

retry:
	for {
		for i := 0; i < numTags; i++ {
			tags[i] = -1
			if matchTags != nil {
				matchTags[i] = -1
			}
		}
		for i := range statesSeen {
			statesSeen[i] = false
		}

		state := -1
		pos = posStart
		getNext()
		ms.pos = pos
		posStart = pos
		nextCStart = ms.nextC

		// Handle initial states.
		var nextTags []int
		for i := range t.initial {
			ti := &t.initial[i]
			if ti.assertions != 0 && ms.failsAssertions(ti.assertions) {
				continue
			}
			if state == -1 {
				// Start from this state.
				state = ti.next - 1
				nextTags = ti.tags
			} else {
				// Backtrack to this state.
				if st := push(ti.next-1, ti.stateID, ms.nextC); st != RegOK {
					return -1, st
				}
				for _, tg := range ti.tags {
					stack[sp].tags[tg] = pos
				}
			}
		}
		for _, tg := range nextTags {
			tags[tg] = pos
		}

		needBT := state == -1
		for {
			if !needBT {
				steps++
				if maxSteps > 0 && steps > maxSteps {
					return -1, ErrSteps
				}
				if state == t.final {
					if matchEO < pos ||
						(matchEO == pos && matchTags != nil && tagOrder(numTags, t.tagDirections, tags, matchTags)) {
						// This match wins the previous match.
						matchEO = pos
						if matchTags != nil {
							copy(matchTags, tags)
						}
					}
					// Our TNFAs never have transitions leaving from the
					// final state, so we jump right to backtracking.
					needBT = true
				}
			}
			if !needBT {
				// Go to the next character in the input string.
				tr0 := &trans[state]
				if tr0.next != 0 && tr0.assertions&assertBackref != 0 {
					// This is a back reference state. All transitions
					// leaving from this state have the same back reference
					// "assertion". Instead of reading the next character,
					// we match the back reference.
					bt := tr0.backref
					t.fillPmatch(pmatch[:bt+1], t.cflags&^NoSub, tags, pos)
					so, eo := pmatch[bt].So, pmatch[bt].Eo
					btLen := eo - so
					matched := false
					if n-pos < btLen {
						matched = false
					} else {
						matched = true
						for k := 0; k < btLen; k++ {
							if s[so+k] != s[pos+k] {
								matched = false
								break
							}
						}
					}
					if matched {
						// Back reference matched. Check for infinite loop.
						emptyBr := btLen == 0
						if emptyBr && statesSeen[tr0.stateID] {
							needBT = true
						} else {
							statesSeen[tr0.stateID] = emptyBr
							// Advance in input string and resync `prevC',
							// `nextC' and pos.
							pos += btLen - 1
							getNext()
						}
					} else {
						needBT = true
					}
				} else {
					// Check for end of string.
					if pos >= n {
						needBT = true
					} else {
						// Read the next character.
						getNext()
					}
				}
			}
			if !needBT {
				ms.pos = pos
				nextState := -1
				nextTags = nil
				for j := state; trans[j].next != 0; j++ {
					tr := &trans[j]
					steps++
					if tr.codeMin <= ms.prevC && tr.codeMax >= ms.prevC {
						if tr.assertions != 0 && (ms.failsAssertions(tr.assertions) || t.failsCharClasses(tr, ms.prevC)) {
							continue
						}
						if nextState == -1 {
							// First matching transition.
							nextState = tr.next - 1
							nextTags = tr.tags
						} else {
							// Second matching transition. We may need to
							// backtrack here to take this transition
							// instead of the first one, so we push this
							// transition in the backtracking stack so we can
							// jump back here if needed.
							if st := push(tr.next-1, tr.stateID, ms.nextC); st != RegOK {
								return -1, st
							}
							for _, tg := range tr.tags {
								stack[sp].tags[tg] = pos
							}
						}
					}
				}
				if nextState != -1 {
					// Matching transitions were found. Take the first one.
					state = nextState
					for _, tg := range nextTags {
						tags[tg] = pos
					}
				} else {
					needBT = true
				}
			}
			if needBT {
				// A matching transition was not found. Try to backtrack.
				if sp > 0 {
					it := &stack[sp]
					if trans[it.state].assertions&assertBackref != 0 {
						statesSeen[it.stateID] = false
					}
					pos = it.pos
					state = it.state
					ms.nextC = it.nextC
					copy(tags, it.tags)
					sp--
					needBT = false
				} else if matchEO < 0 {
					// Try starting from a later position in the input
					// string. Check for end of string.
					if posStart >= n {
						break retry
					}
					ms.nextC = nextCStart
					continue retry
				} else {
					break retry
				}
			}
		}
	}

	if matchEO >= 0 {
		return matchEO, RegOK
	}
	return -1, ErrNoMatch
}
