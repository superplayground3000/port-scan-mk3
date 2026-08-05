// Package buildinfo renders the build-metadata report that every port-scan-mk3
// command prints for `<command> version`, `--version` or `-version`.
//
// The raw values are stamped into each binary at link time by the Makefile
// (`-X main.version`, `-X main.buildTime`, `-X main.commit`). The linker can
// only write to a variable in the binary's own `main` package, so the stamped
// variables live there; everything that can be got wrong — fallbacks for an
// unstamped build, the dirty-tree warning, the exact output layout — lives
// here, in one place, under unit test (constitution I, library-first).
//
// Policy encoded by this package:
//
//   - Version is `git describe --always --dirty`, so it is the nearest tag plus
//     the distance and abbreviated commit, or the bare commit when no tag is
//     reachable. A "-dirty" suffix means the working tree had uncommitted
//     changes; such a build is not reproducible from a commit and must not be
//     published, so String reports it explicitly.
//   - Commit is the full `git rev-parse HEAD` of the build, stamped separately
//     because a build made exactly on a tag describes as just "v2.2.0" and
//     would otherwise carry no commit at all.
//   - BuildTime is the COMMIT timestamp normalized to UTC, not the wall clock,
//     so two builds of the same commit are byte-identical (issue #65/#73).
//   - A binary built without ldflags (plain `go build`, or `go test`) reports
//     the placeholders below rather than empty fields.
package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

// Placeholders used when a value was not stamped at link time.
const (
	// UnstampedVersion is the version reported by a binary built without
	// `-X main.version`.
	UnstampedVersion = "dev"
	// UnstampedValue is the placeholder for any other missing stamp.
	UnstampedValue = "unknown"
)

// dirtySuffix is what `git describe --dirty` appends for a modified tree.
const dirtySuffix = "-dirty"

// Info is the resolved build metadata of a single binary.
type Info struct {
	Name      string // command name, e.g. "port-scan"
	Version   string // `git describe --always --dirty`, or "dev"
	Commit    string // full commit SHA, or "unknown"
	BuildTime string // commit timestamp in UTC RFC3339, or "unknown"
	GoVersion string // toolchain that built the binary
	Platform  string // GOOS/GOARCH the binary was built for
}

// Resolve turns the raw link-time stamps into a reportable Info, substituting
// the documented placeholders for values the linker never wrote. GoVersion and
// Platform are taken from the running binary, so they describe the artifact
// itself and cannot drift from it.
func Resolve(name, version, buildTime, commit string) Info {
	return Info{
		Name:      orDefault(name, UnstampedValue),
		Version:   orDefault(version, UnstampedVersion),
		BuildTime: orDefault(buildTime, UnstampedValue),
		Commit:    orDefault(commit, UnstampedValue),
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// IsDirty reports whether the binary was built from a modified working tree,
// i.e. whether `git describe --dirty` appended its suffix.
func (i Info) IsDirty() bool {
	return strings.HasSuffix(i.Version, dirtySuffix)
}

// String renders the version report, newline-terminated and ready to write to
// stdout. The first line is `<name> version <version>`; the remaining lines are
// `<label>:` fields. A dirty build gets an extra warning line.
func (i Info) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s version %s\n", i.Name, i.Version)
	fmt.Fprintf(&b, "commit:  %s\n", i.Commit)
	fmt.Fprintf(&b, "built:   %s\n", i.BuildTime)
	fmt.Fprintf(&b, "go:      %s %s\n", i.GoVersion, i.Platform)
	if i.IsDirty() {
		b.WriteString("warning: built from a modified working tree; " +
			"this artifact cannot be reproduced from a commit and is not a published release\n")
	}
	return b.String()
}

// IsVersionRequest reports whether args asks for the version report. The token
// is honoured only in FIRST position — deliberately, so that it is one shared
// rule across five commands whose flag parsing differs, and so it can never
// shadow a future flag of the same name in a later position.
//
// Accepted spellings: "version", "-version", "--version".
func IsVersionRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "-version", "--version":
		return true
	default:
		return false
	}
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
