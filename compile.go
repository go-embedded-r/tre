package tre

// Port of tre-compile.c: tag insertion (tre_add_tags), AST expansion of
// bounded repetition (tre_expand_ast), firstpos/lastpos computation
// (tre_compute_npfl) and the conversion of the AST to a tagged NFA.

// tnfaTrans is TRE's tre_tnfa_transition_t. A state is the run of
// transitions starting at some index of tnfa.transitions; the run ends at the
// first transition whose next is 0. next is the index of the destination
// state plus one (0 is the NULL state pointer of the C code).
type tnfaTrans struct {
	codeMin, codeMax rune
	next             int
	stateID          int
	tags             []int
	assertions       int
	class            ctype
	backref          int
	negClasses       []ctype
}

type submatchData struct {
	soTag, eoTag int
	parents      []int
}

type tnfa struct {
	transitions   []tnfaTrans
	initial       []tnfaTrans
	final         int // index of the final state
	submatchData  []submatchData
	firstChar     int
	numSubmatches int
	tagDirections []int
	minimalTags   []int // pairs (end, start)
	numTags       int
	numMinimals   int
	endTag        int
	numStates     int
	cflags        Flags
	haveBackrefs  bool
	rcompat       bool
}

type addTagsSym int

const (
	addtagsRecurse addTagsSym = iota
	addtagsAfterIteration
	addtagsAfterUnionLeft
	addtagsAfterUnionRight
	addtagsAfterCatLeft
	addtagsAfterCatRight
	addtagsSetSubmatchEnd
)

// addTagLeft inserts a catenation node at the root of n: the left child is a
// new tag, the right child is the old root.
func (c *compiler) addTagLeft(n *node, tag int) ErrCode {
	t := c.newLiteral(cTag, tag)
	if t == nil {
		return ErrESpace
	}
	if c.newNode(nLiteral) == nil { // accounts for the moved root
		return ErrESpace
	}
	right := &node{body: n.body, nullable: -1, submatchID: -1}
	n.body = body{typ: nCatenation, left: t, right: right, position: -1}
	return RegOK
}

// addTagRight is the mirror of addTagLeft: the tag is the right child.
func (c *compiler) addTagRight(n *node, tag int) ErrCode {
	t := c.newLiteral(cTag, tag)
	if t == nil {
		return ErrESpace
	}
	if c.newNode(nLiteral) == nil {
		return ErrESpace
	}
	left := &node{body: n.body, nullable: -1, submatchID: -1}
	n.body = body{typ: nCatenation, left: left, right: t, position: -1}
	return RegOK
}

