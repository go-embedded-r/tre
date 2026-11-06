// Package tre is a Go port of TRE, the POSIX regular expression engine by
// Ville Laurikari that R uses for its default (perl=FALSE, fixed=FALSE)
// patterns. It ports tre-parse.c, tre-compile.c, tre-match-parallel.c,
// tre-match-backtrack.c and regexec.c from upstream TRE; the licence notice
// of TRE is in the file NOTICE.
//
// Not ported: approximate matching (tre-match-approx.c, the {~n} bound
// syntax); a pattern that uses it is rejected with ErrBadPat. The wide
// character, system wctype and multibyte-string front ends of the C library
// are replaced by one engine over []rune code units.
package tre

// ErrCode is a TRE error code; the zero value, RegOK, is success. ErrCode
// implements error. The messages are those of TRE's regerror.c.
type ErrCode int

// The error codes, in the order of tre.h's reg_errcode_t.
const (
	RegOK ErrCode = iota
	ErrNoMatch
	ErrBadPat
	ErrECollate
	ErrECType
	ErrEEscape
	ErrESubReg
	ErrEBrack
	ErrEParen
	ErrEBrace
	ErrBadBR
	ErrERange
	ErrESpace
	ErrBadRpt
	ErrBadMax
	// ErrSteps is not a TRE code: the match exceeded Limits.MaxSteps.
	ErrSteps
	// ErrApprox is not a TRE code: the pattern uses TRE's approximate
	// matching syntax ({~n}, {+n}, {1i+1d<2}), which is not ported.
	ErrApprox
)

var errMessages = [...]string{
	"No error",
	"No match",
	"Invalid regexp",
	"Unknown collating element",
	"Unknown character class name",
	"Trailing backslash",
	"Invalid back reference",
	"Missing ']'",
	"Missing ')'",
	"Missing '}'",
	"Invalid contents of {}",
	"Invalid character range",
	"Out of memory",
	"Invalid use of repetition operators",
	"Maximum repetition in {} larger than 255",
	"Match step limit exceeded",
	"Approximate matching syntax is not supported",
}

// Error returns the TRE message for the code.
func (c ErrCode) Error() string {
	if c < 0 || int(c) >= len(errMessages) {
		return "Unknown error"
	}
	return errMessages[c]
}
