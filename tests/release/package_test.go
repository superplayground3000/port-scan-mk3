package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The packaging and smoke scripts are bash so that the SAME script runs on the
// Linux packaging runner and, through Git Bash, on the Windows runner that
// smoke-tests the artifacts. That also makes them testable here.
func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash is not available: %v", err)
	}
}

func requireTool(t *testing.T, tool string) {
	t.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		t.Skipf("%s is not available: %v", tool, err)
	}
}

func runScript(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fakeDist writes placeholder artifacts so the archive/checksum half of the
// packaging script can be exercised without a 10-binary cross-build.
func fakeDist(t *testing.T, target string, suffix string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, cmd := range commands {
		path := filepath.Join(dir, cmd+suffix)
		if err := os.WriteFile(path, []byte("placeholder for "+cmd), 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestPackageRelease_ProducesAnArchiveWithEveryCommandAndAVerifiableChecksum(t *testing.T) {
	requireBash(t)
	requireTool(t, "zip")
	requireTool(t, "unzip")

	dist := fakeDist(t, "windows", ".exe")
	out := filepath.Join(t.TempDir(), "release")

	stdout, err := runScript(t, "scripts/package_release.sh",
		"--dist", dist, "--out", out, "--version", "v9.9.9-test",
		"--target", "windows", "--skip-build")
	if err != nil {
		t.Fatalf("package_release.sh failed: %v\n%s", err, stdout)
	}

	archive := filepath.Join(out, "port-scan-mk3_v9.9.9-test_windows_amd64.zip")
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("expected archive %s: %v\nscript output:\n%s", archive, err, stdout)
	}

	listing, err := exec.Command("unzip", "-Z1", archive).Output()
	if err != nil {
		t.Fatalf("listing %s: %v", archive, err)
	}
	entries := strings.Fields(string(listing))
	for _, cmd := range commands {
		want := cmd + ".exe"
		if !containsString(entries, want) {
			t.Errorf("archive is missing %s; it contains %v", want, entries)
		}
	}
	// The per-binary manifest ships INSIDE the archive so an operator can
	// verify individual EXEs after extracting.
	if !containsString(entries, "SHA256SUMS.txt") {
		t.Errorf("archive is missing its inner SHA256SUMS.txt; entries: %v", entries)
	}

	// The outer manifest covers the archive itself and must verify with the
	// standard tool from the output directory.
	sums := filepath.Join(out, "SHA256SUMS.txt")
	data, err := os.ReadFile(sums)
	if err != nil {
		t.Fatalf("reading %s: %v", sums, err)
	}
	if !strings.Contains(string(data), sha256File(t, archive)) {
		t.Errorf("%s does not carry the archive's real sha256 %s:\n%s",
			sums, sha256File(t, archive), data)
	}
	if strings.Contains(string(data), out) || strings.Contains(string(data), dist) {
		t.Errorf("%s embeds absolute build paths, so it cannot be verified after "+
			"download:\n%s", sums, data)
	}
	if _, err := exec.LookPath("sha256sum"); err == nil {
		check := exec.Command("sha256sum", "--check", "--strict", "SHA256SUMS.txt")
		check.Dir = out
		if checkOut, err := check.CombinedOutput(); err != nil {
			t.Errorf("sha256sum --check on the published manifest failed: %v\n%s", err, checkOut)
		}
	}
}

// A release archive that changes every time it is built cannot be traced to the
// commit it claims, which is the whole point of issue #65/#73. Two packaging
// runs over identical inputs must produce identical bytes.
func TestPackageRelease_ArchiveIsReproducible(t *testing.T) {
	requireBash(t)
	requireTool(t, "zip")

	dist := fakeDist(t, "windows", ".exe")
	outA := filepath.Join(t.TempDir(), "a")
	outB := filepath.Join(t.TempDir(), "b")

	for _, out := range []string{outA, outB} {
		stdout, err := runScript(t, "scripts/package_release.sh",
			"--dist", dist, "--out", out, "--version", "v9.9.9-test",
			"--build-time", "2001-02-03T04:05:06Z",
			"--target", "windows", "--skip-build")
		if err != nil {
			t.Fatalf("package_release.sh failed: %v\n%s", err, stdout)
		}
	}

	name := "port-scan-mk3_v9.9.9-test_windows_amd64.zip"
	a := sha256File(t, filepath.Join(outA, name))
	b := sha256File(t, filepath.Join(outB, name))
	if a != b {
		t.Errorf("archive is not reproducible: %s vs %s", a, b)
	}
}

func TestPackageRelease_RefusesToArchiveAnIncompleteDist(t *testing.T) {
	requireBash(t)
	requireTool(t, "zip")

	dist := fakeDist(t, "windows", ".exe")
	if err := os.Remove(filepath.Join(dist, "windows", "preprocess.exe")); err != nil {
		t.Fatalf("removing an artifact: %v", err)
	}
	out := filepath.Join(t.TempDir(), "release")

	stdout, err := runScript(t, "scripts/package_release.sh",
		"--dist", dist, "--out", out, "--version", "v9.9.9-test",
		"--target", "windows", "--skip-build")
	if err == nil {
		t.Fatalf("packaging a dist that is missing preprocess.exe exited 0; it would "+
			"publish an incomplete release. Output:\n%s", stdout)
	}
	if !strings.Contains(stdout, "preprocess") {
		t.Errorf("the failure does not name the missing command:\n%s", stdout)
	}
}

// The smoke test is what stands between a broken build and a published release,
// so it has to be shown to FAIL on a bad artifact, not just pass on a good one.
func TestSmokeRelease_AcceptsStampedBinariesAndRejectsUnstampedOnes(t *testing.T) {
	requireBash(t)

	root := repoRoot(t)

	stamped := t.TempDir()
	ldflags := "-X main.version=" + sentinelVersion +
		" -X main.commit=" + sentinelCommit +
		" -X main.buildTime=" + sentinelBuildTime
	buildAll(t, root, stamped, ldflags)

	unstamped := t.TempDir()
	buildAll(t, root, unstamped, "")

	t.Run("stamped passes", func(t *testing.T) {
		out, err := runScript(t, "scripts/smoke_release.sh", stamped, sentinelVersion)
		if err != nil {
			t.Fatalf("smoke test rejected correctly stamped binaries: %v\n%s", err, out)
		}
		for _, cmd := range commands {
			if !strings.Contains(out, cmd) {
				t.Errorf("smoke output does not mention %s:\n%s", cmd, out)
			}
		}
	})

	t.Run("unstamped fails", func(t *testing.T) {
		out, err := runScript(t, "scripts/smoke_release.sh", unstamped, sentinelVersion)
		if err == nil {
			t.Fatalf("smoke test PASSED binaries reporting 'dev'; it would let an "+
				"unstamped release through. Output:\n%s", out)
		}
	})

	t.Run("wrong version fails", func(t *testing.T) {
		out, err := runScript(t, "scripts/smoke_release.sh", stamped, "v0.0.0-not-the-tag")
		if err == nil {
			t.Fatalf("smoke test PASSED binaries whose version does not match the tag. "+
				"Output:\n%s", out)
		}
	})

	t.Run("empty directory fails", func(t *testing.T) {
		out, err := runScript(t, "scripts/smoke_release.sh", t.TempDir(), sentinelVersion)
		if err == nil {
			t.Fatalf("smoke test passed vacuously over an empty directory:\n%s", out)
		}
	})
}

func buildAll(t *testing.T, root, outDir, ldflags string) {
	t.Helper()
	args := []string{"build"}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "-o", outDir+string(os.PathSeparator), "./cmd/...")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	_ = runtime.GOOS
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