// addTags adds tags to the parse tree so that subexpressions marked for
// submatch addressing can be traced. With firstPass it only counts the tags.
func (c *compiler) addTags(tree *node, t *tnfa, firstPass bool) ErrCode {
	status := RegOK
	n := tree
	bottom := len(c.stack)
	numTags := 0
	numMinimals := 0
	tag := 0
	nextTag := 1
	minimalTag := -1
	direction := tagMinimize

	if !firstPass {
		t.endTag = 0
		t.minimalTags = t.minimalTags[:0]
	}

	// regset: -1 terminated lists, the visible part starting at rb.
	regset := make([]int, (t.numSubmatches+1)*2+2)
	regset[0] = -1
	rb := 0
	parents := make([]int, t.numSubmatches+2)
	parents[0] = -1

	rsLen := func() int {
		i := 0
		for regset[rb+i] >= 0 {
			i++
		}
		return i
	}
	purge := func(tg int) {
		for i := 0; regset[rb+i] >= 0; i++ {
			id := regset[rb+i] / 2
			start := regset[rb+i]%2 == 0
			if start {
				t.submatchData[id].soTag = tg
			} else {
				t.submatchData[id].eoTag = tg
			}
		}
		regset[rb] = -1
	}
	addMinimal := func(tg int) {
		t.minimalTags = append(t.minimalTags, tg, minimalTag)
		minimalTag = -1
		numMinimals++
	}

	if st := c.pushNode(n); st != RegOK {
		return st
	}
	if st := c.pushInt(int(addtagsRecurse)); st != RegOK {
		return st
	}

	for status == RegOK && len(c.stack) > bottom {
		symbol := addTagsSym(c.popInt())
		switch symbol {
		case addtagsSetSubmatchEnd:
			id := c.popInt()
			// Add end of this submatch to regset.
			i := rsLen()
			regset[rb+i] = id*2 + 1
			regset[rb+i+1] = -1
			// Pop this submatch from the parents stack.
			i = 0
			for parents[i] >= 0 {
				i++
			}
			parents[i-1] = -1

		case addtagsRecurse:
			n = c.popNode()
			if n.submatchID >= 0 {
				id := n.submatchID
				// Add start of this submatch to regset.
				i := rsLen()
				regset[rb+i] = id * 2
				regset[rb+i+1] = -1
				if !firstPass {
					i = 0
					for parents[i] >= 0 {
						i++
					}
					t.submatchData[id].parents = nil
					if i > 0 {
						p := make([]int, i)
						copy(p, parents[:i])
						t.submatchData[id].parents = p
					}
				}
				// Add end of this submatch to regset after processing this
				// node.
				if st := c.pushInt(n.submatchID); st != RegOK {
					return st
				}
				if st := c.pushInt(int(addtagsSetSubmatchEnd)); st != RegOK {
					return st
				}
			}

			switch n.typ {
			case nLiteral:
				if !isSpecial(&n.body) || n.codeMin == cBackref {
					if regset[rb] >= 0 {
						// Regset is not empty, so add a tag before the
						// literal or backref.
						if !firstPass {
							status = c.addTagLeft(n, tag)
							t.tagDirections[tag] = direction
							if minimalTag >= 0 {
								addMinimal(tag)
							}
							purge(tag)
						} else {
							n.numTags = 1
						}
						regset[rb] = -1
						tag = nextTag
						numTags++
						nextTag++
					}
				}
			case nCatenation:
				left, right := n.left, n.right
				reservedTag := -1
				// After processing right child.
				c.pushNode(n)
				c.pushInt(int(addtagsAfterCatRight))
				// Process right child.
				c.pushNode(right)
				c.pushInt(int(addtagsRecurse))
				// After processing left child.
				c.pushInt(nextTag + left.numTags)
				if left.numTags > 0 && right.numTags > 0 {
					// Reserve the next tag to the right child.
					reservedTag = nextTag
					nextTag++
				}
				c.pushInt(reservedTag)
				c.pushInt(int(addtagsAfterCatLeft))
				// Process left child.
				c.pushNode(left)
				if st := c.pushInt(int(addtagsRecurse)); st != RegOK {
					return st
				}
			case nIteration:
				arg := n.arg
				minimal := n.minimal
				if firstPass {
					v := 0
					if regset[rb] >= 0 || minimal {
						v = 1
					}
					c.pushInt(v)
				} else {
					c.pushInt(tag)
					v := 0
					if minimal {
						v = 1
					}
					c.pushInt(v)
				}
				c.pushNode(n)
				c.pushInt(int(addtagsAfterIteration))
				c.pushNode(arg)
				if st := c.pushInt(int(addtagsRecurse)); st != RegOK {
					return st
				}
				// Regset is not empty, so add a tag here.
				if regset[rb] >= 0 || minimal {
					if !firstPass {
						status = c.addTagLeft(n, tag)
						if minimal {
							t.tagDirections[tag] = tagMaximize
						} else {
							t.tagDirections[tag] = direction
						}
						if minimalTag >= 0 {
							addMinimal(tag)
						}
						purge(tag)
					}
					regset[rb] = -1
					tag = nextTag
					numTags++
					nextTag++
				}
				direction = tagMinimize
			case nUnion:
				left, right := n.left, n.right
				var leftTag, rightTag int
				if regset[rb] >= 0 {
					leftTag = nextTag
					rightTag = nextTag + 1
				} else {
					leftTag = tag
					rightTag = nextTag
				}
				// After processing right child.
				c.pushInt(rightTag)
				c.pushInt(leftTag)
				c.pushInt(rb) // the regset pointer
				v := 0
				if regset[rb] >= 0 {
					v = 1
				}
				c.pushInt(v)
				c.pushNode(n)
				c.pushNode(right)
				c.pushNode(left)
				c.pushInt(int(addtagsAfterUnionRight))
				// Process right child.
				c.pushNode(right)
				c.pushInt(int(addtagsRecurse))
				// After processing left child.
				c.pushInt(int(addtagsAfterUnionLeft))
				// Process left child.
				c.pushNode(left)
				if st := c.pushInt(int(addtagsRecurse)); st != RegOK {
					return st
				}
				// Regset is not empty, so add a tag here.
				if regset[rb] >= 0 {
					if !firstPass {
						status = c.addTagLeft(n, tag)
						t.tagDirections[tag] = direction
						if minimalTag >= 0 {
							addMinimal(tag)
						}
						purge(tag)
					}
					regset[rb] = -1
					tag = nextTag
					numTags++
					nextTag++
				}
				if n.numSubmatches > 0 {
					// The next two tags are reserved for markers.
					nextTag++
					tag = nextTag
					nextTag++
				}
			}

			if n.submatchID >= 0 {
				// Push this submatch on the parents stack.
				i := 0
				for parents[i] >= 0 {
					i++
				}
				parents[i] = n.submatchID
				parents[i+1] = -1
			}

		case addtagsAfterIteration:
			minimal := false
			enterTag := 0
			n = c.popNode()
			if firstPass {
				n.numTags = n.arg.numTags + c.popInt()
				minimalTag = -1
			} else {
				minimal = c.popInt() != 0
				enterTag = c.popInt()
				if minimal {
					minimalTag = enterTag
				}
			}
			if !firstPass {
				if minimal {
					direction = tagMinimize
				} else {
					direction = tagMaximize
				}
			}

		case addtagsAfterCatLeft:
			newTag := c.popInt()
			nextTag = c.popInt()
			if newTag >= 0 {
				tag = newTag
			}

		case addtagsAfterCatRight:
			n = c.popNode()
			if firstPass {
				n.numTags = n.left.numTags + n.right.numTags
			}

		case addtagsAfterUnionLeft:
			// Lift the bottom of the `regset' array so that when processing
			// the right operand the items currently in the array are
			// invisible. The original bottom was saved at the union and is
			// restored at addtagsAfterUnionRight.
			for regset[rb] >= 0 {
				rb++
			}

		case addtagsAfterUnionRight:
			left := c.popNode()
			right := c.popNode()
			n = c.popNode()
			addedTags := c.popInt()
			if firstPass {
				extra := 0
				if n.numSubmatches > 0 {
					extra = 2
				}
				n.numTags = n.left.numTags + n.right.numTags + addedTags + extra
			}
			rb = c.popInt()
			tagLeft := c.popInt()
			tagRight := c.popInt()
			// Add tags after both children, the left child gets a smaller
			// tag than the right child. This guarantees that we prefer the
			// left child over the right child.
			if n.numSubmatches > 0 {
				if !firstPass {
					status = c.addTagRight(left, tagLeft)
					t.tagDirections[tagLeft] = tagMaximize
					status = c.addTagRight(right, tagRight)
					t.tagDirections[tagRight] = tagMaximize
				}
				numTags += 2
			}
			direction = tagMaximize
		}
	}

	if !firstPass {
		purge(tag)
	}
	if !firstPass && minimalTag >= 0 {
		addMinimal(tag)
	}
	_ = numMinimals
	t.endTag = numTags
	t.numTags = numTags
	t.numMinimals = numMinimals
	return status
}

