// Package repohygiene holds repository-level invariants that are not about the
// scanner's behaviour but about the checkout itself staying usable on every
// platform the project supports.
//
// The invariant guarded here is line endings. The quality gate runs
// `gofmt -l` (scripts/verify.sh) and gofmt rejects a file whose lines end in
// CRLF. Git's `core.autocrlf=true` — the default that the Git for Windows
// installer offers, and the one used on many Windows developer machines —
// rewrites LF to CRLF at checkout time unless the repository itself declares
// otherwise via .gitattributes. Without that declaration a pristine Windows
// checkout fails the formatting gate before a single line is edited
// (issue #64).
//
// These tests run on Linux and on Windows (CI runs `go test ./...` on both),
// and they do not depend on the line endings of the checkout they run in:
// TestSimulatedWindowsAutocrlfCheckout_* re-materialises the tracked files
// through git with core.autocrlf=true forced on, so the Windows failure mode
// is reproduced deterministically even from a Linux runner.
package repohygiene

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// lfPathspecs selects the tracked text files whose working-tree content must be
// LF on every platform: Go sources (gofmt gate) and POSIX shell scripts
// (scripts/verify.sh, e2e/run_e2e.sh, the git hooks — all run under bash, and
// bash chokes on CRLF).
var lfPathspecs = []string{
	"*.go",
	"*.sh",
	".githooks/*",
}

func TestSimulatedWindowsAutocrlfCheckout_KeepsGoAndShellFilesLF(t *testing.T) {
	root := repoRoot(t)
	exported := exportWithAutocrlfTrue(t, root, lfPathspecs)

	var crlf []string
	for _, rel := range exported.files {
		data, err := os.ReadFile(filepath.Join(exported.dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read exported %s: %v", rel, err)
		}
		if bytes.Contains(data, []byte("\r\n")) {
			crlf = append(crlf, rel)
		}
	}

	if len(crlf) > 0 {
		t.Fatalf("a checkout with core.autocrlf=true produced %d CRLF file(s) that must be LF; "+
			"the repository needs .gitattributes rules pinning them to eol=lf.\nfirst offenders: %s",
			len(crlf), strings.Join(firstN(crlf, 10), ", "))
	}
}

func TestSimulatedWindowsAutocrlfCheckout_IsGofmtClean(t *testing.T) {
	root := repoRoot(t)
	exported := exportWithAutocrlfTrue(t, root, []string{"*.go"})
	if len(exported.files) == 0 {
		t.Fatal("no tracked Go files were exported; the pathspec or the checkout is wrong")
	}

	gofmt := gofmtPath(t)
	args := make([]string, 0, len(exported.files)+2)
	args = append(args, "-l", "--")
	for _, rel := range exported.files {
		args = append(args, filepath.FromSlash(rel))
	}

	cmd := exec.Command(gofmt, args...)
	cmd.Dir = exported.dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gofmt -l failed to run: %v\nstderr: %s", err, stderr.String())
	}

	unformatted := strings.Fields(strings.TrimSpace(string(out)))
	if len(unformatted) > 0 {
		t.Fatalf("gofmt -l reported %d file(s) as unformatted in a pristine core.autocrlf=true checkout; "+
			"this is exactly the formatting gate failing on a clean Windows clone (issue #64).\n"+
			"first offenders: %s",
			len(unformatted), strings.Join(firstN(unformatted, 10), ", "))
	}
}

// TestTrackedTextFiles_DeclareAnExplicitEOL is the regression guard: it fails
// if a future change deletes .gitattributes or stops covering a file type, even
// on a machine where core.autocrlf is not set and the content check would pass
// by luck.
func TestTrackedTextFiles_DeclareAnExplicitEOL(t *testing.T) {
	root := repoRoot(t)
	samples := []struct {
		path string
		want string
	}{
		{"cmd/port-scan/main.go", "lf"},
		{"scripts/verify.sh", "lf"},
		{"e2e/run_e2e.sh", "lf"},
		{".githooks/pre-commit", "lf"},
		{"Makefile", "lf"},
		{"Dockerfile", "lf"},
		{"go.mod", "lf"},
		{".github/workflows/ci.yml", "lf"},
		{"README.md", "lf"},
		// Deliberate exception: batch files must reach Windows as CRLF.
		{"tools/example.bat", "crlf"},
		{"tools/example.cmd", "crlf"},
	}

	for _, s := range samples {
		got := checkAttr(t, root, "eol", s.path)
		if got != s.want {
			t.Errorf("git check-attr eol %s = %q, want %q; .gitattributes must pin this path", s.path, got, s.want)
		}
	}
}

