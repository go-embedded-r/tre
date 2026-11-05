package tre

// Port of tre-match-parallel.c: the parallel (Thompson-style) TNFA matcher
// used when the regexp has no back references. It tracks one tag vector per
// NFA state and resolves races between paths with the tag order.

type reachEntry struct {
	state int // index of the state's first transition
	tags  []int
}

type reachPos struct {
	pos int
	ent *reachEntry
}

// runParallel runs the matcher over s. matchTags (len numTags, or nil when no
// submatch is wanted) receives the tags of the match. It returns the end
// offset of the match, or -1.
func (t *tnfa) runParallel(s []rune, matchTags []int, eflags EFlags, maxSteps int) (int, ErrCode) {
	n := len(s)
	ms := matchState{notBOL: eflags&NotBOL != 0, notEOL: eflags&NotEOL != 0, newline: t.cflags&Newline != 0}
	numTags := 0
	if matchTags != nil {
		numTags = t.numTags
	}

	// Temporary data required for matching, reused across calls.
	ns := t.numStates
	sc := t.getScratch(numTags)
	tmpTags := sc.tmp
	// the tag slices are swapped between entries and tmpTags while matching:
	// hand the one tmpTags ends with back, so that no slice is shared
	defer func() {
		sc.tmp = tmpTags
		t.putScratch(sc, numTags)
	}()
	reach, reachNext := sc.reachA, sc.reachB
	reachN, reachNextN := 0, 0
	rpos := sc.rpos
	for i := range rpos {
		rpos[i].pos = -1
	}
	_ = ns

	pos := -1
	matchEO := -1
	newMatch := false
	steps := 0
	trans := t.transitions

	getNext := func() {
		ms.prevC = ms.nextC
		pos++
		if pos >= n {
			ms.nextC = 0
		} else {
			ms.nextC = s[pos]
		}
	}

	// If only one character can start a match, find it first.
	if t.firstChar >= 0 && t.cflags&UseBytes != 0 {
		idx := -1
		fc := rune(t.firstChar)
		for i, c := range s {
			if c == fc {
				idx = i
				break
			}
		}
		if idx < 0 {
			return -1, ErrNoMatch
		}
		if idx >= 1 {
			ms.prevC = s[idx-1]
		}
		ms.nextC = s[idx]
		pos = idx
	} else {
		getNext()
		pos = 0
	}
	ms.pos = pos

	for {
		ms.pos = pos
		// If no match found yet, add the initial states to `reachNext'.
		if matchEO < 0 {
			for i := range t.initial {
				ti := &t.initial[i]
				if rpos[ti.stateID].pos < pos {
					if ti.assertions != 0 && ms.failsAssertions(ti.assertions) {
						continue
					}
					e := &reachNext[reachNextN]
					e.state = ti.next - 1
					for k := 0; k < numTags; k++ {
						e.tags[k] = -1
					}
					for _, tg := range ti.tags {
						if tg < numTags {
							e.tags[tg] = pos
						}
					}
					if e.state == t.final {
						// found empty match
						matchEO = pos
						newMatch = true
						copy(matchTags, e.tags)
					}
					rpos[ti.stateID].pos = pos
					rpos[ti.stateID].ent = e
					reachNextN++
				}
			}
		} else if numTags == 0 || reachNextN == 0 {
			// We have found a match.
			break
		}

		// Check for end of string.
		if pos >= n {
			break
		}
		getNext()
		ms.pos = pos

		// Swap `reach' and `reachNext'.
		reach, reachNext = reachNext, reach
		reachN = reachNextN

		// For each state in `reach', weed out states that don't fulfill the
		// minimal matching conditions.
		if t.numMinimals > 0 && newMatch {
			newMatch = false
			reachNextN = 0
			for ri := 0; ri < reachN; ri++ {
				ent := &reach[ri]
				skip := false
				for i := 0; i+1 < len(t.minimalTags); i += 2 {
					end := t.minimalTags[i]
					start := t.minimalTags[i+1]
					if end >= numTags {
						skip = true
						break
					} else if ent.tags[start] == matchTags[start] && ent.tags[end] < matchTags[end] {
						skip = true
						break
					}
				}
				if !skip {
					nx := &reachNext[reachNextN]
					nx.state = ent.state
					nx.tags, ent.tags = ent.tags, nx.tags
					reachNextN++
				}
			}
			// Swap `reach' and `reachNext'.
			reach, reachNext = reachNext, reach
			reachN = reachNextN
		}

		// For each state in `reach' see if there is a transition leaving with
		// the current input symbol to a state not yet in `reachNext', and
		// add the destination states to `reachNext'.
		reachNextN = 0
		for ri := 0; ri < reachN; ri++ {
			ent := &reach[ri]
			for j := ent.state; trans[j].next != 0; j++ {
				tr := &trans[j]
				steps++
				// Does this transition match the input symbol?
				if tr.codeMin <= ms.prevC && tr.codeMax >= ms.prevC {
					if tr.assertions != 0 && (ms.failsAssertions(tr.assertions) || t.failsCharClasses(tr, ms.prevC)) {
						continue
					}
					// Compute the tags after this transition.
					copy(tmpTags, ent.tags)
					for _, tg := range tr.tags {
						if tg < numTags {
							tmpTags[tg] = pos
						}
					}
					rp := &rpos[tr.stateID]
					if rp.pos < pos {
						// Found an unvisited node.
						e := &reachNext[reachNextN]
						e.state = tr.next - 1
						e.tags, tmpTags = tmpTags, e.tags
						rp.pos = pos
						rp.ent = e
						if e.state == t.final && (matchEO == -1 || (numTags > 0 && e.tags[0] <= matchTags[0])) {
							matchEO = pos
							newMatch = true
							copy(matchTags, e.tags)
						}
						reachNextN++
					} else {
						// Another path has also reached this state. We
						// choose the winner by examining the tag values for
						// both paths.
						if tagOrder(numTags, t.tagDirections, tmpTags, rp.ent.tags) {
							// The new path wins.
							old := rp.ent.tags
							rp.ent.tags = tmpTags
							if tr.next-1 == t.final {
								matchEO = pos
								newMatch = true
								copy(matchTags, tmpTags)
							}
							tmpTags = old
						}
					}
				}
			}
			if maxSteps > 0 && steps > maxSteps {
				return -1, ErrSteps
			}
		}
	}

	if matchEO >= 0 {
		return matchEO, RegOK
	}
	return -1, ErrNoMatch
}

