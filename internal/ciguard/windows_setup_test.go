package ciguard

import (
	"regexp"
	"strings"
	"testing"
)

// windowsSetupScript provisions the native-Windows gate's build prerequisites
// (a 64-bit MinGW-w64 compiler and ASCII temporary directories) so the
// windows-build-test job does NOT depend on whatever the windows-latest image
// happens to preinstall. Issue #63 requires "clear setup for x64 MinGW-w64 and
// ASCII temporary directories"; relying on a preinstalled compiler silently
// breaks the day GitHub changes the image, so the repo provisions it itself and
// these tests keep that provisioning from being quietly deleted.
const windowsSetupScript = "scripts/windows_setup_mingw.ps1"

func readSetupScript(t *testing.T) string {
	t.Helper()
	body, err := readRepoFile(".", windowsSetupScript)
	if err != nil {
		t.Fatalf("the Windows setup script is missing: %v", err)
	}
	return body
}

// TestCIWorkflow_WindowsJobProvisionsPrereqsBeforeTheGate is the drift guard for
// blocker 1: the windows-build-test job must explicitly provision the race
// compiler and ASCII temp BEFORE it runs the gate, rather than trusting the
// image. Dropping the provisioning step (reverting to image-dependence) fails
// this test loudly inside `make verify`, on every platform, long before CI.
func TestCIWorkflow_WindowsJobProvisionsPrereqsBeforeTheGate(t *testing.T) {
	body, err := readRepoFile(".", ciWorkflow)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	job, ok := windowsJobBlock(body)
	if !ok {
		t.Fatal("ci.yml no longer defines a `windows-build-test:` job")
	}

	setupAt := strings.Index(job, windowsSetupScript)
	if setupAt < 0 {
		t.Fatalf("the windows-build-test job must provision prerequisites by running %s "+
			"(explicit 64-bit MinGW-w64 + ASCII temp), not rely on the windows-latest image; job block:\n%s",
			windowsSetupScript, job)
	}
	gateAt := strings.Index(job, windowsGateScript)
	if gateAt < 0 {
		t.Fatalf("the windows-build-test job must still call %s; job block:\n%s", windowsGateScript, job)
	}
	if setupAt >= gateAt {
		t.Fatalf("the windows-build-test job must run %s BEFORE %s, so the compiler and ASCII "+
			"TEMP/TMP/GOTMPDIR are in place when the gate builds -race; job block:\n%s",
			windowsSetupScript, windowsGateScript, job)
	}
}

// TestWindowsSetupScript_Provisions64BitMinGWAndFailsLoudly enforces the two
// non-negotiables of blocker 1's provisioning: the setup must obtain a genuine
// 64-bit (x86_64-w64-mingw32) compiler with an explicit install step, and it
// must FAIL LOUDLY (throw) when a 64-bit gcc cannot be provided — never let the
// gate degrade to a non-race run.
func TestWindowsSetupScript_Provisions64BitMinGWAndFailsLoudly(t *testing.T) {
	body := readSetupScript(t)

	if !regexp.MustCompile(`(?i)mingw`).MatchString(body) {
		t.Fatalf("%s must provision MinGW-w64", windowsSetupScript)
	}
	// An explicit install path (choco / pacman) is what makes the job
	// reproducible instead of image-dependent.
	if !regexp.MustCompile(`(?i)\bchoco\b|\bpacman\b`).MatchString(body) {
		t.Fatalf("%s must install the compiler explicitly (e.g. `choco install mingw`), "+
			"not assume the image already has one", windowsSetupScript)
	}
	if !strings.Contains(body, "x86_64") {
		t.Fatalf("%s must require a 64-bit x86_64-w64-mingw32 compiler; a 32-bit gcc cannot "+
			"build the race runtime", windowsSetupScript)
	}
	if !strings.Contains(body, "throw") {
		t.Fatalf("%s must `throw` when a 64-bit gcc cannot be provisioned — a missing race "+
			"compiler has to FAIL the job, never silently skip -race", windowsSetupScript)
	}
}

// TestWindowsSetupScript_ExportsAsciiTempToLaterSteps checks that the setup step
// creates an ASCII temporary directory and exports TEMP/TMP/GOTMPDIR through
// $GITHUB_ENV, so the gate step (and its MSYS2 GCC race build, which breaks on
// non-ASCII temp paths) inherits them.
func TestWindowsSetupScript_ExportsAsciiTempToLaterSteps(t *testing.T) {
	body := readSetupScript(t)

	if !strings.Contains(body, "GITHUB_ENV") {
		t.Fatalf("%s must export the ASCII temp variables to later steps via $env:GITHUB_ENV", windowsSetupScript)
	}
	for _, v := range []string{"TEMP", "TMP", "GOTMPDIR"} {
		if !strings.Contains(body, v) {
			t.Fatalf("%s must set an ASCII %s so MSYS2 GCC can create its temp files", windowsSetupScript, v)
		}
	}
}
