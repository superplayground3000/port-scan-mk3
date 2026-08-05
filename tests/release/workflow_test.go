package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The release workflow cannot be proven by running it here — publishing a real
// tag is a user action. What CAN be pinned locally are the invariants that make
// the tag path safe, and those are exactly the ones a careless edit would drop:
// that a manual dispatch never publishes, that publication waits on the Windows
// smoke test, and that the checkout fetches tags (without them `git describe`
// silently reports a bare commit and every artifact would be mislabelled).
//
// These are string assertions rather than YAML-model assertions on purpose: the
// repository takes no third-party Go dependencies for test scaffolding
// (constitution "Technology Stack"), and the workflow's own syntax is validated
// by GitHub on push.
func readWorkflow(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestReleaseWorkflow_PublishesOnlyForAPushedTag(t *testing.T) {
	wf := readWorkflow(t)

	// A workflow_dispatch run can be started ON a tag ref, so gating publication
	// on the ref alone would let a manual run publish. The event type must be
	// part of the condition.
	const gate = "github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')"
	if !strings.Contains(wf, gate) {
		t.Errorf("release.yml does not gate publication on %q; a manual dispatch could publish", gate)
	}
	if !strings.Contains(wf, "workflow_dispatch:") {
		t.Errorf("release.yml has no workflow_dispatch trigger, so the build/package/smoke " +
			"path cannot be exercised without publishing a release")
	}
	if !strings.Contains(wf, "tags:") {
		t.Errorf("release.yml does not trigger on tag pushes")
	}
}

func TestReleaseWorkflow_SmokeTestsOnWindowsBeforePublishing(t *testing.T) {
	wf := readWorkflow(t)

	if !strings.Contains(wf, "windows-latest") {
		t.Errorf("release.yml never uses a windows runner, so the Windows EXEs it " +
			"publishes are never executed (a Linux runner cannot run them)")
	}
	if !strings.Contains(wf, "scripts/smoke_release.sh") {
		t.Errorf("release.yml does not run the artifact smoke test")
	}
	if !strings.Contains(wf, "needs: [package, smoke-windows]") {
		t.Errorf("the publish job does not depend on both packaging and the Windows " +
			"smoke test, so an unverified artifact could be published")
	}
}

// A tag name reaches this workflow as data from outside. `${{ }}` inside a
// `run:` body is spliced into the script TEXT before any shell parses it, so a
// tag containing a quote, semicolon or backtick executes on the runner — and
// the publish job holds `contents: write`. Values must arrive through `env:`,
// where they are only ever data.
func TestReleaseWorkflow_NeverInterpolatesRefDataIntoRunBodies(t *testing.T) {
	wf := readWorkflow(t)

	// A `run:` block owns every following line indented deeper than the `run:`
	// key itself; the first line at or below that indentation ends it.
	runIndent := -1
	for i, line := range strings.Split(wf, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		inBody := false
		if strings.HasPrefix(trimmed, "run:") {
			runIndent = indent
			inBody = true // a one-line `run: cmd` interpolates on this very line
		} else if runIndent >= 0 && trimmed != "" {
			if indent > runIndent {
				inBody = true
			} else {
				runIndent = -1
			}
		}

		// Comments are not executed; the explanatory note above the fix quotes
		// the very syntax being banned.
		if !inBody || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "${{") {
			t.Errorf("release.yml:%d interpolates %s into a run body; pass it through "+
				"env: instead, or a crafted tag name executes on the runner:\n  %s",
				i+1, "${{ ... }}", trimmed)
		}
	}
}

func TestReleaseWorkflow_FetchesTagsAndBuildsFromSource(t *testing.T) {
	wf := readWorkflow(t)

	if !strings.Contains(wf, "fetch-depth: 0") {
		t.Errorf("release.yml checks out without fetch-depth: 0; `git describe` would " +
			"not see the tag and every artifact would be stamped with a bare commit")
	}
	// fetch-depth: 0 is NOT enough: actions/checkout@v4 passes --no-tags unless
	// fetch-tags is set, so a full-history checkout can still have no tag refs.
	// That is what failed the v2.2.0 release -- `git describe` resolved to the
	// previous tag and the version assertion refused to build. The workflow must
	// either ask checkout for the tags or fetch them itself before describing.
	fetchesTags := strings.Contains(wf, "fetch-tags: true") ||
		strings.Contains(wf, "git fetch --force --tags")
	if !fetchesTags {
		t.Errorf("release.yml never fetches tags: fetch-depth: 0 alone leaves " +
			"actions/checkout@v4 running with --no-tags, so `git describe` reports the " +
			"PREVIOUS tag and the release is refused. Set fetch-tags: true on the " +
			"checkout, or run `git fetch --force --tags` before `git describe`")
	}
	if !strings.Contains(wf, "scripts/package_release.sh") {
		t.Errorf("release.yml does not build its assets with scripts/package_release.sh")
	}
	// Windows ARM64 is deliberately out of scope (issue #65/#70).
	if strings.Contains(wf, "arm64") {
		t.Errorf("release.yml references arm64, which is out of scope for this release path")
	}
}
