package buildinfo_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/buildinfo"
)

func TestResolve_StampedValues_AreReportedVerbatim(t *testing.T) {
	info := buildinfo.Resolve("port-scan", "v2.2.0", "2026-08-04T10:22:31Z", "5b2fb97dcafe")

	if info.Name != "port-scan" {
		t.Errorf("Name = %q, want %q", info.Name, "port-scan")
	}
	if info.Version != "v2.2.0" {
		t.Errorf("Version = %q, want %q", info.Version, "v2.2.0")
	}
	if info.BuildTime != "2026-08-04T10:22:31Z" {
		t.Errorf("BuildTime = %q, want %q", info.BuildTime, "2026-08-04T10:22:31Z")
	}
	if info.Commit != "5b2fb97dcafe" {
		t.Errorf("Commit = %q, want %q", info.Commit, "5b2fb97dcafe")
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	wantPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != wantPlatform {
		t.Errorf("Platform = %q, want %q", info.Platform, wantPlatform)
	}
}

// An unstamped binary (plain `go build`, or `go test`) must still answer the
// version question rather than printing empty fields.
func TestResolve_UnstampedValues_FallBackToDocumentedPlaceholders(t *testing.T) {
	info := buildinfo.Resolve("preprocess", "", "", "")

	if info.Version != "dev" {
		t.Errorf("Version = %q, want %q", info.Version, "dev")
	}
	if info.Commit != "unknown" {
		t.Errorf("Commit = %q, want %q", info.Commit, "unknown")
	}
	if info.BuildTime != "unknown" {
		t.Errorf("BuildTime = %q, want %q", info.BuildTime, "unknown")
	}
}

func TestResolve_BlankNameFallsBackToUnknown(t *testing.T) {
	if got := buildinfo.Resolve("", "v1", "t", "c").Name; got != "unknown" {
		t.Errorf("Name = %q, want %q", got, "unknown")
	}
}

func TestInfoString_RendersTheDocumentedReport(t *testing.T) {
	info := buildinfo.Info{
		Name:      "port-scan",
		Version:   "v2.2.0",
		Commit:    "5b2fb97dcafe",
		BuildTime: "2026-08-04T10:22:31Z",
		GoVersion: "go1.24.0",
		Platform:  "windows/amd64",
	}

	want := strings.Join([]string{
		"port-scan version v2.2.0",
		"commit:  5b2fb97dcafe",
		"built:   2026-08-04T10:22:31Z",
		"go:      go1.24.0 windows/amd64",
		"",
	}, "\n")

	if got := info.String(); got != want {
		t.Errorf("String() =\n%q\nwant\n%q", got, want)
	}
}

// `git describe --dirty` appends "-dirty" when the working tree had
// uncommitted changes. Such a build cannot be reproduced from a commit, so the
// report must say so out loud instead of leaving it to the reader to notice a
// suffix.
func TestInfoString_DirtyVersion_CarriesAnExplicitWarning(t *testing.T) {
	info := buildinfo.Info{
		Name:      "preprocess",
		Version:   "v2.2.0-3-gabc1234-dirty",
		Commit:    "abc1234",
		BuildTime: "2026-08-04T10:22:31Z",
		GoVersion: "go1.24.0",
		Platform:  "linux/amd64",
	}

	got := info.String()
	if !strings.Contains(got, "modified working tree") {
		t.Errorf("String() for a dirty build lacks the modified-tree warning:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("String() must end in a newline, got %q", got)
	}
}

func TestInfoIsDirty(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"v2.2.0", false},
		{"v2.2.0-dirty", true},
		{"v2.2.0-3-gabc1234-dirty", true},
		{"dev", false},
		{"", false},
		// "-dirty" must be a suffix, not a substring anywhere.
		{"v2.2.0-dirty-fix", false},
	}
	for _, tc := range tests {
		info := buildinfo.Info{Version: tc.version}
		if got := info.IsDirty(); got != tc.want {
			t.Errorf("Info{Version:%q}.IsDirty() = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestIsVersionRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"version subcommand", []string{"version"}, true},
		{"single dash flag", []string{"-version"}, true},
		{"double dash flag", []string{"--version"}, true},
		{"trailing args are ignored", []string{"version", "extra"}, true},
		{"no args", nil, false},
		{"empty first arg", []string{""}, false},
		{"other subcommand", []string{"scan"}, false},
		// Documented limitation: the token is only honoured in first position,
		// so a real flag named --version elsewhere is not shadowed.
		{"not first", []string{"-input", "a", "--version"}, false},
		{"case sensitive", []string{"Version"}, false},
	}
	for _, tc := range tests {
		if got := buildinfo.IsVersionRequest(tc.args); got != tc.want {
			t.Errorf("%s: IsVersionRequest(%q) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}
