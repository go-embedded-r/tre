package tre

import (
	"fmt"
	"testing"
)

func benchLines(n int) [][]rune {
	out := make([][]rune, n)
	for i := 1; i <= n; i++ {
		out[i-1] = []rune(fmt.Sprintf("id%d user%d@host%d.example.org %d abc%d-xyz", i, i%97, i%13, i*7, i%1000))
	}
	return out
}

func benchExec(b *testing.B, pat string, nmatch int) {
	re, err := CompileOptions(pat, Options{Flags: Extended | UseBytes, RCompat: true})
	if err != nil {
		b.Fatal(err)
	}
	lines := benchLines(20000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, l := range lines {
			re.Exec(l, nmatch, 0)
		}
	}
}

func BenchmarkGrepl(b *testing.B)     { benchExec(b, `[0-9]+@host[0-9]+\.example`, 0) }
func BenchmarkRegexpr(b *testing.B)   { benchExec(b, `abc[0-9]+`, 1) }
func BenchmarkGsubGroup(b *testing.B) { benchExec(b, `([a-z]+)([0-9]+)`, 3) }
func BenchmarkBackref(b *testing.B)   { benchExec(b, `([0-9])\1`, 0) }

// BenchmarkGsubLoop is gsub("([a-z]+)([0-9]+)", ...): every match of every line.
func BenchmarkGsubLoop(b *testing.B) {
	re, _ := CompileOptions(`([a-z]+)([0-9]+)`, Options{Flags: Extended | UseBytes, RCompat: true})
	lines := benchLines(20000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, l := range lines {
			pos := 0
			for pos <= len(l) {
				pm, err := re.Exec(l[pos:], 3, NotBOL)
				if err != nil {
					break
				}
				if pm[0].Eo == pm[0].So {
					pos++
				}
				pos += pm[0].Eo
			}
		}
	}
}
