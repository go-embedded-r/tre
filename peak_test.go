package tre

import (
	"runtime"
	"testing"
)

// bombPeakBudget is the most memory the Go runtime may have taken from the OS
// (a high-water mark, cumulative over the bomb tests of the process): a wasm32
// heap, go-notebook's playground, has 4 GiB and shares it with the program.
// The same budget applies on every target, wasm included.
const bombPeakBudget = 256 << 20

// checkPeak fails the test when the process high-water mark is over budget;
// the bomb tests defer it.
func checkPeak(t *testing.T) {
	t.Helper()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("%s: process high-water %d MiB (budget %d MiB)", t.Name(), m.Sys>>20, bombPeakBudget>>20)
	if m.Sys > bombPeakBudget {
		t.Errorf("peak memory %d MiB is over the %d MiB budget", m.Sys>>20, bombPeakBudget>>20)
	}
}
