package ciguard

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// windowsGateScript is the native-Windows gate introduced by issue #63. It is a
// PowerShell script because it must drive real .exe files on windows-latest,
// but the contract it has to keep is asserted here so that `make verify` — which
// runs on Linux — catches drift before CI does.
const windowsGateScript = "scripts/windows_gate.ps1"

// ciWorkflow is the GitHub Actions workflow that must invoke the gate.
const ciWorkflow = ".github/workflows/ci.yml"

func readGateScript(t *testing.T) string {
	t.Helper()
	body, err := ReadRepoFile(".", windowsGateScript)
	if err != nil {
		t.Fatalf("the native Windows gate script is missing: %v", err)
	}
	return body
}

// TestWindowsGateScript_LaunchesEveryCommandUnderCmd is the drift guard the
// acceptance criteria call for ("all five packaged EXEs are launched"). The
// expectation is derived from the filesystem, not from a hardcoded five, so
// adding a sixth command under cmd/ fails this test until the gate covers it.
func TestWindowsGateScript_LaunchesEveryCommandUnderCmd(t *testing.T) {
	root, err := RepoRoot(".")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatalf("read cmd/: %v", err)
	}
	var want []string
	for _, e := range entries {
		if e.IsDir() {
			want = append(want, e.Name())
		}
	}
	if len(want) == 0 {
		t.Fatal("no command directories found under cmd/; the expectation would be vacuous")
	}
	sort.Strings(want)

	got := gateCommandList(t, readGateScript(t))
	sort.Strings(got)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the Windows gate builds/launches %v but cmd/ contains %v;\n"+
			"every command must be built as an .exe and launched by %s", got, want, windowsGateScript)
	}
}

// gateCommandList extracts the `$gateCommands = @('a', 'b', ...)` assignment.
func gateCommandList(t *testing.T, body string) []string {
	t.Helper()
	assignment := regexp.MustCompile(`(?s)\$gateCommands\s*=\s*@\((.*?)\)`)
	m := assignment.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("%s must declare the commands it covers as `$gateCommands = @('name', ...)`", windowsGateScript)
	}
	quoted := regexp.MustCompile(`'([^']+)'`)
	var out []string
	for _, q := range quoted.FindAllStringSubmatch(m[1], -1) {
		out = append(out, q[1])
	}
	if len(out) == 0 {
		t.Fatalf("$gateCommands in %s is empty", windowsGateScript)
	}
	return out
}

// TestWindowsGateScript_RunsTheRaceDetectorAndFailsWhenTheCompilerIsMissing
// covers the acceptance criterion "race detector is genuinely enabled, not
// silently skipped". On Windows `-race` needs cgo and a 64-bit MinGW-w64 gcc;
// if that compiler is absent the gate must abort, never continue.
func TestWindowsGateScript_RunsTheRaceDetectorAndFailsWhenTheCompilerIsMissing(t *testing.T) {
	body := readGateScript(t)

	if !regexp.MustCompile(`go\s+test\b[^\n]*-race\b`).MatchString(body) {
		t.Fatalf("%s must run `go test ... -race ...`", windowsGateScript)
	}
	if !regexp.MustCompile(`go\s+test\b[^\n]*-shuffle=on\b`).MatchString(body) {
		t.Fatalf("%s must run the race tests with -shuffle=on, mirroring scripts/verify.sh", windowsGateScript)
	}

	lines := strings.Split(body, "\n")
	probe := -1
	for i, l := range lines {
		if strings.Contains(l, "-dumpmachine") {
			probe = i
			break
		}
	}
	if probe < 0 {
		t.Fatalf("%s must probe the C compiler with `gcc -dumpmachine` before trusting -race", windowsGateScript)
	}
	window := strings.Join(lines[probe:min(probe+30, len(lines))], "\n")
	if !strings.Contains(window, "x86_64") {
		t.Fatalf("%s must require a 64-bit (x86_64-w64-mingw32) compiler; the check near line %d does not mention x86_64",
			windowsGateScript, probe+1)
	}
	if !strings.Contains(window, "throw") {
		t.Fatalf("%s must `throw` when the 64-bit MinGW-w64 compiler is missing — a missing compiler has to FAIL the job, not skip the race run (check near line %d)",
			windowsGateScript, probe+1)
	}

	// CGO must be switched on explicitly: -race on windows/amd64 is a no-op
	// request that fails to build without cgo, and leaving it to the default is
	// how the race run silently stops being a race run.
	if !strings.Contains(body, "CGO_ENABLED") {
		t.Fatalf("%s must set CGO_ENABLED=1 explicitly for the race run", windowsGateScript)
	}
}

