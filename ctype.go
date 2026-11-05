package tre

import "unicode"

// ctype is a character class (TRE's tre_ctype_t); 0 means "no class".
type ctype uint8

const (
	ctNone ctype = iota
	ctAlnum
	ctAlpha
	ctBlank
	ctCntrl
	ctDigit
	ctGraph
	ctLower
	ctPrint
	ctPunct
	ctSpace
	ctUpper
	ctXdigit
)

var ctypeNames = map[string]ctype{
	"alnum": ctAlnum, "alpha": ctAlpha, "blank": ctBlank, "cntrl": ctCntrl,
	"digit": ctDigit, "graph": ctGraph, "lower": ctLower, "print": ctPrint,
	"punct": ctPunct, "space": ctSpace, "upper": ctUpper, "xdigit": ctXdigit,
}

// treCharMax is TRE_CHAR_MAX for a 32-bit wchar_t.
const treCharMax = 0x7fffffff

func noBreakSpace(c rune) bool { return c == 0xa0 || c == 0x2007 || c == 0x202f }

func isSpace(c rune) bool {
	if c < 0x80 {
		return c == ' ' || (c >= '\t' && c <= '\r')
	}
	return unicode.IsSpace(c) && !noBreakSpace(c)
}

func isAlpha(c rune) bool {
	if c < 0x80 {
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return unicode.IsLetter(c)
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

func isAlnum(c rune) bool {
	if c < 0x80 {
		return isAlpha(c) || isDigit(c)
	}
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

func isLower(c rune) bool {
	if c < 0x80 {
		return c >= 'a' && c <= 'z'
	}
	return unicode.IsLower(c)
}

func isUpper(c rune) bool {
	if c < 0x80 {
		return c >= 'A' && c <= 'Z'
	}
	return unicode.IsUpper(c)
}

func isXdigit(c rune) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isCntrl(c rune) bool {
	return c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) || c == 0x2028 || c == 0x2029
}

func isBlank(c rune) bool {
	if c < 0x80 {
		return c == ' ' || c == '\t'
	}
	return unicode.Is(unicode.Zs, c) && !noBreakSpace(c)
}

func isPrint(c rune) bool {
	if c < 0x80 {
		return c >= 0x20 && c < 0x7f
	}
	return unicode.IsPrint(c) || (unicode.Is(unicode.Zs, c) && !noBreakSpace(c))
}

func isGraph(c rune) bool {
	return isPrint(c) && !isSpace(c) && !isBlank(c) && c != ' '
}

func isPunct(c rune) bool {
	if c < 0x80 {
		return c > 0x20 && c < 0x7f && !isAlnum(c)
	}
	return isGraph(c) && !isAlnum(c)
}

func toUpper(c rune) rune { return unicode.ToUpper(c) }
func toLower(c rune) rune { return unicode.ToLower(c) }

// isctype is tre_isctype: iswctype(c, class).
func isctype(c rune, cl ctype) bool {
	switch cl {
	case ctAlnum:
		return isAlnum(c)
	case ctAlpha:
		return isAlpha(c)
	case ctBlank:
		return isBlank(c)
	case ctCntrl:
		return isCntrl(c)
	case ctDigit:
		return isDigit(c)
	case ctGraph:
		return isGraph(c)
	case ctLower:
		return isLower(c)
	case ctPrint:
		return isPrint(c)
	case ctPunct:
		return isPunct(c)
	case ctSpace:
		return isSpace(c)
	case ctUpper:
		return isUpper(c)
	case ctXdigit:
		return isXdigit(c)
	}
	return false
}

func isWordChar(c rune) bool { return c == '_' || isAlnum(c) }
