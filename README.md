# tre

A pure-Go port of [TRE](https://github.com/laurikari/tre), the POSIX regular
expression engine (leftmost-longest, submatch addressing with tags, lazy
quantifiers, back-references, `\<` `\>` `\b` `\w` `\d` `\s`, `(?i)` and
friends) that R uses for its default patterns (`perl = FALSE`, `fixed = FALSE`).

* BSD 2-Clause, with the TRE notice kept in [NOTICE](NOTICE).
* Ported from upstream TRE at commit `71bfcaf` (`lib/tre-parse.c`,
  `tre-compile.c`, `tre-match-parallel.c`, `tre-match-backtrack.c`,
  `regexec.c`): parse, AST, tagged NFA, the parallel matcher, and the
  backtracking matcher used only when the pattern has back-references.
* `Options.RCompat` reproduces the behaviours of R 4.6.2's modified copy of TRE
  where they differ from upstream (listed below), so `github.com/go-embedded-r/r`
  answers like R.
* Resource limits: pattern size, AST nodes, NFA states and transitions, the
  size of the firstpos/lastpos sets, and match steps. A hostile pattern or
  subject gets an error (`ErrESpace`, `ErrSteps`), never an out-of-memory or a
  hang.

```go
re, err := tre.Compile(`(a|ab)(c|bcd)(d*)`, tre.Extended)
m, err := re.ExecString("abcd", 4, 0) // []tre.Match{{0,4},{0,1},{1,4},{4,4}}
```

Offsets are in characters (UTF-8 decoded), or bytes with `tre.UseBytes`.

## Not ported

Approximate matching (`tre-match-approx.c`, the `{~n}`, `{+n}`, `{1i+1d<2}`
bound syntax): a pattern using it fails with `ErrApprox`. The C library's
system-wctype, multibyte-string and user-string front ends: one engine over
`[]rune` code units replaces them. Character classes use Go's `unicode`
tables, so classes of non-ASCII characters can differ from a given C library's
`iswctype`.

## How it is checked

* **TRE's own tests**: `tests/retest.c` is built from upstream and run through a
  recording wrapper (`cmd/crefbuild -record`); `TestRetest` replays every call
  against the port: 286 compilations and 3514 executions (wide) and 3510
  (narrow) with byte-for-byte equal return codes and match offsets. Excluded and
  counted: the 4 narrow-API patterns of retest's EUC-JP section (20
  executions), which run in a multibyte locale the port does not have.
* **Differential fuzz** against upstream TRE built as C (`TestDifferential`,
  random patterns with lazy quantifiers, back-references, assertions, classes,
  every flag): 197,588 compared, 0 mismatches. Excluded and counted: 2,410
  approximate-matching patterns, 1 backtracking case cut by the step limit.
* **R 4.6.2** for the R-level behaviour, through `evalfuzz --mode regex` of
  go-embedded-r/r (grepl, sub, gsub, regexpr, gregexpr, regmatches, strsplit).
* Fuzz target `FuzzCompileExec` (corpus in `testdata/fuzz`), bomb tests that
  assert refusals under `GOMEMLIMIT` and `ulimit -v`, `govulncheck`, 3 OSes with
  `-race`, 386, every GOOS, wasm. The CI `oracle` lane rebuilds the C oracle at
  the pinned upstream commit.

## Where R's copy differs from upstream TRE

Found by diffing R 4.6.2 `src/extra/tre` against upstream (R's changes are GPL
and were used as specification only; the behaviours are re-implemented here and
judged by R itself):

* `{,n}` keeps a minimum of -1, so it matches n+1 repetitions
  (`^a{,3}$` matches `aaaa`); a bound that overflows an int means "unbounded,
  or 1"; a bound above 255 is "Invalid contents of {}" (never "Maximum
  repetition"); `{m,n}` expands to `(|x(|x))` rather than `(x(x)?)?`.
* An empty `REG_LITERAL` pattern matches the empty string.
* The backtracking matcher decides whether to retry from a later start position
  by the current character instead of the character at the start of the attempt:
  a back-reference search that fails at the end of the subject does not try
  later starts (`([abc]+)\w*\1` does not match `banana` in R).
* The items of a bracket expression (and the two cases of an `(?i)` literal)
  share one NFA position.
* `tre_copy_ast` forgets the negated classes of a copied literal: in wide
  mode `[^[:space:]]{1,3}` matches a space on its repetitions.
* Messages and limits: no `TRE_MAX_RE`, no `REG_BADMAX`, `assert` becomes an R
  error, one extra text for `tre_version`; `int` positions where upstream uses
  `size_t`.

Upstream changes R's old copy lacks, kept here: the pattern-length and
string-length limits, the saturating bound parser, the hex-escape length limit.
