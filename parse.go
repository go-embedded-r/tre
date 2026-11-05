package tre

import "sort"

// Port of tre-parse.c: the parser for POSIX.2 regexps, both the obsolete
// default syntax and the "extended" syntax, with TRE's extensions. It is a
// recursive descent parser run on an explicit stack, as upstream.

const (
	charPipe         = '|'
	charLParen       = '('
	charRParen       = ')'
	charLBrace       = '{'
	charRBrace       = '}'
	charLBracket     = '['
	charRBracket     = ']'
	charMinus        = '-'
	charStar         = '*'
	charQuestionMark = '?'
	charPlus         = '+'
	charPeriod       = '.'
	charColon        = ':'
	charEqual        = '='
	charComma        = ','
	charCaret        = '^'
	charDollar       = '$'
	charBackslash    = '\\'
	charHash         = '#'
	charTilde        = '~'
)

var macros = []struct {
	c   rune
	exp string
}{
	{'t', "\t"}, {'n', "\n"}, {'r', "\r"},
	{'f', "\f"}, {'a', "\a"}, {'e', "\033"},
	{'w', "[[:alnum:]_]"}, {'W', "[^[:alnum:]_]"}, {'s', "[[:space:]]"},
	{'S', "[^[:space:]]"}, {'d', "[[:digit:]]"}, {'D', "[^[:digit:]]"},
}

// expandMacro returns the expansion of the macro named by re[ri], or nil.
func expandMacro(re []rune, ri int) []rune {
	if ri >= len(re) {
		return nil
	}
	for _, m := range macros {
		if m.c == re[ri] {
			return []rune(m.exp)
		}
	}
	return nil
}

type parseCtx struct {
	c           *compiler
	re          []rune
	ri          int // index of the next character (ctx->re)
	submatchID  int
	maxBackref  int
	cflags      Flags
	nofirstsub  bool
	curMax      int // 1 in byte mode: character classes are expanded
	result      *node
	approxFound bool
}

// at returns re[i], or 0 past the end: the C code reads the terminating NUL.
func (p *parseCtx) at(i int) rune {
	if i >= 0 && i < len(p.re) {
		return p.re[i]
	}
	return 0
}

type bracketItems struct {
	items []*node
	maxI  int
}

func (p *parseCtx) newItem(b *bracketItems, min, max int) ErrCode {
	if len(b.items) >= b.maxI {
		// If the array is already 1024 items large, give up.
		if b.maxI > 1024 {
			return ErrESpace
		}
		b.maxI *= 2
	}
	n := p.c.newLiteral(min, max)
	if n == nil {
		return ErrESpace
	}
	b.items = append(b.items, n)
	return RegOK
}

// expandCtype expands a character class to character ranges (8-bit mode).
func (p *parseCtx) expandCtype(cl ctype, b *bracketItems) ErrCode {
	min, max := -1, 0
	for j := 0; j < 256; j++ {
		c := rune(j)
		if isctype(c, cl) || (p.cflags&ICase != 0 && (isctype(toLower(c), cl) || isctype(toUpper(c), cl))) {
			if min < 0 {
				min = j
			}
			max = j
		} else if min >= 0 {
			if st := p.newItem(b, min, max); st != RegOK {
				return st
			}
			min = -1
		}
	}
	if min >= 0 {
		return p.newItem(b, min, max)
	}
	return RegOK
}