type copySym int

const (
	copyRecurse copySym = iota
	copySetResultPtr
)

const (
	copyRemoveTags       = 1
	copyMaximizeFirstTag = 2
)

// copyAST copies the tree ast to *copy.
func (c *compiler) copyAST(ast *node, flags int, tagDirections []int, copy **node) ErrCode {
	status := RegOK
	bottom := len(c.stack)
	firstTag := true
	result := copy
	var groupMap map[int]int

	c.pushNode(ast)
	if st := c.pushInt(int(copyRecurse)); st != RegOK {
		return st
	}
	for status == RegOK && len(c.stack) > bottom {
		symbol := copySym(c.popInt())
		switch symbol {
		case copySetResultPtr:
			result = c.pop().pp
		case copyRecurse:
			n := c.popNode()
			switch n.typ {
			case nLiteral:
				min, max := n.codeMin, n.codeMax
				if isSpecial(&n.body) && n.codeMin != cBackref {
					if n.codeMin == cTag && flags&copyRemoveTags != 0 {
						// Change this tag to empty.
						min = cEmpty
						max = -1
					} else if n.codeMin == cTag && flags&copyMaximizeFirstTag != 0 && firstTag {
						// Maximize the first tag.
						tagDirections[max] = tagMaximize
						firstTag = false
					}
				}
				r := c.newLiteral(min, max)
				if r == nil {
					return ErrESpace
				}
				*result = r
				if n.posGroup != 0 {
					if groupMap == nil {
						groupMap = map[int]int{}
					}
					g, ok := groupMap[n.posGroup]
					if !ok {
						g = c.newGroup()
						groupMap[n.posGroup] = g
					}
					r.posGroup = g
				}
				if !isSpecial(&n.body) {
					r.class = n.class
					if !c.rcompat {
						// R's copy of tre_copy_ast forgets the negated
						// classes of a copied literal, so in wide mode
						// [^[:space:]]{2} matches a space on its second
						// repetition (R bug, reproduced for R).
						r.negClasses = n.negClasses
					}
				}
			case nUnion:
				u := c.newUnion(n.left, n.right)
				if u == nil {
					return ErrESpace
				}
				*result = u
				result = &u.left
				c.pushNode(n.right)
				c.pushInt(int(copyRecurse))
				c.push(stackItem{pp: &u.right})
				c.pushInt(int(copySetResultPtr))
				c.pushNode(n.left)
				if st := c.pushInt(int(copyRecurse)); st != RegOK {
					return st
				}
			case nCatenation:
				k := c.newCatenation(n.left, n.right)
				if k == nil {
					return ErrESpace
				}
				k.left = nil
				k.right = nil
				*result = k
				result = &k.left
				c.pushNode(n.right)
				c.pushInt(int(copyRecurse))
				c.push(stackItem{pp: &k.right})
				c.pushInt(int(copySetResultPtr))
				c.pushNode(n.left)
				if st := c.pushInt(int(copyRecurse)); st != RegOK {
					return st
				}
			case nIteration:
				c.pushNode(n.arg)
				if st := c.pushInt(int(copyRecurse)); st != RegOK {
					return st
				}
				it := c.newIter(n.arg, n.min, n.max, n.minimal)
				if it == nil {
					return ErrESpace
				}
				*result = it
				result = &it.arg
			}
		}
	}
	return status
}

