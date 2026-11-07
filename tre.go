package tre

import (
	"unicode/utf8"
)

// Flags are the tre_regcomp() flags, with the values of tre.h.
type Flags int

const (
	Extended   Flags = 1  // REG_EXTENDED
	ICase      Flags = 2  // REG_ICASE
	Newline    Flags = 4  // REG_NEWLINE
	NoSub      Flags = 8  // REG_NOSUB
	Literal    Flags = 16 // REG_LITERAL: the pattern is a literal string
	RightAssoc Flags = 32 // REG_RIGHT_ASSOC
	Ungreedy   Flags = 64 // REG_UNGREEDY
	// UseBytes is R's REG_USEBYTES: the pattern and the subject are
	// sequences of bytes (code units below 256), not characters; character
	// classes are expanded over 0..255 as TRE does in a single-byte locale.
	UseBytes Flags = 128
)

// EFlags are the tre_regexec() flags.
type EFlags int

const (
	NotBOL EFlags = 1 // REG_NOTBOL
	NotEOL EFlags = 2 // REG_NOTEOL
	// BacktrackingMatcher forces the backtracking matcher
	// (REG_BACKTRACKING_MATCHER).
	BacktrackingMatcher EFlags = 8
)

// Limits bound the resources of one compilation and one match; the zero
// value of a field selects its default. They make hostile patterns and
// inputs fail with an error instead of exhausting memory or time.
type Limits struct {
	// MaxPattern is the longest pattern in code units (default 65536, TRE's
	// TRE_MAX_RE).
	MaxPattern int
	// MaxNodes bounds the AST nodes, including those created by expanding
	// bounded repetition (default 1<<20).
	MaxNodes int
	// MaxStates bounds the positions (NFA states) (default 1<<19).
	MaxStates int
	// MaxTransitions bounds the NFA transitions (default 1<<23).
	MaxTransitions int
	// MaxSetItems bounds the entries of all firstpos/lastpos sets, whose
	// size can be quadratic in the pattern (default 1<<23).
	MaxSetItems int
	// MaxSteps bounds the work of one match: transitions examined by the
	// parallel matcher, loop iterations and transitions by the backtracking
	// matcher (default 1<<28; negative means unlimited).
	MaxSteps int
}

func (l Limits) maxNodes() int       { return def(l.MaxNodes, 1<<20) }
func (l Limits) maxStates() int      { return def(l.MaxStates, 1<<19) }
func (l Limits) maxTransitions() int { return def(l.MaxTransitions, 1<<23) }
func (l Limits) maxSetItems() int    { return def(l.MaxSetItems, 1<<23) }
func (l Limits) maxPattern() int     { return def(l.MaxPattern, defaultMaxPatternLen) }
func (l Limits) maxSteps() int {
	if l.MaxSteps < 0 {
		return 0
	}
	return def(l.MaxSteps, 1<<28)
}

func def(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// Options configure Compile.
type Options struct {
	Flags Flags
	// RCompat selects the behaviours of R's modified copy of TRE where they
	// differ from upstream TRE: {,n} keeps a minimum of -1 (so it matches
	// n+1 repetitions), a bound that overflows an int is "unbounded or 1",
	// a bound above 255 is "Invalid contents of {}" (never "Maximum
	// repetition"), {m,n} is expanded as (|x(|x...)) rather than (x(x...)?)?,
	// and an empty REG_LITERAL pattern matches "".
	RCompat bool
	Limits  Limits
}

// Regexp is a compiled pattern. It is safe for concurrent use.
type Regexp struct {
	t     *tnfa
	nsub  int
	flags Flags
	lim   Limits
}

// Compile compiles pattern, a UTF-8 string, with default options. With the
// UseBytes flag each byte of the pattern is one code unit.
func Compile(pattern string, flags Flags) (*Regexp, error) {
	return CompileOptions(pattern, Options{Flags: flags})
}

// CompileOptions compiles pattern with the given options.
func CompileOptions(pattern string, opt Options) (*Regexp, error) {
	return CompileRunes(decode(pattern, opt.Flags&UseBytes != 0), opt)
}

// decode turns a string into code units: bytes in byte mode, characters
// otherwise (an invalid UTF-8 byte becomes U+FFFD, as for utf8.DecodeRune).
func decode(s string, bytes bool) []rune {
	if bytes {
		r := make([]rune, len(s))
		for i := 0; i < len(s); i++ {
			r[i] = rune(s[i])
		}
		return r
	}
	return []rune(s)
}

// CompileRunes compiles a pattern given as code units.
func CompileRunes(pattern []rune, opt Options) (*Regexp, error) {
	if len(pattern) > opt.Limits.maxPattern() {
		return nil, ErrESpace
	}
	c := &compiler{limits: opt.Limits, rcompat: opt.RCompat}
	t, nsub, st := c.compile(pattern, opt.Flags)
	if st != RegOK {
		return nil, st
	}
	return &Regexp{t: t, nsub: nsub, flags: opt.Flags, lim: opt.Limits}, nil
}

// NumSubexp returns the number of parenthesized subexpressions.
func (re *Regexp) NumSubexp() int { return re.nsub }

// HaveBackrefs reports whether the pattern has back references (and so is
// matched by the backtracking matcher).
func (re *Regexp) HaveBackrefs() bool { return re.t.haveBackrefs }

// Exec matches the subject s, given as code units, and returns nmatch
// submatches (the first is the whole match). The error is ErrNoMatch when the
// subject does not match, ErrSteps when the step limit was exceeded.
func (re *Regexp) Exec(s []rune, nmatch int, eflags EFlags) ([]Match, error) {
	t := re.t
	if nmatch == 0 && eflags&BacktrackingMatcher == 0 {
		if m, done := t.dfaMatchRunes(s); done {
			return re.verdict(m)
		}
	}
	var tags []int
	if t.numTags > 0 && nmatch > 0 {
		tags = make([]int, t.numTags)
	}
	var eo int
	var st ErrCode
	if t.haveBackrefs || eflags&BacktrackingMatcher != 0 {
		eo, st = t.runBacktrack(s, tags, eflags, re.lim.maxSteps())
	} else {
		eo, st = t.runParallel(s, tags, eflags, re.lim.maxSteps())
	}
	if st != RegOK {
		return nil, st
	}
	pm := make([]Match, nmatch)
	t.fillPmatch(pm, t.cflags, tags, eo)
	return pm, nil
}

// ExecString matches the string s. In byte mode every byte is a code unit and
// the offsets are byte offsets; otherwise s is decoded as UTF-8 and the
// offsets are in characters. A subject that is not valid UTF-8 does not match
// (TRE's mbrtowc failure).
func (re *Regexp) ExecString(s string, nmatch int, eflags EFlags) ([]Match, error) {
	if nmatch == 0 && eflags&BacktrackingMatcher == 0 && (re.flags&UseBytes != 0 || isASCII(s)) {
		if m, done := re.t.dfaMatchBytes(s); done {
			return re.verdict(m)
		}
	}
	if re.flags&UseBytes != 0 {
		return re.Exec(decode(s, true), nmatch, eflags)
	}
	if !utf8.ValidString(s) {
		return nil, ErrNoMatch
	}
	return re.Exec([]rune(s), nmatch, eflags)
}

// MatchString reports whether s matches (no submatches are computed).
func (re *Regexp) MatchString(s string) bool {
	_, err := re.ExecString(s, 0, 0)
	return err == nil
}

func (re *Regexp) verdict(m bool) ([]Match, error) {
	if m {
		return []Match{}, nil
	}
	return nil, ErrNoMatch
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