// TestWindowsGateScript_TargetsLoopbackOnly enforces constitution V: the gate
// scans only 127.0.0.0/8. Any other IPv4 literal in the script is a bug.
func TestWindowsGateScript_TargetsLoopbackOnly(t *testing.T) {
	body := readGateScript(t)
	ipv4 := regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	for _, lit := range ipv4.FindAllString(body, -1) {
		if !strings.HasPrefix(lit, "127.") {
			t.Fatalf("%s contains the non-loopback address %q; the gate must only ever touch 127.0.0.0/8 (constitution V)",
				windowsGateScript, lit)
		}
	}
}

// TestWindowsGateScript_UsesAnOutputPathContainingSpaces covers the acceptance
// criterion about Windows path shapes: the whole pipeline must run under a
// directory whose name contains a space.
func TestWindowsGateScript_UsesAnOutputPathContainingSpaces(t *testing.T) {
	body := readGateScript(t)
	m := regexp.MustCompile(`\$gateWorkspaceName\s*=\s*'([^']*)'`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("%s must declare its scratch directory as `$gateWorkspaceName = '...'`", windowsGateScript)
	}
	if !strings.Contains(m[1], " ") {
		t.Fatalf("$gateWorkspaceName is %q; it must contain a space so the gate proves output paths with spaces work", m[1])
	}
}

// TestCIWorkflow_WindowsJobRunsTheGateScriptAndIsBlocking asserts the two
// properties the workflow owns: it delegates to the script (repo rule — logic
// lives in scripts, the workflow stays thin) and it is blocking.
func TestCIWorkflow_WindowsJobRunsTheGateScriptAndIsBlocking(t *testing.T) {
	body, err := ReadRepoFile(".", ciWorkflow)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	job := windowsJobBlock(t, body)

	if !strings.Contains(job, "scripts/windows_gate.ps1") {
		t.Fatalf("the windows-build-test job must call %s; job block:\n%s", windowsGateScript, job)
	}
	if !strings.Contains(job, "pwsh") {
		t.Fatalf("the windows-build-test job must run the gate with `shell: pwsh`; job block:\n%s", job)
	}
	// Comments are stripped first: this job's comment block deliberately warns
	// future editors not to re-add continue-on-error, and that warning must not
	// be mistaken for the setting itself.
	if strings.Contains(stripYAMLComments(job), "continue-on-error") {
		t.Fatalf("the windows-build-test job must stay BLOCKING; found a continue-on-error setting in:\n%s", job)
	}
	if !strings.Contains(job, "windows-latest") {
		t.Fatalf("the windows-build-test job must run on windows-latest; job block:\n%s", job)
	}
}

// windowsJobBlock slices ci.yml from the `windows-build-test:` job key to the
// next job key at the same indentation, so assertions cannot accidentally read
// a neighbouring job (the e2e job legitimately carries continue-on-error).
func windowsJobBlock(t *testing.T, workflow string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	start := -1
	for i, l := range lines {
		if l == "  windows-build-test:" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("ci.yml no longer defines a `windows-build-test:` job")
	}
	jobKey := regexp.MustCompile(`^  [A-Za-z0-9_-]+:\s*$`)
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if jobKey.MatchString(lines[i]) {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// stripYAMLComments drops whole-line `#` comments so prose about a setting is
// never confused with the setting.
func stripYAMLComments(block string) string {
	var kept []string
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}