type expandSym int

const (
	expandRecurse expandSym = iota
	expandAfterIter
)

// expandAST expands each iteration node that has a finite nonzero minimum or
// maximum iteration count to a catenated sequence of copies of the node.
func (c *compiler) expandAST(ast *node, tagDirections []int) ErrCode {
	bottom := len(c.stack)
	c.pushNode(ast)
	if st := c.pushInt(int(expandRecurse)); st != RegOK {
		return st
	}
	for len(c.stack) > bottom {
		symbol := expandSym(c.popInt())
		n := c.popNode()
		switch symbol {
		case expandRecurse:
			switch n.typ {
			case nLiteral:
			case nUnion:
				c.pushNode(n.right)
				c.pushInt(int(expandRecurse))
				c.pushNode(n.left)
				if st := c.pushInt(int(expandRecurse)); st != RegOK {
					return st
				}
			case nCatenation:
				c.pushNode(n.right)
				c.pushInt(int(expandRecurse))
				c.pushNode(n.left)
				if st := c.pushInt(int(expandRecurse)); st != RegOK {
					return st
				}
			case nIteration:
				c.pushNode(n)
				c.pushInt(int(expandAfterIter))
				c.pushNode(n.arg)
				if st := c.pushInt(int(expandRecurse)); st != RegOK {
					return st
				}
			}
		case expandAfterIter:
			if n.min > 1 || n.max > 1 {
				var seq1, seq2 *node
				// Create a catenated sequence of copies of the node.
				for j := 0; j < n.min; j++ {
					var cp *node
					// Remove tags from all but the last copy.
					flags := copyMaximizeFirstTag
					if j+1 < n.min {
						flags = copyRemoveTags
					}
					if st := c.copyAST(n.arg, flags, tagDirections, &cp); st != RegOK {
						return st
					}
					if seq1 != nil {
						seq1 = c.newCatenation(seq1, cp)
					} else {
						seq1 = cp
					}
					if seq1 == nil {
						return ErrESpace
					}
				}
				if n.max == -1 {
					// No upper limit.
					if st := c.copyAST(n.arg, 0, nil, &seq2); st != RegOK {
						return st
					}
					seq2 = c.newIter(seq2, 0, -1, false)
					if seq2 == nil {
						return ErrESpace
					}
				} else {
					for j := n.min; j < n.max; j++ {
						var cp *node
						if st := c.copyAST(n.arg, 0, nil, &cp); st != RegOK {
							return st
						}
						if seq2 != nil {
							seq2 = c.newCatenation(cp, seq2)
						} else {
							seq2 = cp
						}
						if seq2 == nil {
							return ErrESpace
						}
						if c.rcompat {
							// R's copy builds (|seq2) instead of (seq2)?.
							tmp := c.newLiteral(cEmpty, -1)
							if tmp == nil {
								return ErrESpace
							}
							seq2 = c.newUnion(tmp, seq2)
						} else {
							seq2 = c.newIter(seq2, 0, 1, false)
						}
						if seq2 == nil {
							return ErrESpace
						}
					}
				}
				if seq1 == nil {
					seq1 = seq2
				} else if seq2 != nil {
					seq1 = c.newCatenation(seq1, seq2)
				}
				if seq1 == nil {
					return ErrESpace
				}
				n.body = seq1.body
			}
		}
	}
	return RegOK
}

