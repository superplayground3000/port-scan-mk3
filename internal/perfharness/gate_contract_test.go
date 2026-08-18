package perfharness_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPerformanceGateEntrypointsKeepOSAdaptersThin(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	makefile := readContractFile(t, root, "Makefile")
	linux := readContractFile(t, root, "scripts/performance_gate.sh")
	windows := readContractFile(t, root, "scripts/performance_gate.ps1")
	workflow := readContractFile(t, root, ".github/workflows/ci.yml")
	dockerIgnore := readContractFile(t, root, ".dockerignore")

	if !strings.Contains(makefile, "verify-performance:") || !strings.Contains(makefile, "scripts/performance_gate.sh full") {
		t.Fatal("Makefile does not expose the complete Linux performance matrix")
	}
	for _, required := range []string{"mktemp -d", "50000000000", "/usr/bin/time", "internal/perfharness/cmd/perf-harness"} {
		if !strings.Contains(linux, required) {
			t.Errorf("Linux adapter lacks %q", required)
		}
	}
	if !strings.Contains(linux, "TestScanInterruptContext_OnLinux_") {
		t.Fatal("Linux adapter does not run automated SIGINT cases")
	}
	// The hardware-qualified thresholds leave the correctness gate only because
	// this adapter still runs them. Dropping the step retires them in silence.
	// The certified profile must guard the step: the smoke profile runs on
	// shared CI hardware, which cannot hold these thresholds.
	qualifiedCases := strings.Index(linux, "-tags perfqualified")
	if qualifiedCases < 0 {
		t.Fatal("Linux adapter does not run the hardware-qualified cases")
	}
	if !strings.Contains(linux[:qualifiedCases], `"$profile" == "full"`) {
		t.Fatal("Linux adapter does not limit the hardware-qualified cases to the certified profile")
	}
	if strings.Index(linux, "mv \"$qualified_log\"") < qualifiedCases {
		t.Fatal("Linux adapter does not preserve the hardware-qualified log after the cases run")
	}
	if !strings.Contains(linux, "matrix_status=$?") || !strings.Contains(linux, "exit \"$matrix_status\"") {
		t.Fatal("Linux adapter does not preserve artifacts before it returns the matrix status")
	}
	linuxStdoutMove := strings.Index(linux, "mv \"$stdout_log\"")
	linuxStderrMove := strings.Index(linux, "mv \"$stderr_log\"")
	linuxExit := strings.Index(linux, "exit \"$matrix_status\"")
	if linuxStdoutMove < 0 || linuxStderrMove < 0 || linuxExit < linuxStdoutMove || linuxExit < linuxStderrMove {
		t.Fatal("Linux adapter does not preserve stdout and stderr before it returns the matrix status")
	}
	for _, forbidden := range []string{"12.5", "11.0", "1.25", "rm -rf"} {
		if strings.Contains(linux, forbidden) || strings.Contains(windows, forbidden) {
			t.Errorf("an OS adapter contains threshold or destructive cleanup text %q", forbidden)
		}
	}
	for _, required := range []string{"PeakWorkingSet64", "PeakPagedMemorySize64", "internal/perfharness/cmd/perf-harness", "performance smoke"} {
		if !strings.Contains(windows, required) {
			t.Errorf("Windows adapter lacks %q", required)
		}
	}
	if !strings.Contains(windows, "TestScanInterruptContext_OnWindows_") {
		t.Fatal("Windows adapter does not run the bounded Ctrl+Break case")
	}
	windowsMove := strings.Index(windows, "Move-Item -LiteralPath $stdoutLog")
	windowsExit := strings.Index(windows, "exit $exitCode")
	if windowsMove < 0 || windowsExit < windowsMove {
		t.Fatal("Windows adapter does not preserve artifacts before it returns the matrix status")
	}
	if strings.Count(workflow, "performance smoke") < 2 || !strings.Contains(workflow, "100000 items and 100 MB") {
		t.Fatal("CI does not run bounded Linux and Windows performance smoke")
	}
	if !strings.Contains(dockerIgnore, "performance-out/") {
		t.Fatal("Docker build contexts do not exclude generated performance artifacts")
	}
}

// The committed-memory budgets are hardware-qualified thresholds. This case
// runs on every platform, so it holds the build tag even where the behavioural
// check in rich_memory_qualification_linux_test.go cannot compile the package.
func TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	budgets := readContractFile(t, root, "internal/perfharness/rich_memory_linux_test.go")
	tag, _, found := strings.Cut(budgets, "\n")
	if !found || !strings.HasPrefix(tag, "//go:build ") {
		t.Fatalf("rich memory budgets have no build constraint on the first line: %q", tag)
	}
	if !strings.Contains(tag, "perfqualified") {
		t.Fatalf("rich memory budgets are not hardware-qualified: %q", tag)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readContractFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
