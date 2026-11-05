package tre

// Port of tre-ast.h / tre-ast.c (the abstract syntax tree) and the stack and
// allocator-limit bookkeeping of tre-stack.c / tre-mem.c.

type nodeType uint8

const (
	nLiteral nodeType = iota
	nCatenation
	nIteration
	nUnion
)

// Special code_min values of a literal node.
const (
	cEmpty     = -1 // empty leaf (denotes the empty string)
	cAssertion = -2
	cTag       = -3
	cBackref   = -4
)

// Assertion bits.
const (
	assertAtBOL          = 1
	assertAtEOL          = 2
	assertCharClass      = 4
	assertCharClassNeg   = 8
	assertAtBOW          = 16
	assertAtEOW          = 32
	assertAtWB           = 64
	assertAtWBNeg        = 128
	assertBackref        = 256
	assertLast           = 256
	maxNegClasses        = 64
	maxStackItems        = 1048576 // TRE_MAX_STACK
	tagMinimize          = 0
	tagMaximize          = 1
	reDupMax             = 255
	defaultMaxPatternLen = 65536 // TRE_MAX_RE
)

func isSpecial(b *body) bool { return b.codeMin < 0 }

// body is the "obj" of a TRE AST node: the literal, catenation, iteration or
// union payload. Copying a body between nodes is TRE's obj/type swap.
type body struct {
	typ nodeType
	// literal
	codeMin, codeMax int
	class            ctype
	negClasses       []ctype
	position         int
	// catenation and union
	left, right *node
	// iteration
	arg     *node
	min     int
	max     int
	minimal bool
}

type node struct {
	body
	nullable      int
	submatchID    int
	numSubmatches int
	numTags       int
	firstpos      []posTags
	lastpos       []posTags
}

// posTags is TRE's tre_pos_and_tags_t: an element of a firstpos/lastpos set.
type posTags struct {
	position   int
	codeMin    int
	codeMax    int
	tags       []int
	assertions int
	class      ctype
	negClasses []ctype
	backref    int
}

// stackItem is one entry of the explicit stack the parser and compiler use
// instead of recursion: an int, a node pointer or a pointer to a node slot.
type stackItem struct {
	i  int
	n  *node
	pp **node
}

// compiler holds the state shared by the parser and the compiler passes: the
// explicit stack and the resource counters behind Limits.
type compiler struct {
	limits   Limits
	rcompat  bool
	stack    []stackItem
	nodes    int
	setItems int
}

func (c *compiler) push(it stackItem) ErrCode {
	if len(c.stack) >= maxStackItems {
		return ErrESpace
	}
	c.stack = append(c.stack, it)
	return RegOK
}

func (c *compiler) pushInt(i int) ErrCode    { return c.push(stackItem{i: i}) }
func (c *compiler) pushNode(n *node) ErrCode { return c.push(stackItem{n: n}) }

func (c *compiler) pop() stackItem {
	it := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	return it
}

func (c *compiler) popInt() int    { return c.pop().i }
func (c *compiler) popNode() *node { return c.pop().n }

// newNode allocates an AST node; nil means the node limit was exceeded
// (REG_ESPACE in TRE when its allocator fails).
func (c *compiler) newNode(t nodeType) *node {
	c.nodes++
	if c.nodes > c.limits.maxNodes() {
		return nil
	}
	return &node{body: body{typ: t, position: -1}, nullable: -1, submatchID: -1}
}

func (c *compiler) newLiteral(codeMin, codeMax int) *node {
	n := c.newNode(nLiteral)
	if n == nil {
		return nil
	}
	n.codeMin = codeMin
	n.codeMax = codeMax
	return n
}

func (c *compiler) newIter(arg *node, min, max int, minimal bool) *node {
	n := c.newNode(nIteration)
	if n == nil {
		return nil
	}
	n.arg = arg
	n.min = min
	n.max = max
	n.minimal = minimal
	n.numSubmatches = arg.numSubmatches
	return n
}

func (c *compiler) newUnion(left, right *node) *node {
	n := c.newNode(nUnion)
	if n == nil {
		return nil
	}
	n.left = left
	n.right = right
	n.numSubmatches = left.numSubmatches + right.numSubmatches
	return n
}

func (c *compiler) newCatenation(left, right *node) *node {
	n := c.newNode(nCatenation)
	if n == nil {
		return nil
	}
	n.left = left
	n.right = right
	n.numSubmatches = left.numSubmatches + right.numSubmatches
	return n
}