func (c *compiler) setEmpty() []posTags { return nil }

func (c *compiler) setOne(position, codeMin, codeMax int, class ctype, negClasses []ctype, backref int) []posTags {
	if !c.countSet(1) {
		return nil
	}
	return []posTags{{position: position, codeMin: codeMin, codeMax: codeMax, class: class,
		negClasses: negClasses, backref: backref}}
}

// countSet accounts for n new set entries against the limit.
func (c *compiler) countSet(n int) bool {
	c.setItems += n
	return c.setItems <= c.limits.maxSetItems()
}

// setUnion is tre_set_union. ok is false when the resource limit is hit.
func (c *compiler) setUnion(set1, set2 []posTags, tags []int, assertions int) (res []posTags, ok bool) {
	if !c.countSet(len(set1) + len(set2)) {
		return nil, false
	}
	res = make([]posTags, 0, len(set1)+len(set2))
	for _, e := range set1 {
		ne := e
		ne.assertions = e.assertions | assertions
		if e.tags == nil && tags == nil {
			ne.tags = nil
		} else {
			nt := make([]int, 0, len(e.tags)+len(tags))
			nt = append(nt, e.tags...)
			nt = append(nt, tags...)
			ne.tags = nt
		}
		res = append(res, ne)
	}
	for _, e := range set2 {
		ne := e
		// XXX upstream: why not | assertions here as well?
		if e.tags == nil {
			ne.tags = nil
		} else {
			ne.tags = append([]int(nil), e.tags...)
		}
		res = append(res, ne)
	}
	return res, true
}

// matchEmpty finds the empty path through n that is the one that should be
// taken according to POSIX.2 rules, and adds the tags on that path to *tags.
func (c *compiler) matchEmpty(n *node, tags *[]int, assertions *int, numTagsSeen *int) ErrCode {
	bottom := len(c.stack)
	if numTagsSeen != nil {
		*numTagsSeen = 0
	}
	if st := c.pushNode(n); st != RegOK {
		return st
	}
	for len(c.stack) > bottom {
		n = c.popNode()
		switch n.typ {
		case nLiteral:
			switch n.codeMin {
			case cTag:
				if n.codeMax >= 0 {
					if tags != nil {
						// Add the tag to `tags' (no duplicates).
						found := false
						for _, tg := range *tags {
							if tg == n.codeMax {
								found = true
								break
							}
						}
						if !found {
							*tags = append(*tags, n.codeMax)
						}
					}
					if numTagsSeen != nil {
						*numTagsSeen++
					}
				}
			case cAssertion:
				if assertions != nil {
					*assertions |= n.codeMax
				}
			case cEmpty:
			}
		case nUnion:
			// Subexpressions starting earlier take priority over ones
			// starting later, so we prefer the left subexpression over the
			// right subexpression.
			var st ErrCode
			if n.left.nullable != 0 {
				st = c.pushNode(n.left)
			} else if n.right.nullable != 0 {
				st = c.pushNode(n.right)
			}
			if st != RegOK {
				return st
			}
		case nCatenation:
			// The path must go through both children.
			c.pushNode(n.left)
			if st := c.pushNode(n.right); st != RegOK {
				return st
			}
		case nIteration:
			// A match with an empty string is preferred over no match at
			// all, so we go through the argument if possible.
			if n.arg.nullable != 0 {
				if st := c.pushNode(n.arg); st != RegOK {
					return st
				}
			}
		}
	}
	return RegOK
}

