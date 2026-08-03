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
//
// The assertions run against the script with comments stripped, and they match
// EXECUTED statement structure (an actual `choco install ...`/`pacman -S ...`
// invocation, an `^x86_64` triple guard, a `throw`) rather than mere token
// presence — a comment or an error-message string mentioning "choco" or
// "x86_64" must NOT be able to satisfy the test. Deleting the real install
// invocation makes this test fail, which is exactly the drift it must catch.
func TestWindowsSetupScript_Provisions64BitMinGWAndFailsLoudly(t *testing.T) {
	code := stripPowerShellComments(readSetupScript(t))

	if !regexp.MustCompile(`(?i)mingw`).MatchString(code) {
		t.Fatalf("%s must provision MinGW-w64", windowsSetupScript)
	}
	// An explicit install invocation (`choco install ...` or `pacman -S ...`) as
	// an executed statement is what makes the job reproducible instead of
	// image-dependent. Anchored to line start (optionally via the call operator
	// `&`) so a mention inside a comment or a throw-message string cannot pass.
	install := regexp.MustCompile(`(?im)^\s*&?\s*(choco\s+install\b[^\n]*\bmingw\b|pacman\s+-S\b[^\n]*mingw)`)
	if !install.MatchString(code) {
		t.Fatalf("%s must contain an executed compiler-install invocation "+
			"(e.g. `& choco install mingw -y`), not merely mention one; without it the job "+
			"silently reverts to depending on whatever the runner image preinstalls", windowsSetupScript)
	}
	// The 64-bit requirement must be an actual triple guard, not a word in prose:
	// the script matches the compiler's `-dumpmachine` output against `^x86_64`.
	tripleGuard := regexp.MustCompile(`-match\s+'\^x86_64'|-notmatch\s+'\^x86_64'`)
	if !tripleGuard.MatchString(code) {
		t.Fatalf("%s must guard on the compiler triple with an `^x86_64` (not)match; a 32-bit "+
			"gcc cannot build the race runtime and must be rejected structurally", windowsSetupScript)
	}
	if !regexp.MustCompile(`(?m)^\s*throw\b`).MatchString(code) {
		t.Fatalf("%s must `throw` (as a statement) when a 64-bit gcc cannot be provisioned — a "+
			"missing race compiler has to FAIL the job, never silently skip -race", windowsSetupScript)
	}
}

// stripPowerShellComments removes comment-based-help blocks (<# ... #>) and
// whole-line `# ...` comments, so a contract assertion cannot be satisfied by
// text that never executes. Trailing inline comments on code lines are left
// alone (removing them safely would require parsing PowerShell strings); the
// assertions here only need whole-line and block comments gone.
func stripPowerShellComments(s string) string {
	block := regexp.MustCompile(`(?s)<#.*?#>`)
	s = block.ReplaceAllString(s, "")
	var kept []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
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
