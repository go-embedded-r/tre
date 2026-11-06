// Command crefbuild builds the C oracle (upstream TRE with the differential
// harness and the retest recorder) and, with -record, writes the recordings of
// TRE's own tests to testdata.
//
//	crefbuild -upstream <clone of laurikari/tre> -out <dir> [-record <testdata dir>]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-embedded-r/tre/internal/cref"
)

func main() {
	upstream := flag.String("upstream", "", "clone of github.com/laurikari/tre")
	out := flag.String("out", "", "directory for the built binaries")
	record := flag.String("record", "", "directory to write retest recordings to")
	here := flag.String("here", "internal/cref/c", "directory of harness.c")
	flag.Parse()
	if *upstream == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	b, err := cref.Build(*upstream, *here, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("harness:", b.Harness)
	if *record != "" {
		for name, bin := range map[string]string{"retest-narrow.txt": b.Retest, "retest-wide.txt": b.Wretest} {
			if err := cref.Record(bin, filepath.Join(*record, name)); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := cref.Dedupe(filepath.Join(*record, name)); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("recorded", name)
		}
	}
}