type nflSym int

const (
	nflRecurse nflSym = iota
	nflPostUnion
	nflPostCatenation
	nflPostIteration
)

// computeNPFL computes `nullable', `position', `firstpos' and `lastpos' for
// the nodes of the AST; nextpos is updated.
func (c *compiler) computeNPFL(tree *node, nextpos *int) ErrCode {
	groupPos := map[int]int{}
	bottom := len(c.stack)
	c.pushNode(tree)
	if st := c.pushInt(int(nflRecurse)); st != RegOK {
		return st
	}
	for len(c.stack) > bottom {
		symbol := nflSym(c.popInt())
		n := c.popNode()
		switch symbol {
		case nflRecurse:
			switch n.typ {
			case nLiteral:
				switch {
				case n.codeMin == cBackref:
					// Back references: nullable = false, firstpos = {i},
					// lastpos = {i}.
					n.nullable = 0
					n.position = *nextpos
					*nextpos++
					n.firstpos = c.setOne(n.position, 0, treCharMax, ctNone, nil, -1)
					n.lastpos = c.setOne(n.position, 0, treCharMax, ctNone, nil, n.codeMax)
					if n.firstpos == nil || n.lastpos == nil {
						return ErrESpace
					}
				case n.codeMin < 0:
					// Tags, empty strings, params, and zero width
					// assertions: nullable = true, firstpos = {}, and
					// lastpos = {}.
					n.nullable = 1
					n.firstpos = c.setEmpty()
					n.lastpos = c.setEmpty()
				default:
					// Literal at position i: nullable = false,
					// firstpos = {i}, lastpos = {i}.
					n.nullable = 0
					if g := n.posGroup; g != 0 {
						if pos, ok := groupPos[g]; ok {
							n.position = pos
						} else {
							n.position = *nextpos
							*nextpos++
							groupPos[g] = n.position
						}
					} else {
						n.position = *nextpos
						*nextpos++
					}
					n.firstpos = c.setOne(n.position, n.codeMin, n.codeMax, ctNone, nil, -1)
					n.lastpos = c.setOne(n.position, n.codeMin, n.codeMax, n.class, n.negClasses, -1)
					if n.firstpos == nil || n.lastpos == nil {
						return ErrESpace
					}
				}
			case nUnion:
				c.pushNode(n)
				c.pushInt(int(nflPostUnion))
				c.pushNode(n.right)
				c.pushInt(int(nflRecurse))
				c.pushNode(n.left)
				if st := c.pushInt(int(nflRecurse)); st != RegOK {
					return st
				}
			case nCatenation:
				c.pushNode(n)
				c.pushInt(int(nflPostCatenation))
				c.pushNode(n.right)
				c.pushInt(int(nflRecurse))
				c.pushNode(n.left)
				if st := c.pushInt(int(nflRecurse)); st != RegOK {
					return st
				}
			case nIteration:
				c.pushNode(n)
				c.pushInt(int(nflPostIteration))
				c.pushNode(n.arg)
				if st := c.pushInt(int(nflRecurse)); st != RegOK {
					return st
				}
			}
		case nflPostUnion:
			if n.left.nullable != 0 || n.right.nullable != 0 {
				n.nullable = 1
			} else {
				n.nullable = 0
			}
			var ok bool
			if n.firstpos, ok = c.setUnion(n.left.firstpos, n.right.firstpos, nil, 0); !ok {
				return ErrESpace
			}
			if n.lastpos, ok = c.setUnion(n.left.lastpos, n.right.lastpos, nil, 0); !ok {
				return ErrESpace
			}
		case nflPostIteration:
			if n.min == 0 || n.arg.nullable != 0 {
				n.nullable = 1
			} else {
				n.nullable = 0
			}
			n.firstpos = n.arg.firstpos
			n.lastpos = n.arg.lastpos
		case nflPostCatenation:
			if n.left.nullable != 0 && n.right.nullable != 0 {
				n.nullable = 1
			} else {
				n.nullable = 0
			}
			// Compute firstpos.
			if n.left.nullable != 0 {
				// The left side matches the empty string: the tags and
				// assertions of its empty path are added to the right
				// side's firstpos.
				tags := []int{}
				assertions := 0
				if st := c.matchEmpty(n.left, &tags, &assertions, nil); st != RegOK {
					return st
				}
				var ok bool
				if n.firstpos, ok = c.setUnion(n.right.firstpos, n.left.firstpos, tags, assertions); !ok {
					return ErrESpace
				}
			} else {
				n.firstpos = n.left.firstpos
			}
			// Compute lastpos.
			if n.right.nullable != 0 {
				tags := []int{}
				assertions := 0
				if st := c.matchEmpty(n.right, &tags, &assertions, nil); st != RegOK {
					return st
				}
				var ok bool
				if n.lastpos, ok = c.setUnion(n.left.lastpos, n.right.lastpos, tags, assertions); !ok {
					return ErrESpace
				}
			} else {
				n.lastpos = n.right.lastpos
			}
		}
	}
	return RegOK
}

