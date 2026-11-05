package tre

// Port of tre-match-utils.h and of tre_fill_pmatch (regexec.c).

// matchState carries what the CHECK_ASSERTIONS macro reads.
type matchState struct {
	pos            int
	prevC, nextC   rune
	notBOL, notEOL bool
	newline        bool
}

// failsAssertions is CHECK_ASSERTIONS: true when an assertion does not hold.
func (m *matchState) failsAssertions(a int) bool {
	return (a&assertAtBOL != 0 && (m.pos > 0 || m.notBOL) && (m.prevC != '\n' || !m.newline)) ||
		(a&assertAtEOL != 0 && (m.nextC != 0 || m.notEOL) && (m.nextC != '\n' || !m.newline)) ||
		(a&assertAtBOW != 0 && (isWordChar(m.prevC) || !isWordChar(m.nextC))) ||
		(a&assertAtEOW != 0 && (!isWordChar(m.prevC) || isWordChar(m.nextC))) ||
		(a&assertAtWB != 0 && (m.pos != 0 && m.nextC != 0 && isWordChar(m.prevC) == isWordChar(m.nextC))) ||
		(a&assertAtWBNeg != 0 && (m.pos == 0 || m.nextC == 0 || isWordChar(m.prevC) != isWordChar(m.nextC)))
}

// failsCharClasses is CHECK_CHAR_CLASSES: true when the class assertions of
// the transition do not hold for the consumed character prevC.
func (t *tnfa) failsCharClasses(tr *tnfaTrans, prevC rune) bool {
	icase := t.cflags&ICase != 0
	if tr.assertions&assertCharClass != 0 {
		if !icase {
			if !isctype(prevC, tr.class) {
				return true
			}
		} else if !isctype(toLower(prevC), tr.class) && !isctype(toUpper(prevC), tr.class) {
			return true
		}
	}
	return tr.assertions&assertCharClassNeg != 0 && negCharClassesMatch(tr.negClasses, prevC, icase)
}

func negCharClassesMatch(classes []ctype, wc rune, icase bool) bool {
	for _, cl := range classes {
		if (!icase && isctype(wc, cl)) ||
			(icase && (isctype(toUpper(wc), cl) || isctype(toLower(wc), cl))) {
			return true // Match.
		}
	}
	return false
}

// tagOrder returns true if t1 wins t2.
func tagOrder(numTags int, dirs []int, t1, t2 []int) bool {
	for i := 0; i < numTags; i++ {
		if dirs[i] == tagMinimize {
			if t1[i] < t2[i] {
				return true
			}
			if t1[i] > t2[i] {
				return false
			}
		} else {
			if t1[i] > t2[i] {
				return true
			}
			if t1[i] < t2[i] {
				return false
			}
		}
	}
	return false
}

// Match is one submatch: the offsets are in code units of the matched input
// (characters, or bytes in byte mode); -1 means the group did not take part.
type Match struct{ So, Eo int }

// fillPmatch fills the POSIX.2 regmatch array according to the TNFA tag and
// match endpoint values (tre_fill_pmatch).
func (t *tnfa) fillPmatch(pmatch []Match, cflags Flags, tags []int, matchEO int) {
	nmatch := len(pmatch)
	i := 0
	if matchEO >= 0 && cflags&NoSub == 0 {
		// Construct submatch offsets from the tags.
		for i < t.numSubmatches && i < nmatch {
			sd := &t.submatchData[i]
			if sd.soTag == t.endTag {
				pmatch[i].So = matchEO
			} else {
				pmatch[i].So = tags[sd.soTag]
			}
			if sd.eoTag == t.endTag {
				pmatch[i].Eo = matchEO
			} else {
				pmatch[i].Eo = tags[sd.eoTag]
			}
			// If either of the endpoints were not used, this submatch was
			// not part of the match.
			if pmatch[i].So == -1 || pmatch[i].Eo == -1 {
				pmatch[i].So, pmatch[i].Eo = -1, -1
			}
			i++
		}
		// Reset all submatches that are not within all of their parent
		// submatches.
		i = 0
		for i < t.numSubmatches && i < nmatch {
			for _, p := range t.submatchData[i].parents {
				if p < nmatch && (pmatch[i].So < pmatch[p].So || pmatch[i].Eo > pmatch[p].Eo) {
					pmatch[i].So, pmatch[i].Eo = -1, -1
				}
			}
			i++
		}
	}
	for i < nmatch {
		pmatch[i] = Match{-1, -1}
		i++
	}
}