func (p *parseCtx) parseBracketItems(negate bool, negClasses *[]ctype, b *bracketItems) ErrCode {
	re := p.ri
	start := p.ri
	for {
		skip := false
		if re >= len(p.re) {
			return ErrEBrack
		}
		if p.at(re) == charRBracket && re > start {
			re++
			break
		}
		var class ctype
		var min, max int
		switch {
		case re+2 < len(p.re) && p.at(re+1) == charMinus && p.at(re+2) != charRBracket:
			min = int(p.at(re))
			max = int(p.at(re + 2))
			re += 3
			if min > max {
				return ErrERange
			}
		case re+1 < len(p.re) && p.at(re) == charLBracket && p.at(re+1) == charPeriod:
			return ErrECollate
		case re+1 < len(p.re) && p.at(re) == charLBracket && p.at(re+1) == charEqual:
			return ErrECollate
		case re+1 < len(p.re) && p.at(re) == charLBracket && p.at(re+1) == charColon:
			endptr := re + 2
			for endptr < len(p.re) && p.at(endptr) != charColon {
				endptr++
			}
			if endptr == len(p.re) {
				return ErrECType
			}
			l := endptr - re - 2
			if l > 63 {
				l = 63
			}
			name := p.re[re+2 : re+2+l]
			// wcsrtombs fails on a character that is not representable:
			// every class name is ASCII, so such a name is unknown anyway.
			class = ctypeNames[string(name)]
			if class == ctNone {
				return ErrECType
			}
			// Optimize character classes for 8 bit character sets.
			if p.curMax == 1 {
				if st := p.expandCtype(class, b); st != RegOK {
					return st
				}
				class = ctNone
				skip = true
			}
			re = endptr + 2
			min = 0
			max = treCharMax
		default:
			if p.at(re) == charMinus && p.at(re+1) != charRBracket && start != re {
				// Two ranges are not allowed to share an endpoint.
				return ErrERange
			}
			min = int(p.at(re))
			max = min
			re++
		}

		if class != ctNone && negate {
			if len(*negClasses) >= maxNegClasses {
				return ErrESpace
			}
			*negClasses = append(*negClasses, class)
		} else if !skip {
			if st := p.newItem(b, min, max); st != RegOK {
				return st
			}
			b.items[len(b.items)-1].class = class
		}

		// Add opposite-case counterpoints if REG_ICASE is present. This is
		// broken if there are more than two "same" characters.
		if p.cflags&ICase != 0 && class == ctNone && !skip {
			for min <= max {
				if isLower(rune(min)) {
					cmin := int(toUpper(rune(min)))
					ccurr := cmin
					min++
					for isLower(rune(min)) && int(toUpper(rune(min))) == ccurr+1 && min <= max {
						ccurr = int(toUpper(rune(min)))
						min++
					}
					if st := p.newItem(b, cmin, ccurr); st != RegOK {
						return st
					}
				} else if isUpper(rune(min)) {
					cmin := int(toLower(rune(min)))
					ccurr := cmin
					min++
					for isUpper(rune(min)) && int(toLower(rune(min))) == ccurr+1 && min <= max {
						ccurr = int(toLower(rune(min)))
						min++
					}
					if st := p.newItem(b, cmin, ccurr); st != RegOK {
						return st
					}
				} else {
					min++
				}
			}
		}
	}
	p.ri = re
	return RegOK
}

func (p *parseCtx) parseBracket() (*node, ErrCode) {
	var result *node
	negate := false
	b := &bracketItems{maxI: 32}
	var negClasses []ctype

	if p.at(p.ri) == charCaret {
		negate = true
		p.ri++
	}
	if st := p.parseBracketItems(negate, &negClasses, b); st != RegOK {
		return nil, st
	}
	items := b.items
	gid := 0
	if p.c.rcompat {
		gid = p.c.newGroup()
		for _, it := range items {
			it.posGroup = gid
		}
	}

	// Sort the array if we need to negate it.
	if negate {
		sort.SliceStable(items, func(i, j int) bool { return items[i].codeMin < items[j].codeMin })
	}

	copyNeg := func() []ctype {
		if len(negClasses) == 0 {
			return nil
		}
		return append([]ctype(nil), negClasses...)
	}

	currMax, currMin := 0, 0
	// Build a union of the items in the array, negated if necessary.
	for _, it := range items {
		l := it
		min, max := l.codeMin, l.codeMax
		if negate {
			if min < currMax {
				// Overlap.
				if max+1 > currMax {
					currMax = max + 1
				}
				l = nil
			} else {
				// No overlap.
				currMax = min - 1
				if currMax >= currMin {
					l.codeMin = currMin
					l.codeMax = currMax
				} else {
					l = nil
				}
				currMin = max + 1
				currMax = currMin
			}
		}
		if l != nil {
			l.negClasses = copyNeg()
			if result == nil {
				result = it
			} else {
				u := p.c.newUnion(result, it)
				if u == nil {
					return nil, ErrESpace
				}
				result = u
			}
		}
	}
	if negate {
		n := p.c.newLiteral(currMin, treCharMax)
		if n == nil {
			return nil, ErrESpace
		}
		n.negClasses = copyNeg()
		n.posGroup = gid
		if result == nil {
			result = n
		} else {
			u := p.c.newUnion(result, n)
			if u == nil {
				return nil, ErrESpace
			}
			result = u
		}
	}
	return result, RegOK
}