// makeTrans adds a transition from each position in p1 to each position in
// p2. With trans == nil it only counts (counts[p1.position]++).
func (c *compiler) makeTrans(p1, p2 []posTags, trans []tnfaTrans, counts, offs []int, total *int) ErrCode {
	if trans == nil {
		for _, a := range p1 {
			counts[a.position] += len(p2)
			*total += len(p2)
			if *total > c.limits.maxTransitions() {
				return ErrESpace
			}
		}
		return RegOK
	}
	for i := range p1 {
		a := &p1[i]
		prevP2Pos := -1
		for j := range p2 {
			b := &p2[j]
			// Optimization: if this position was already handled, skip it.
			if b.position == prevP2Pos {
				continue
			}
			prevP2Pos = b.position
			// Find the next unused transition from position a.position.
			idx := offs[a.position]
			for trans[idx].next != 0 {
				idx++
			}
			t := &trans[idx]
			// Use the character ranges, assertions, etc. from `a' for the
			// transition from `a' to `b'.
			t.codeMin = rune(a.codeMin)
			t.codeMax = rune(a.codeMax)
			t.next = offs[b.position] + 1
			t.stateID = b.position
			t.assertions = a.assertions | b.assertions
			if a.class != ctNone {
				t.assertions |= assertCharClass
			}
			if a.negClasses != nil {
				t.assertions |= assertCharClassNeg
			}
			if a.backref >= 0 {
				t.backref = a.backref
				t.assertions |= assertBackref
			} else {
				t.class = a.class
			}
			if a.negClasses != nil {
				t.negClasses = a.negClasses
			}
			// Tags: those of a, then those of b that are not in a's.
			t.tags = nil
			if len(a.tags)+len(b.tags) > 0 {
				tg := make([]int, 0, len(a.tags)+len(b.tags))
				tg = append(tg, a.tags...)
				for _, x := range b.tags {
					dup := false
					for _, y := range a.tags {
						if y == x {
							dup = true
							break
						}
					}
					if !dup {
						tg = append(tg, x)
					}
				}
				t.tags = tg
			}
		}
	}
	return RegOK
}

// astToTNFA converts the syntax tree to a TNFA (the iterative version).
func (c *compiler) astToTNFA(root *node, trans []tnfaTrans, counts, offs []int, total *int) ErrCode {
	bottom := len(c.stack)
	if st := c.pushNode(root); st != RegOK {
		return st
	}
	for len(c.stack) > bottom {
		n := c.popNode()
		switch n.typ {
		case nLiteral:
		case nUnion:
			c.pushNode(n.right)
			if st := c.pushNode(n.left); st != RegOK {
				return st
			}
		case nCatenation:
			// Add a transition from each position in left.lastpos to each
			// position in right.firstpos.
			if st := c.makeTrans(n.left.lastpos, n.right.firstpos, trans, counts, offs, total); st != RegOK {
				return st
			}
			c.pushNode(n.right)
			if st := c.pushNode(n.left); st != RegOK {
				return st
			}
		case nIteration:
			if !(n.max == -1 || n.max == 1) {
				return ErrBadBR
			}
			if n.max == -1 {
				if !(n.min == 0 || n.min == 1) {
					return ErrBadBR
				}
				// Add a transition from each last position in the iterated
				// expression to each first position.
				if st := c.makeTrans(n.arg.lastpos, n.arg.firstpos, trans, counts, offs, total); st != RegOK {
					return st
				}
			}
			if st := c.pushNode(n.arg); st != RegOK {
				return st
			}
		}
	}
	return RegOK
}