// scratch is the working memory of one parallel match, pooled per tnfa and tag
// count (with and without submatches the sizes differ).
type scratch struct {
	reachA, reachB []reachEntry
	rpos           []reachPos
	tmp            []int
}

func (t *tnfa) getScratch(numTags int) *scratch {
	pool := &t.pools[0]
	if numTags > 0 {
		pool = &t.pools[1]
	}
	if v := pool.Get(); v != nil {
		return v.(*scratch)
	}
	ns := t.numStates
	sc := &scratch{reachA: make([]reachEntry, ns+1), reachB: make([]reachEntry, ns+1),
		rpos: make([]reachPos, ns), tmp: make([]int, numTags)}
	tagsBuf := make([]int, 2*ns*numTags)
	for i := 0; i < ns; i++ {
		sc.reachA[i].tags = tagsBuf[(2*i)*numTags : (2*i+1)*numTags : (2*i+1)*numTags]
		sc.reachB[i].tags = tagsBuf[(2*i+1)*numTags : (2*i+2)*numTags : (2*i+2)*numTags]
	}
	return sc
}

func (t *tnfa) putScratch(sc *scratch, numTags int) {
	pool := &t.pools[0]
	if numTags > 0 {
		pool = &t.pools[1]
	}
	pool.Put(sc)
}