const intMax = 0x7fffffff

// parseInt parses a decimal integer. Upstream saturates at INT_MAX and
// returns -1 for no digits; R's modified copy returns -1 on overflow too.
func (p *parseCtx) parseInt(r *int) int {
	if p.c.rcompat {
		num := -1
		overflow := false
		for *r < len(p.re) && p.re[*r] >= '0' && p.re[*r] <= '9' {
			if num < 0 {
				num = 0
			}
			if num <= (intMax-9)/10 {
				num = num*10 + int(p.re[*r]-'0')
			} else {
				overflow = true
			}
			*r++
		}
		if overflow {
			return -1
		}
		return num
	}
	num := uint64(0)
	overflow := false
	i := *r
	for i < len(p.re) && p.re[i] >= '0' && p.re[i] <= '9' {
		if !overflow {
			num = num*10 + uint64(p.re[i]-'0')
			if num > intMax {
				overflow = true
			}
		}
		i++
	}
	if i == *r {
		return -1
	}
	*r = i
	if overflow {
		return intMax
	}
	return int(num)
}

func (p *parseCtx) parseBound(result **node) ErrCode {
	r := p.ri
	minimal := p.cflags&Ungreedy != 0
	approx, costsSet, countsSet := false, false, false

	// Parse number (minimum repetition count).
	min := -1
	if r < len(p.re) && p.re[r] >= '0' && p.re[r] <= '9' {
		min = p.parseInt(&r)
	}

	// Parse comma and second number (maximum repetition count).
	max := min
	if r < len(p.re) && p.re[r] == charComma {
		if min < 0 && !p.c.rcompat {
			min = 0
		}
		r++
		max = p.parseInt(&r)
	}

	// Check that the repeat counts are sane.
	if p.c.rcompat {
		// R's copy folds BADMAX into BADBR, tests only the maximum, and
		// leaves a missing minimum at -1.
		if (max >= 0 && min > max) || max > reDupMax {
			return ErrBadBR
		}
	} else {
		if max >= 0 && min > max {
			return ErrBadBR
		}
		if min > reDupMax || max > reDupMax {
			return ErrBadMax
		}
	}

	// The approximate-matching syntax of TRE is parsed (so that the bound is
	// delimited as upstream does) but not supported.
	for {
		start := r
		if !countsSet {
			done := false
			for r+1 < len(p.re) && !done {
				switch p.re[r] {
				case charPlus, charMinus, charHash:
					r++
					p.parseInt(&r)
					countsSet = true
				case charTilde:
					r++
					p.parseInt(&r)
					approx = true
				case charComma, ' ':
					r++
				default:
					done = true
				}
			}
		}
		if !costsSet {
			done := false
			for r+1 < len(p.re) && !done {
				switch c := p.re[r]; {
				case c == charPlus || c == ' ':
					r++
				case c == '<':
					r++
					for p.at(r) == ' ' {
						r++
					}
					p.parseInt(&r)
					approx = true
				case c == charComma:
					r++
					done = true
				case c >= '0' && c <= '9':
					p.parseInt(&r)
					switch p.at(r) {
					case 'i', 'd', 's':
						r++
						costsSet = true
					default:
						return ErrBadBR
					}
				default:
					done = true
				}
			}
		}
		if start == r {
			break
		}
	}

	// Missing }.
	if r >= len(p.re) {
		return ErrEBrace
	}
	// Empty contents of {}.
	if r == p.ri {
		return ErrBadBR
	}
	// Parse the ending '}' or '\}'.
	if p.cflags&Extended != 0 {
		if r >= len(p.re) || p.re[r] != charRBrace {
			return ErrBadBR
		}
		r++
	} else {
		if r+1 >= len(p.re) || p.re[r] != charBackslash || p.re[r+1] != charRBrace {
			return ErrBadBR
		}
		r += 2
	}

	// Parse trailing '?' marking minimal repetition.
	if r < len(p.re) {
		if p.re[r] == charQuestionMark {
			minimal = p.cflags&Ungreedy == 0
			r++
		} else if p.re[r] == charStar || p.re[r] == charPlus {
			// These are reserved for future extensions.
			return ErrBadRpt
		}
	}

	if approx || costsSet || countsSet {
		return ErrApprox // approximate matching is not ported
	}

	// Create the AST node(s).
	if min == 0 && max == 0 {
		n := p.c.newLiteral(cEmpty, -1)
		if n == nil {
			return ErrESpace
		}
		*result = n
	} else {
		if min < 0 && max < 0 {
			// Only approximate parameters set, no repetitions.
			min, max = 1, 1
		}
		n := p.c.newIter(*result, min, max, minimal)
		if n == nil {
			return ErrESpace
		}
		*result = n
	}
	p.ri = r
	return RegOK
}