// compile is tre_compile: it parses re and builds the TNFA.
func (c *compiler) compile(re []rune, cflags Flags) (*tnfa, int, ErrCode) {
	curMax := 2
	if cflags&UseBytes != 0 {
		curMax = 1
	}
	p := &parseCtx{c: c, re: re, cflags: cflags, maxBackref: -1, curMax: curMax}
	if st := p.parse(); st != RegOK {
		return nil, 0, st
	}
	nsub := p.submatchID - 1
	tree := p.result
	if tree == nil {
		return nil, 0, ErrBadPat
	}

	// Referring to nonexistent subexpressions is illegal.
	if p.maxBackref > nsub {
		return nil, 0, ErrESubReg
	}

	t := &tnfa{rcompat: c.rcompat}
	t.haveBackrefs = p.maxBackref >= 0
	t.numSubmatches = p.submatchID

	// Set up tags for submatch addressing. If REG_NOSUB is set and the
	// regexp does not have back references, this can be skipped.
	if t.haveBackrefs || cflags&NoSub == 0 {
		t.submatchData = make([]submatchData, p.submatchID)
		// Figure out how many tags we will need.
		if st := c.addTags(tree, t, true); st != RegOK {
			return nil, 0, st
		}
		if t.numTags > 0 {
			t.tagDirections = make([]int, t.numTags+1)
			for i := range t.tagDirections {
				t.tagDirections[i] = -1
			}
		}
		t.minimalTags = make([]int, 0, 2*t.numTags)
		if st := c.addTags(tree, t, false); st != RegOK {
			return nil, 0, st
		}
	}

	// Expand iteration nodes.
	if st := c.expandAST(tree, t.tagDirections); st != RegOK {
		return nil, 0, st
	}

	// Add a dummy node for the final state.
	tmpL := tree
	tmpR := c.newLiteral(0, 0)
	if tmpR == nil {
		return nil, 0, ErrESpace
	}
	tree = c.newCatenation(tmpL, tmpR)
	if tree == nil {
		return nil, 0, ErrESpace
	}

	numpos := 0
	if st := c.computeNPFL(tree, &numpos); st != RegOK {
		return nil, 0, st
	}
	if numpos > c.limits.maxStates() {
		return nil, 0, ErrESpace
	}

	counts := make([]int, numpos)
	offs := make([]int, numpos)
	total := 0
	if st := c.astToTNFA(tree, nil, counts, nil, &total); st != RegOK {
		return nil, 0, st
	}
	add := 0
	for i := 0; i < numpos; i++ {
		offs[i] = add
		add += counts[i] + 1
		counts[i] = 0
	}
	transitions := make([]tnfaTrans, add+1)
	t.transitions = transitions
	if st := c.astToTNFA(tree, transitions, counts, offs, &total); st != RegOK {
		return nil, 0, st
	}

	// If in eight bit mode, compute a table of characters that can be the
	// first character of a match.
	t.firstChar = -1
	if curMax == 1 && !tmpL.nullable0() {
		var seen [256]bool
		count := 0
		for _, ps := range tree.firstpos {
			for j := offs[ps.position]; transitions[j].next != 0; j++ {
				for k := transitions[j].codeMin; k <= transitions[j].codeMax && k < 256; k++ {
					seen[k] = true
					count++
				}
			}
		}
		if count == 1 {
			for k := 0; k < 256; k++ {
				if seen[k] {
					t.firstChar = k
					break
				}
			}
		}
	}

	t.initial = make([]tnfaTrans, 0, len(tree.firstpos))
	for _, ps := range tree.firstpos {
		it := tnfaTrans{next: offs[ps.position] + 1, stateID: ps.position, assertions: ps.assertions}
		if ps.tags != nil {
			it.tags = append([]int(nil), ps.tags...)
		}
		t.initial = append(t.initial, it)
	}
	t.final = offs[tree.lastpos[0].position]
	t.numStates = numpos
	t.cflags = cflags
	return t, nsub, RegOK
}

func (n *node) nullable0() bool { return n.nullable != 0 }