// TestTrackedBinaries_AreNotEOLConverted guards the other direction: a
// catch-all text rule must never touch the committed binaries, or a Windows
// checkout would corrupt them.
func TestTrackedBinaries_AreNotEOLConverted(t *testing.T) {
	root := repoRoot(t)
	binaries := []string{
		"dist/windows/port-scan.exe",
		"dist/linux/port-scan",
		"docs/slides/architecture-review/port-scan-mk3-architecture-review.pptx",
	}

	for _, path := range binaries {
		if got := checkAttr(t, root, "text", path); got != "unset" {
			t.Errorf("git check-attr text %s = %q, want %q (binaries must never be EOL-converted)", path, got, "unset")
		}
	}
}

// TestBatchFilesAreNormalisedInTheIndexButCRLFOnDisk pins the deliberate CRLF
// exception end to end: `text` must be set (so the committed blob is LF and
// diffs stay sane whoever commits it) while `eol` must be crlf (so cmd.exe
// receives the CR it needs). Getting only one of the two is the subtle
// mistake this test exists to catch.
func TestBatchFilesAreNormalisedInTheIndexButCRLFOnDisk(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, ".gitattributes")); err != nil {
		t.Fatalf("the repository must own a .gitattributes at its root: %v", err)
	}

	for _, path := range []string{"tools/example.bat", "tools/example.cmd"} {
		if got := checkAttr(t, root, "text", path); got != "set" {
			t.Errorf("git check-attr text %s = %q, want %q (batch blobs must still be LF in the index)", path, got, "set")
		}
		if got := checkAttr(t, root, "eol", path); got != "crlf" {
			t.Errorf("git check-attr eol %s = %q, want %q (cmd.exe needs CRLF on disk)", path, got, "crlf")
		}
	}
}

// TestGofmtBinaryName_UsesExeSuffixOnWindows pins the fallback path used when
// gofmt is not on PATH. GOROOT/bin holds gofmt.exe on Windows, so a bare
// "gofmt" would stat a path that never exists and fail the whole
// repo-hygiene suite on a Windows developer machine that installed Go from the
// zip archive (no PATH entry) — a platform-only failure this Linux box cannot
// otherwise catch.
func TestGofmtBinaryName_UsesExeSuffixOnWindows(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want string
	}{
		{"windows", "gofmt.exe"},
		{"linux", "gofmt"},
		{"darwin", "gofmt"},
	} {
		if got := gofmtBinaryName(tc.goos); got != tc.want {
			t.Errorf("gofmtBinaryName(%q) = %q, want %q", tc.goos, got, tc.want)
		}
	}
}

type exportedTree struct {
	dir   string
	files []string // slash-separated, relative to dir
}

// exportWithAutocrlfTrue materialises the tracked files matching pathspecs into
// a scratch directory using git's working-tree conversion with
// core.autocrlf=true forced on — i.e. exactly what a Windows developer with the
// installer default gets from `git clone`. It is deterministic and platform
// independent: the conversion is done by git, not by the host OS.
func exportWithAutocrlfTrue(t *testing.T, root string, pathspecs []string) exportedTree {
	t.Helper()

	listArgs := append([]string{"ls-files", "-z", "--"}, pathspecs...)
	raw := gitOutput(t, root, listArgs...)
	var files []string
	for _, name := range strings.Split(raw, "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		t.Fatalf("git ls-files %v matched nothing", pathspecs)
	}

	dir := t.TempDir()
	// git wants a trailing separator and accepts forward slashes on Windows.
	prefix := filepath.ToSlash(dir)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	cmd := exec.Command("git", "-c", "core.autocrlf=true", "checkout-index", "-f", "--prefix="+prefix, "--stdin", "-z")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(files, "\x00") + "\x00")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git checkout-index with core.autocrlf=true failed: %v\nstderr: %s", err, stderr.String())
	}

	return exportedTree{dir: dir, files: files}
}

// checkAttr returns the value git resolves for one attribute on one path.
// The path need not exist: .gitattributes rules are pure pattern matching, so
// this also covers file types the repository does not have yet.
func checkAttr(t *testing.T, root, attr, path string) string {
	t.Helper()
	out := gitOutput(t, root, "check-attr", attr, "--", path)
	// Format: "<path>: <attr>: <value>"
	idx := strings.LastIndex(out, ": ")
	if idx < 0 {
		t.Fatalf("unexpected git check-attr output: %q", out)
	}
	return strings.TrimSpace(out[idx+2:])
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimRight(string(out), "\r\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return gitOutput(t, wd, "rev-parse", "--show-toplevel")
}

// gofmtBinaryName returns the file name of the gofmt executable on goos.
// Windows needs the .exe suffix: GOROOT/bin holds gofmt.exe there, so joining
// a bare "gofmt" would stat a path that never exists.
func gofmtBinaryName(goos string) string {
	if goos == "windows" {
		return "gofmt.exe"
	}
	return "gofmt"
}

func gofmtPath(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("gofmt"); err == nil {
		return p
	}
	goroot := gitFreeCommand(t, "go", "env", "GOROOT")
	p := filepath.Join(goroot, "bin", gofmtBinaryName(runtime.GOOS))
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("gofmt not found on PATH nor at %s: %v", p, err)
	}
	return p
}

func gitFreeCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func firstN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[:n]
}