type parseSym int

const (
	parseRE parseSym = iota
	parseAtom
	parseMarkForSubmatch
	parseBranch
	parsePiece
	parseCatenation
	parsePostCatenation
	parseUnion
	parsePostUnion
	parsePostfix
	parseRestoreCflags
)

// parse is tre_parse: it parses p.re into p.result.
func (p *parseCtx) parse() ErrCode {
	c := p.c
	var result *node
	bottom := len(c.stack)
	depth := 0
	temporaryCflags := Flags(0)

	push := func(s parseSym) ErrCode { return c.pushInt(int(s)) }
	if !p.nofirstsub {
		if st := c.pushInt(p.submatchID); st != RegOK {
			return st
		}
		if st := push(parseMarkForSubmatch); st != RegOK {
			return st
		}
		p.submatchID++
	}
	if st := push(parseRE); st != RegOK {
		return st
	}

	for len(c.stack) > bottom {
		symbol := parseSym(c.popInt())
		var st ErrCode
		switch symbol {
		case parseRE:
			// Parse a full regexp. A regexp is one or more branches,
			// separated by the union operator `|'.
			if p.cflags&Literal == 0 && p.cflags&Extended != 0 {
				if st = push(parseUnion); st != RegOK {
					return st
				}
			}
			if st = push(parseBranch); st != RegOK {
				return st
			}

		case parseBranch:
			if st = push(parseCatenation); st != RegOK {
				return st
			}
			if st = push(parsePiece); st != RegOK {
				return st
			}

		case parsePiece:
			if p.cflags&Literal == 0 {
				if st = push(parsePostfix); st != RegOK {
					return st
				}
			}
			if st = push(parseAtom); st != RegOK {
				return st
			}

		case parseCatenation:
			// If the expression has not ended, parse another piece.
			if p.ri >= len(p.re) {
				break
			}
			ch := p.at(p.ri)
			if p.cflags&Literal == 0 {
				if p.cflags&Extended != 0 && ch == charPipe {
					break
				}
				if (p.cflags&Extended != 0 && ch == charRParen && depth > 0) ||
					(p.cflags&Extended == 0 && ch == charBackslash && p.at(p.ri+1) == charRParen) {
					if p.cflags&Extended == 0 && depth == 0 {
						return ErrEParen
					}
					depth--
					if p.cflags&Extended == 0 {
						p.ri += 2
					}
					break
				}
			}
			if p.cflags&RightAssoc != 0 {
				// Right associative concatenation.
				if st = c.pushNode(result); st != RegOK {
					return st
				}
				if st = push(parsePostCatenation); st != RegOK {
					return st
				}
				if st = push(parseCatenation); st != RegOK {
					return st
				}
				if st = push(parsePiece); st != RegOK {
					return st
				}
			} else {
				// Default case, left associative concatenation.
				if st = push(parseCatenation); st != RegOK {
					return st
				}
				if st = c.pushNode(result); st != RegOK {
					return st
				}
				if st = push(parsePostCatenation); st != RegOK {
					return st
				}
				if st = push(parsePiece); st != RegOK {
					return st
				}
			}

		case parsePostCatenation:
			tree := c.popNode()
			tmp := c.newCatenation(tree, result)
			if tmp == nil {
				return ErrESpace
			}
			result = tmp

		case parseUnion:
			if p.ri >= len(p.re) {
				break
			}
			if p.cflags&Literal != 0 {
				break
			}
			switch p.at(p.ri) {
			case charPipe:
				if st = push(parseUnion); st != RegOK {
					return st
				}
				if st = c.pushNode(result); st != RegOK {
					return st
				}
				if st = push(parsePostUnion); st != RegOK {
					return st
				}
				if st = push(parseBranch); st != RegOK {
					return st
				}
				p.ri++
			case charRParen:
				p.ri++
			}

		case parsePostUnion:
			tree := c.popNode()
			tmp := c.newUnion(tree, result)
			if tmp == nil {
				return ErrESpace
			}
			result = tmp

		case parsePostfix:
			// Parse postfix operators.
			if p.ri >= len(p.re) {
				break
			}
			if p.cflags&Literal != 0 {
				break
			}
			ch := p.at(p.ri)
			parseBraceNow := false
			switch ch {
			case charPlus, charQuestionMark, charStar:
				if ch != charStar && p.cflags&Extended == 0 {
					break
				}
				minimal := p.cflags&Ungreedy != 0
				repMin, repMax := 0, -1
				if ch == charPlus {
					repMin = 1
				}
				if ch == charQuestionMark {
					repMax = 1
				}
				if p.ri+1 < len(p.re) {
					if p.at(p.ri+1) == charQuestionMark {
						minimal = p.cflags&Ungreedy == 0
						p.ri++
					} else if p.at(p.ri+1) == charStar || p.at(p.ri+1) == charPlus {
						// These are reserved for future extensions.
						return ErrBadRpt
					}
				}
				p.ri++
				tmp := c.newIter(result, repMin, repMax, minimal)
				if tmp == nil {
					return ErrESpace
				}
				result = tmp
				if st = push(parsePostfix); st != RegOK {
					return st
				}
			case charBackslash:
				// "\{" is special without REG_EXTENDED
				if p.cflags&Extended == 0 && p.ri+1 < len(p.re) && p.at(p.ri+1) == charLBrace {
					p.ri++
					parseBraceNow = true
				}
			case charLBrace:
				// "{" is literal without REG_EXTENDED
				if p.cflags&Extended != 0 {
					parseBraceNow = true
				}
			}
			if parseBraceNow {
				p.ri++
				if st = p.parseBound(&result); st != RegOK {
					return st
				}
				if st = push(parsePostfix); st != RegOK {
					return st
				}
			}

		case parseAtom:
			// Parse an atom. An atom is a regular expression enclosed in
			// `()', an empty set of `()', a bracket expression, `.', `^',
			// `$', a `\' followed by a character, or a single character.
			ar := p.atom(&result, &depth, &temporaryCflags)
			if ar != RegOK {
				return ar
			}

		case parseMarkForSubmatch:
			submatchID := c.popInt()
			if result != nil {
				if result.submatchID >= 0 {
					n := c.newLiteral(cEmpty, -1)
					if n == nil {
						return ErrESpace
					}
					tmp := c.newCatenation(n, result)
					if tmp == nil {
						return ErrESpace
					}
					tmp.numSubmatches = result.numSubmatches
					result = tmp
				}
				result.submatchID = submatchID
				result.numSubmatches++
			}

		case parseRestoreCflags:
			p.cflags = Flags(c.popInt())
		}
	}

	// Check for missing closing parentheses.
	if depth > 0 {
		return ErrEParen
	}
	p.result = result
	return RegOK
}

