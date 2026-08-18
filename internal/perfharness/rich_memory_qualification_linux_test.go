//go:build linux

package perfharness_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The committed-memory budgets in rich_memory_linux_test.go are
// hardware-qualified thresholds. They must stay OUT of the untagged build,
// which is what the correctness gate compiles, and they must stay IN the
// perfqualified build, which only the certified performance profile runs.
// A build tag alone does not retire a case from CI, and a text match cannot
// prove which build a case lands in, so this case asks the toolchain instead.
// See .claude/rules/50-lessons.md, 2026-08-16 and 2026-08-18.
func TestRichCommittedMemoryBudgetsStayHardwareQualified(t *testing.T) {
	t.Parallel()

	qualified := []string{
		"TestRichFixtureLoadFitsScaledCommittedMemoryBudget",
		"TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget",
	}

	untagged := listPerfHarnessTests(t)
	for _, name := range qualified {
		if untagged[name] {
			t.Errorf("%s is in the untagged build, so the correctness gate enforces a hardware-qualified budget on shared CI hardware", name)
		}
	}

	tagged := listPerfHarnessTests(t, "-tags", "perfqualified")
	for _, name := range qualified {
		if !tagged[name] {
			t.Errorf("%s is in no build, so the certified performance profile no longer enforces it", name)
		}
	}
}

// listPerfHarnessTests returns the test names the toolchain compiles into this
// package for the given build flags.
func listPerfHarnessTests(t *testing.T, flags ...string) map[string]bool {
	t.Helper()

	args := append([]string{"test", "-list", ".*"}, flags...)
	args = append(args, "./internal/perfharness")
	command := exec.Command("go", args...)
	command.Dir = repositoryRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	names := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		names[strings.TrimSpace(line)] = true
	}
	return names
}
