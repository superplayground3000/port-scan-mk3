// Package release holds tests that exercise the RELEASE build path — the parts
// of the contract that only exist once a binary has been linked with the
// Makefile's LDFLAGS. Nothing here can be proven by an in-process test: `go
// test` links no -X stamps at all, so a test that runs runMain and asserts the
// "dev" fallback passes just as happily against a build recipe whose stamps go
// nowhere. That is exactly how the dead `-X main.version` / `-X main.buildTime`
// flags survived unnoticed until issue #70.
//
// These tests therefore build real binaries with real ldflags and run them.
package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Sentinels chosen so they cannot occur in a real build or anywhere in the
// repository; if one appears in a binary's output it can only have come from
// the linker stamp under test.
const (
	sentinelVersion   = "v9.9.9-stamp-sentinel"
	sentinelBuildTime = "2001-02-03T04:05:06Z"
	sentinelCommit    = "0ddba11deadbeef0ddba11deadbeef0ddba11dea"
)

// commands are the five release binaries. Kept as an explicit list, not
// discovered from cmd/*, so that deleting a command's version wiring cannot
// silently shrink what this test covers.
var commands = []string{
	"cidr-compare",
	"csv-transform",
	"enrich-targets",
	"port-scan",
	"preprocess",
}

// TestReleaseLDFLAGS_StampVersionCommitAndBuildTimeIntoEveryCommand is the
// regression test for issue #70. It takes the ldflags string from the Makefile
// itself — substituting sentinels for the values make would compute — so a
// change that renames a stamped variable, drops a -X flag, or points one at a
// package the linker cannot write to fails here instead of shipping a binary
// that reports "dev".
func TestReleaseLDFLAGS_StampVersionCommitAndBuildTimeIntoEveryCommand(t *testing.T) {
	ldflags := makefileLDFLAGS(t)

	binDir := t.TempDir()
	// One invocation builds every command into binDir. Built for the host, not
	// cross-compiled, because the test must EXECUTE the result.
	build := exec.Command("go", "build", "-ldflags", ldflags, "-o", binDir+string(os.PathSeparator), "./cmd/...")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the commands with release ldflags failed: %v\n%s", err, out)
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			out := runVersion(t, filepath.Join(binDir, cmd+exeSuffix()))

			for label, want := range map[string]string{
				"version": sentinelVersion,
				"commit":  sentinelCommit,
				"built":   sentinelBuildTime,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("%s --version does not report the stamped %s %q; "+
						"the linker discarded that -X flag. Output:\n%s", cmd, label, want, out)
				}
			}
			if strings.Contains(out, "dev") || strings.Contains(out, "unknown") {
				t.Errorf("%s --version fell back to a placeholder despite being stamped:\n%s", cmd, out)
			}
			if !strings.HasPrefix(out, cmd+" version ") {
				t.Errorf("%s --version does not start with %q:\n%s", cmd, cmd+" version ", out)
			}
		})
	}
}

// A "-dirty" version means the tree was modified, so the artifact cannot be
// reproduced from a commit. The warning has to survive into the real binary,
// not just the unit test of the formatter.
func TestStampedDirtyVersion_WarnsInTheRealBinary(t *testing.T) {
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "port-scan"+exeSuffix())
	ldflags := "-X main.version=" + sentinelVersion + "-dirty" +
		" -X main.commit=" + sentinelCommit +
		" -X main.buildTime=" + sentinelBuildTime

	build := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, "./cmd/port-scan")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building port-scan failed: %v\n%s", err, out)
	}

	out := runVersion(t, bin)
	if !strings.Contains(out, "modified working tree") {
		t.Errorf("a -dirty stamped binary does not warn about the modified tree:\n%s", out)
	}
}

// makefileLDFLAGS reads the LDFLAGS assignment out of the Makefile and returns
// it with make's variable references replaced by this test's sentinels. It
// fails the test if any of the three stamps is absent, which is the static half
// of the issue #70 guard: a release recipe that stamps nothing is a bug even
// before anything is built.
func makefileLDFLAGS(t *testing.T) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), "Makefile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	// LDFLAGS := -ldflags "<flags>"
	re := regexp.MustCompile(`(?m)^LDFLAGS\s*:?=\s*-ldflags\s+"([^"]*)"`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatalf("no `LDFLAGS := -ldflags \"...\"` assignment found in %s; this test can no "+
			"longer verify the real release recipe and must be updated", path)
	}
	flags := string(m[1])

	replacements := map[string]string{
		"$(VERSION)":    sentinelVersion,
		"$(BUILD_TIME)": sentinelBuildTime,
		"$(COMMIT)":     sentinelCommit,
	}
	for _, stamp := range []struct{ variable, makeVar string }{
		{"main.version", "$(VERSION)"},
		{"main.buildTime", "$(BUILD_TIME)"},
		{"main.commit", "$(COMMIT)"},
	} {
		want := "-X " + stamp.variable + "=" + stamp.makeVar
		if !strings.Contains(flags, want) {
			t.Fatalf("Makefile LDFLAGS does not contain %q, so release binaries would not report "+
				"their %s. LDFLAGS is: %s", want, stamp.variable, flags)
		}
	}
	for makeVar, sentinel := range replacements {
		flags = strings.ReplaceAll(flags, makeVar, sentinel)
	}
	if strings.Contains(flags, "$(") {
		t.Fatalf("Makefile LDFLAGS still references an unresolved make variable after "+
			"substitution: %s", flags)
	}
	return flags
}

// runVersion executes `<bin> --version` and returns its stdout.
func runVersion(t *testing.T, bin string) string {
	t.Helper()
	var stdout, stderr strings.Builder
	cmd := exec.Command(bin, "--version")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s --version failed: %v\nstdout: %s\nstderr: %s", bin, err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("%s --version wrote to stderr: %s", bin, stderr.String())
	}
	return stdout.String()
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// repoRoot returns the repository root, derived from this test's own location
// (tests/release) so it does not depend on the working directory of the runner.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}
