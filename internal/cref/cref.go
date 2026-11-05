// Package cref builds upstream TRE (C) with the harness and recorder of this
// directory and drives the harness: it is the engine-level oracle of the Go
// port's tests.
package cref

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Bin is a set of built oracle binaries.
type Bin struct {
	Harness string // the differential harness
	Retest  string // upstream tests/retest.c with the recorder, narrow
	Wretest string // the same, wide (WRETEST)
}

var libFiles = []string{"regerror.c", "tre-ast.c", "tre-compile.c", "tre-match-backtrack.c",
	"tre-match-parallel.c", "tre-mem.c", "tre-parse.c", "tre-stack.c", "xmalloc.c"}

var renames = []string{"tre_regncomp", "tre_regcomp", "tre_regnexec", "tre_regexec",
	"tre_regwncomp", "tre_regwcomp", "tre_regwnexec", "tre_regwexec"}

func cc(dir string, args ...string) error {
	cmd := exec.Command("cc", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cc %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// Build compiles upstream TRE (the clone at upstream) into out. here is this
// package's directory (it holds harness.c, recorder.c and tre-config.h).
func Build(upstream, here, out string) (*Bin, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	var err error
	if here, err = filepath.Abs(here); err != nil {
		return nil, err
	}
	if upstream, err = filepath.Abs(upstream); err != nil {
		return nil, err
	}
	// tre-internal.h tests HAVE_WCTYPE_H before it includes tre.h (which
	// includes the configuration): force the configuration in first.
	inc := []string{"-O1", "-w", "-include", filepath.Join(here, "tre-config.h"), "-I", here, "-I", filepath.Join(upstream, "include"), "-I", filepath.Join(upstream, "lib")}
	lib := func(f string) string { return filepath.Join(upstream, "lib", f) }
	var libs []string
	for _, f := range libFiles {
		libs = append(libs, lib(f))
	}

	// The harness links the unmodified library.
	b := &Bin{Harness: filepath.Join(out, "harness")}
	args := append(append([]string{}, inc...), "-o", b.Harness, filepath.Join(here, "harness.c"), lib("regcomp.c"), lib("regexec.c"))
	if err := cc(out, append(args, libs...)...); err != nil {
		return nil, err
	}

	// The recorder wraps the entry points of regcomp.c and regexec.c.
	var defs []string
	for _, n := range renames {
		defs = append(defs, "-D"+n+"=real_"+strings.TrimPrefix(n, "tre_"))
	}
	for _, f := range []string{"regcomp.c", "regexec.c"} {
		o := filepath.Join(out, strings.TrimSuffix(f, ".c")+".o")
		args := append(append(append([]string{}, inc...), defs...), "-c", lib(f), "-o", o)
		if err := cc(out, args...); err != nil {
			return nil, err
		}
	}
	objs := []string{filepath.Join(out, "regcomp.o"), filepath.Join(out, "regexec.o"), filepath.Join(here, "recorder.c")}
	b.Retest = filepath.Join(out, "retest")
	// retest.c enables the n-variants when TRE_VERSION is defined before it
	// includes tre.h, which only the autotools build arranges.
	args = append(append([]string{}, inc...), "-DHAVE_REGNEXEC=1", "-DHAVE_REGNCOMP=1", "-o", b.Retest, filepath.Join(upstream, "tests", "retest.c"))
	if err := cc(out, append(append(args, objs...), libs...)...); err != nil {
		return nil, err
	}
	b.Wretest = filepath.Join(out, "wretest")
	args = append(append([]string{}, inc...), "-DWRETEST", "-DHAVE_REGNEXEC=1", "-DHAVE_REGNCOMP=1", "-o", b.Wretest, filepath.Join(upstream, "tests", "retest.c"))
	if err := cc(out, append(append(args, objs...), libs...)...); err != nil {
		return nil, err
	}
	return b, nil
}

// Record runs a recording retest binary and returns the recording.
func Record(bin, recordFile string) error {
	os.Remove(recordFile)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "TRE_RECORD="+recordFile, "LC_ALL=en_US.UTF-8")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v\n%s", bin, err, out)
	}
	// retest prints "." and errors; an error line means upstream disagrees
	// with its own expectations.
	if strings.Contains(string(out), "Comp error") || strings.Contains(string(out), "Exec error") {
		return fmt.Errorf("%s reported errors:\n%s", bin, out)
	}
	return nil
}

// Harness is a running differential harness process.
type Harness struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// Start launches the harness binary.
func Start(bin string) (*Harness, error) {
	cmd := exec.Command(bin)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Harness{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

// Close stops the harness.
func (h *Harness) Close() {
	h.in.Close()
	h.cmd.Wait()
}

func enc(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	return hex.EncodeToString(b)
}

// Result is the harness' reply.
type Result struct {
	Err     int // compile error code (>0), or 0
	NoMatch bool
	Nsub    int
	Match   [][2]int
}

// Do runs one request. mode is 'w' (UTF-8 pattern and subject, offsets in
// characters) or 'b' (bytes).
func (h *Harness) Do(mode byte, cflags, eflags, nmatch int, pattern, subject []byte) (Result, error) {
	fmt.Fprintf(h.in, "%c %d %d %d %s %s\n", mode, cflags, eflags, nmatch, enc(pattern), enc(subject))
	line, err := h.out.ReadString('\n')
	if err != nil {
		return Result{}, fmt.Errorf("harness died: %v", err)
	}
	f := strings.Fields(line)
	switch f[0] {
	case "ERR":
		c, _ := strconv.Atoi(f[1])
		return Result{Err: c}, nil
	case "NOMATCH":
		return Result{NoMatch: true}, nil
	}
	r := Result{}
	r.Nsub, _ = strconv.Atoi(f[1])
	for _, p := range f[2:] {
		so, eo, _ := strings.Cut(p, ",")
		a, _ := strconv.Atoi(so)
		b, _ := strconv.Atoi(eo)
		r.Match = append(r.Match, [2]int{a, b})
	}
	return r, nil
}

// Dedupe rewrites a recording keeping each compile line once and each
// (compile, execute) pair once: retest runs every case with several matcher
// flag combinations and both regexec and regnexec.
func Dedupe(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out strings.Builder
	seen := map[string]bool{}
	cur, curEmitted := "", false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.HasPrefix(line, "C ") {
			cur, curEmitted = line, false
			// a compile that failed has no executions: emit it at once
			f := strings.Fields(line)
			if f[len(f)-1] != "0" && !seen[line] {
				seen[line] = true
				out.WriteString(line + "\n")
			}
			continue
		}
		key := cur + "\x00" + line
		if seen[key] {
			continue
		}
		seen[key] = true
		if !curEmitted {
			out.WriteString(cur + "\n")
			curEmitted = true
		}
		out.WriteString(line + "\n")
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}