// atom parses one PARSE_ATOM step. It may push stack items; the new value of
// *result is the atom (nil when only the stack was extended).
func (p *parseCtx) atom(result **node, depth *int, temporaryCflags *Flags) ErrCode {
	c := p.c
	// End of regexp? (empty string).
	if p.ri >= len(p.re) {
		return p.literal(result, temporaryCflags)
	}
	if p.cflags&Literal != 0 {
		return p.literal(result, temporaryCflags)
	}

	switch ch := p.at(p.ri); ch {
	case charLParen: // parenthesized subexpression
		// Handle "(?...)" extensions. They work in a way similar to Perls
		// corresponding extensions.
		if p.cflags&Extended != 0 && p.at(p.ri+1) == charQuestionMark {
			newCflags := p.cflags
			bit := true
			p.ri += 2
		ext:
			for {
				switch p.at(p.ri) {
				case 'i':
					if bit {
						newCflags |= ICase
					} else {
						newCflags &^= ICase
					}
					p.ri++
				case 'n':
					if bit {
						newCflags |= Newline
					} else {
						newCflags &^= Newline
					}
					p.ri++
				case 'r':
					if bit {
						newCflags |= RightAssoc
					} else {
						newCflags &^= RightAssoc
					}
					p.ri++
				case 'U':
					if bit {
						newCflags |= Ungreedy
					} else {
						newCflags &^= Ungreedy
					}
					p.ri++
				case charMinus:
					p.ri++
					bit = false
				case charColon:
					p.ri++
					*depth++
					break ext
				case charHash:
					// A comment can contain any character except a right
					// parenthesis.
					for p.at(p.ri) != charRParen && p.ri < len(p.re) {
						p.ri++
					}
					if p.at(p.ri) == charRParen && p.ri < len(p.re) {
						p.ri++
						break ext
					}
					return ErrBadPat
				case charRParen:
					p.ri++
					break ext
				default:
					return ErrBadPat
				}
			}
			// Turn on the cflags changes for the rest of the enclosing group.
			if st := c.pushInt(int(p.cflags)); st != RegOK {
				return st
			}
			if st := c.pushInt(int(parseRestoreCflags)); st != RegOK {
				return st
			}
			if st := c.pushInt(int(parseRE)); st != RegOK {
				return st
			}
			p.cflags = newCflags
			return RegOK
		}
		if p.cflags&Extended == 0 {
			return p.literal(result, temporaryCflags)
		}
		return p.atomLParen(depth)

	case charRParen: // end of current subexpression
		if p.cflags&Extended == 0 || *depth == 0 {
			return p.literal(result, temporaryCflags)
		}
		return p.atomRParen(result)

	case charLBracket: // bracket expression
		p.ri++
		n, st := p.parseBracket()
		if st != RegOK {
			return st
		}
		*result = n

	case charBackslash:
		// If this is "\(" or "\)" skip straight there.
		if p.cflags&Extended == 0 && p.ri+1 < len(p.re) {
			p.ri++
			if p.at(p.ri) == charLParen {
				return p.atomLParen(depth)
			}
			if p.at(p.ri) == charRParen {
				return p.atomRParen(result)
			}
			p.ri--
		}

		// If a macro is used, parse the expanded macro recursively.
		if buf := expandMacro(p.re, p.ri+1); buf != nil {
			sub := *p
			sub.re = buf
			sub.ri = 0
			sub.nofirstsub = true
			sub.result = nil
			if st := sub.parse(); st != RegOK {
				return st
			}
			p.ri += 2
			*result = sub.result
			return RegOK
		}

		if p.ri+1 >= len(p.re) {
			// Trailing backslash.
			return ErrEEscape
		}

		if p.at(p.ri+1) == 'Q' {
			p.cflags |= Literal
			*temporaryCflags |= Literal
			p.ri += 2
			return c.pushInt(int(parseAtom))
		}

		p.ri++
		switch p.at(p.ri) {
		case 'b':
			*result = c.newLiteral(cAssertion, assertAtWB)
			p.ri++
		case 'B':
			*result = c.newLiteral(cAssertion, assertAtWBNeg)
			p.ri++
		case '<':
			*result = c.newLiteral(cAssertion, assertAtBOW)
			p.ri++
		case '>':
			*result = c.newLiteral(cAssertion, assertAtEOW)
			p.ri++
		case 'x':
			p.ri++
			if p.at(p.ri) != charLBrace && p.ri < len(p.re) {
				// 8 bit hex char.
				val := 0
				if isXdigit(p.at(p.ri)) && p.ri < len(p.re) {
					val = hexVal(p.at(p.ri))
					p.ri++
					if isXdigit(p.at(p.ri)) && p.ri < len(p.re) {
						val = val*16 + hexVal(p.at(p.ri))
						p.ri++
					}
				}
				*result = c.newLiteral(val, val)
				break
			} else if p.ri < len(p.re) {
				// Wide char.
				var val int64
				i := 0
				p.ri++
				for len(p.re)-p.ri >= 0 {
					if p.at(p.ri) == charRBrace {
						break
					}
					if isXdigit(p.at(p.ri)) && i < 8 {
						val = val*16 + int64(hexVal(p.at(p.ri)))
						i++
						p.ri++
						continue
					}
					return ErrEBrace
				}
				p.ri++
				v := int(int32(val))
				*result = c.newLiteral(v, v)
				break
			}
			// FALLTHROUGH
			p.escapedOrBackref(result)
		default:
			p.escapedOrBackref(result)
		}
		if *result == nil {
			return ErrESpace
		}

	case charPeriod: // the any-symbol
		if p.cflags&Newline != 0 {
			tmp1 := c.newLiteral(0, '\n'-1)
			if tmp1 == nil {
				return ErrESpace
			}
			tmp2 := c.newLiteral('\n'+1, treCharMax)
			if tmp2 == nil {
				return ErrESpace
			}
			u := c.newUnion(tmp1, tmp2)
			if u == nil {
				return ErrESpace
			}
			*result = u
		} else {
			n := c.newLiteral(0, treCharMax)
			if n == nil {
				return ErrESpace
			}
			*result = n
		}
		p.ri++

	case charCaret: // beginning of line assertion
		// '^' has a special meaning everywhere in EREs, and in the beginning
		// of the RE and after \( is BREs.
		if p.cflags&Extended != 0 ||
			(p.ri-2 >= 0 && p.at(p.ri-2) == charBackslash && p.at(p.ri-1) == charLParen) ||
			p.ri == 0 {
			n := c.newLiteral(cAssertion, assertAtBOL)
			if n == nil {
				return ErrESpace
			}
			*result = n
			p.ri++
		} else {
			return p.literal(result, temporaryCflags)
		}

	case charDollar: // end of line assertion.
		// '$' is special everywhere in EREs, and in the end of the string
		// and before \) is BREs.
		if p.cflags&Extended != 0 ||
			(p.ri+2 < len(p.re) && p.at(p.ri+1) == charBackslash && p.at(p.ri+2) == charRParen) ||
			p.ri+1 == len(p.re) {
			n := c.newLiteral(cAssertion, assertAtEOL)
			if n == nil {
				return ErrESpace
			}
			*result = n
			p.ri++
		} else {
			return p.literal(result, temporaryCflags)
		}

	default:
		return p.literal(result, temporaryCflags)
	}
	return RegOK
}

func hexVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return int(c-'A') + 10
}

// escapedOrBackref handles the default branch after a backslash: a back
// reference \1-\9 or an escaped character.
func (p *parseCtx) escapedOrBackref(result **node) {
	c := p.c
	if isDigit(p.at(p.ri)) {
		val := int(p.at(p.ri) - '0')
		n := c.newLiteral(cBackref, val)
		if n == nil {
			*result = nil
			return
		}
		*result = n
		if val > p.maxBackref {
			p.maxBackref = val
		}
		p.ri++
	} else {
		// Escaped character.
		*result = c.newLiteral(int(p.at(p.ri)), int(p.at(p.ri)))
		p.ri++
	}
}

func (p *parseCtx) atomLParen(depth *int) ErrCode {
	c := p.c
	*depth++
	if p.ri+2 < len(p.re) && p.at(p.ri+1) == charQuestionMark && p.at(p.ri+2) == charColon {
		// Don't mark for submatching.
		p.ri += 3
		return c.pushInt(int(parseRE))
	}
	p.ri++
	// First parse a whole RE, then mark the resulting tree for submatching.
	if st := c.pushInt(p.submatchID); st != RegOK {
		return st
	}
	if st := c.pushInt(int(parseMarkForSubmatch)); st != RegOK {
		return st
	}
	if st := c.pushInt(int(parseRE)); st != RegOK {
		return st
	}
	p.submatchID++
	return RegOK
}

