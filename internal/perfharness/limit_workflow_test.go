package perfharness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestRunTargetLimitCaseExecutesEveryRequiredBypassKind(t *testing.T) {
	t.Parallel()

	var harness perfharness.Harness = perfharness.New()
	for _, flagName := range []string{"-target-count-limit", "-target-memory-limit-gb"} {
		for _, bypass := range []perfharness.BypassCase{
			{Kind: perfharness.BypassExactDefault, Multiplier: 1},
			{Kind: perfharness.BypassDefaultPlusOne},
			{Kind: perfharness.BypassPositiveOverride, Multiplier: 2},
			{Kind: perfharness.BypassDisabledTwice, Multiplier: 2},
			{Kind: perfharness.BypassNegative},
			{Kind: perfharness.BypassOverflow},
		} {
			outputDir := filepath.Join(t.TempDir(), string(bypass.Kind))
			result, err := harness.RunTargetLimitCase(context.Background(), perfharness.TargetLimitSpec{
				OutputDir: outputDir,
				Flag:      flagName,
				Case:      bypass,
			})
			if err != nil {
				t.Fatalf("flag=%s kind=%s: %v", flagName, bypass.Kind, err)
			}
			if !result.Verdict.Passed || !result.Correctness.ExpectedValues {
				t.Fatalf("flag=%s kind=%s result=%+v", flagName, bypass.Kind, result)
			}
			if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
				t.Fatalf("flag=%s kind=%s performed I/O: %v", flagName, bypass.Kind, statErr)
			}
		}
	}
}
