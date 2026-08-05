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

// jobBlock returns the body of one job from the workflow: everything from that
// job's key until the next key at the same indentation. Assertions that must
// hold for a SPECIFIC job scope themselves through this, so a setting satisfying
// them from a different job cannot be mistaken for the real thing.
func jobBlock(t *testing.T, wf, job string) string {
	t.Helper()
	const indent = "  " // jobs are nested one level under `jobs:`
	start := strings.Index(wf, "\n"+indent+job+":")
	if start < 0 {
		t.Fatalf("release.yml has no %q job; this test pins that job's behavior and "+
			"needs updating alongside a rename", job)
	}
	body := wf[start+1:]
	for offset := 0; ; {
		next := strings.Index(body[offset:], "\n"+indent)
		if next < 0 {
			return body
		}
		at := offset + next + 1
		rest := body[at+len(indent):]
		// A sibling job starts with a non-space, non-comment character at this
		// indentation; deeper-indented lines and comments belong to this job.
		if len(rest) > 0 && rest[0] != ' ' && rest[0] != '#' {
			return body[:at]
		}
		offset = at
	}
}

// codeLines drops comment and blank lines, so an assertion about what a job
// DOES cannot be satisfied by a comment that merely talks about it.
func codeLines(block string) []string {
	var out []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
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
	// previous tag and the version assertion refused to build.
	//
	// The invariant is narrower than "the file mentions tags somewhere": the
	// PACKAGE job must have the tags in hand BEFORE it runs `git describe`. A
	// fetch in another job, or one placed after the describe, would leave the
	// bug in place, so scope the search to that job and to the text preceding
	// the describe.
	// Comments in this job discuss `git describe` and tag fetching by name, so
	// every lookup here ignores comment lines. Matching them would let the very
	// prose that explains the bug stand in for the fix.
	pkg := codeLines(jobBlock(t, wf, "package"))
	describeAt := -1
	for i, line := range pkg {
		if strings.Contains(line, "git describe") {
			describeAt = i
			break
		}
	}
	if describeAt < 0 {
		t.Fatalf("the package job no longer runs `git describe`; this test pins where " +
			"its tags must come from and needs updating alongside that change")
	}
	beforeDescribe := strings.Join(pkg[:describeAt], "\n")
	fetchesTags := strings.Contains(beforeDescribe, "fetch-tags: true") ||
		strings.Contains(beforeDescribe, "git fetch --force --tags")
	if !fetchesTags {
		t.Errorf("the package job reaches `git describe` without fetching tags first: " +
			"fetch-depth: 0 alone leaves actions/checkout@v4 running with --no-tags, so " +
			"describe reports the PREVIOUS tag and the release is refused. Set " +
			"fetch-tags: true on the package job's checkout, or run " +
			"`git fetch --force --tags` ahead of the describe in that same job")
	}
	if !strings.Contains(wf, "scripts/package_release.sh") {
		t.Errorf("release.yml does not build its assets with scripts/package_release.sh")
	}
	// Windows ARM64 is deliberately out of scope (issue #65/#70).
	if strings.Contains(wf, "arm64") {
		t.Errorf("release.yml references arm64, which is out of scope for this release path")
	}
}