func (p *parseCtx) atomRParen(result **node) ErrCode {
	// We were expecting an atom, but instead the current subexpression was
	// closed. POSIX leaves the meaning of this to be implementation-defined.
	// We interpret this as an empty expression (which matches an empty
	// string).
	n := p.c.newLiteral(cEmpty, -1)
	if n == nil {
		return ErrESpace
	}
	*result = n
	if p.cflags&Extended == 0 {
		p.ri--
	}
	return RegOK
}

// literal is the parse_literal label of tre_parse.
func (p *parseCtx) literal(result **node, temporaryCflags *Flags) ErrCode {
	c := p.c
	if *temporaryCflags != 0 && p.ri+1 < len(p.re) && p.at(p.ri) == charBackslash && p.at(p.ri+1) == 'E' {
		p.cflags &^= *temporaryCflags
		*temporaryCflags = 0
		p.ri += 2
		return c.pushInt(int(parsePiece))
	}

	// We are expecting an atom. If the subexpression (or the whole regexp)
	// ends here, we interpret it as an empty expression (which matches an
	// empty string).
	if p.cflags&Literal == 0 &&
		(p.ri >= len(p.re) ||
			p.at(p.ri) == charStar ||
			(p.cflags&Extended != 0 &&
				(p.at(p.ri) == charPipe || p.at(p.ri) == charLBrace ||
					p.at(p.ri) == charPlus || p.at(p.ri) == charQuestionMark)) ||
			// Test for "\{" in BRE mode.
			(p.cflags&Extended == 0 && p.ri+1 < len(p.re) &&
				p.at(p.ri) == charBackslash && p.at(p.ri+1) == charLBrace)) {
		n := c.newLiteral(cEmpty, -1)
		if n == nil {
			return ErrESpace
		}
		*result = n
		return RegOK
	}

	if c.rcompat && p.cflags&Literal != 0 && p.at(p.ri) == 0 {
		// R's REG_LITERAL fix: an empty literal pattern matches "".
		n := c.newLiteral(cEmpty, -1)
		if n == nil {
			return ErrESpace
		}
		*result = n
		return RegOK
	}

	// Note that we can't use an isalpha() test here, since there may be
	// characters which are alphabetic but neither upper or lower case.
	ch := p.at(p.ri)
	if p.cflags&ICase != 0 && (isUpper(ch) || isLower(ch)) {
		tmp1 := c.newLiteral(int(toUpper(ch)), int(toUpper(ch)))
		if tmp1 == nil {
			return ErrESpace
		}
		tmp2 := c.newLiteral(int(toLower(ch)), int(toLower(ch)))
		if tmp2 == nil {
			return ErrESpace
		}
		if c.rcompat {
			// both case variants share one position in R's copy
			g := c.newGroup()
			tmp1.posGroup, tmp2.posGroup = g, g
		}
		u := c.newUnion(tmp1, tmp2)
		if u == nil {
			return ErrESpace
		}
		*result = u
	} else {
		n := c.newLiteral(int(ch), int(ch))
		if n == nil {
			return ErrESpace
		}
		*result = n
	}
	p.ri++
	return RegOK
}
