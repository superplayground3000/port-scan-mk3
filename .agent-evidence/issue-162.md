# Issue 162 evidence

Restores the strict end-of-input check on the legacy snapshot array path.
The check existed at base commit `4febd75` and was lost when
`pkg/state/state.go` was rewritten in PR #152.

## Seam

The maintainer agreed one seam before any test was written: the exported
`state.LoadSnapshot`, driven by a real file in `t.TempDir()`.

`Load` and `LoadSnapshot` are one-line wrappers over `LoadSnapshotWithLimits`,
so there is one decode path and one public boundary. No test touches an
unexported helper.

Four cases run through that seam:

1. legacy array with trailing content, which must be rejected
2. legacy array that is valid, which must still load
3. object envelope with trailing content, which must be rejected
4. object envelope that is valid, which must still load

## RED proof

The red run used a throwaway `git worktree` at the fix commit, with **only** the
production hunk reverted. The test file stayed as committed, so the failure is
attributable to the 8 production lines and to nothing else.

```text
git worktree add /tmp/psmk3-162-redproof 6967f8c
git checkout master -- pkg/state/state.go
git diff HEAD --stat
 pkg/state/state.go | 8 --------
 1 file changed, 8 deletions(-)
```

```text
GOTOOLCHAIN=go1.24.4 go test ./pkg/state -run '^TestLoadSnapshot_When(LegacyArray|ObjectEnvelope)' -count=1 -v

=== RUN   TestLoadSnapshot_WhenLegacyArrayHasTrailingContent_ReturnsError
    snapshot_trailing_content_test.go:26: LoadSnapshot() error = nil, want an error for trailing content after the legacy array
--- FAIL: TestLoadSnapshot_WhenLegacyArrayHasTrailingContent_ReturnsError (0.00s)
--- PASS: TestLoadSnapshot_WhenLegacyArrayIsValid_LoadsChunks (0.00s)
--- PASS: TestLoadSnapshot_WhenObjectEnvelopeHasTrailingContent_ReturnsError (0.00s)
--- PASS: TestLoadSnapshot_WhenObjectEnvelopeIsValid_LoadsChunks (0.00s)
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/state	0.002s
```

Case 1 is red and cases 2, 3, and 4 are green. That shape is the answer to
acceptance criterion 3: **the object envelope path is already strict and needs no
change.** The object path calls `json.Unmarshal`, which requires the whole input
to be one JSON value. The legacy path used `json.Decoder`, which reads a stream
and stops after the first value, so it never saw the trailing bytes.

Cases 2 and 4 green before the change demonstrate, on those two inputs, that the
fix does not narrow what the base accepted. They are spot checks, not a proof.
The argument that nothing narrowed is mechanical: the added check uses the same
idiom as the base, so it rejects the same trailing-content set the base rejected,
and `bytes.TrimSpace` still runs first.

## Second red proof, added after review round 2

Round 2 found that only one branch of the new check was covered. Valid trailing
JSON makes the second decode return `nil`. Trailing content that is not JSON
makes it return a syntax error. The committed tests covered the first branch
only, so a fifth case now covers the second.

The same method proved it red. The worktree held the new test and the reverted
production hunk:

```text
git diff HEAD --stat
 pkg/state/snapshot_trailing_content_test.go | 12 ++++++++++++
 pkg/state/state.go                          |  8 --------
```

```text
=== RUN   TestLoadSnapshot_WhenLegacyArrayHasTrailingGarbage_ReturnsError
    snapshot_trailing_content_test.go:38: LoadSnapshot() error = nil, want an error for trailing garbage after the legacy array
--- FAIL: TestLoadSnapshot_WhenLegacyArrayHasTrailingGarbage_ReturnsError (0.00s)
```

All five cases pass after the change.

## Round-trip probe

A separate throwaway probe, not committed, answered two risks:

```text
SaveSnapshot wrote first byte '{', last byte '}'
Save() wrote first byte '{', last byte '}'
--- PASS: TestProbe_TrailingWhitespaceAndSaveRoundTrip
```

- A legacy array followed by newlines and tabs still loads, because
  `bytes.TrimSpace` runs before the decoder reads.
- `SaveSnapshot` and the legacy `Save` both write the object envelope. The writer
  never emits the array form, so the stricter array path cannot reject a file
  this program wrote.

## Quality gate

```text
GOTOOLCHAIN=go1.24.4 make verify

coverage gate passed: 85.6%

=== RESULT ===
All selected quality gates passed.
```

Exit status 0.

## Performance

G3 names large-input parsing as a hot path, and `LoadSnapshot` accepts snapshots
up to 1 GB. `BenchmarkLoadSnapshot/legacy_4000_chunks` already existed at
`pkg/state/snapshot_load_bench_test.go:42`, so it ran before and after.

```text
GOTOOLCHAIN=go1.24.4 go test ./pkg/state -run '^$' -bench '^BenchmarkLoadSnapshot$/legacy_4000_chunks' -benchmem -count=6
```

| Run | Before `17e054d` ns/op | After `6967f8c` ns/op |
| ---: | ---: | ---: |
| 1 | 4651836 | 4694899 |
| 2 | 4704652 | 4706732 |
| 3 | 4798792 | 4775400 |
| 4 | 4738300 | 4575708 |
| 5 | 4700905 | 4645994 |
| 6 | 4689337 | 4734615 |

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Median time | 4702779 ns/op | 4700816 ns/op | -0.04% |
| Allocated bytes | 4686288 B/op | 4686363 B/op | under 0.01% |
| Allocations | 20047 allocs/op | 20047 allocs/op | 0 |

The second `Decode` call runs one time for each load, not one time for each
chunk, and it reads no more input. The result is inside measurement noise and far
below the 10% block threshold.

## Validation triggers

- **Unit:** covered above.
- **e2e:** not triggered. The change touches the snapshot load path only. It does
  not touch the scan pipeline, the writers, or pressure control.
- **Performance:** triggered and measured above.

## Independent review

**Round 1, cross-provider, Codex**, on commit `6967f8c` in a separate worktree.

Verdict: **BLOCK**, on two process findings and **zero** correctness
findings. Codex confirmed the idiom matches the base, that `[] []` and
`[] garbage` are rejected while trailing whitespace is accepted, that no fixture
in the repository relies on the loose behavior, that the tests sit at the correct
exported seam, and that `make verify-e2e` is not triggered.

Both findings are answered by this file:

1. **No recorded red proof.** The red run existed but lived only in the
   implementing session, so a reviewer reading the commit could not see it. It is
   recorded above. This is the defect the finding names, and recording it is the
   fix.
2. **No before and after benchmark.** Correct, and the benchmark already existed,
   so there was no reason to skip it. Measured above.

Codex could not run `make verify` itself. Its sandbox mounts
`/home/hp/.cache/go-build` and `/tmp` read-only, so `go vet` and the focused tests
could not start. That is an environment limit, not a code defect, and Codex
labelled it as one. The gate result above comes from a normal checkout.

**Round 2 did not use Codex.** The provider hit a usage limit before round 2 ran,
with credits returning 2026-08-20. Rule G2 ranks reviewers as different provider,
then different Claude model, then any fresh-context agent. Round 2 therefore used
the second rank: a different Claude model, with no knowledge of the implementing
conversation.

**State this plainly when citing this file: the change has one full cross-provider
round and one same-provider, different-model round, not two cross-provider
rounds.**

Round 2 verdict: **APPROVE**. It reproduced every claim rather than accepting it,
including its own throwaway worktree for the red proof and its own six benchmark
runs. Its independent runs agreed: identical red and green shape, `20047`
allocations on every run, and `make verify` exit 0 at 85.6% coverage. It also
confirmed by experiment that `json.Decoder` silently reads a trailing value while
`json.Unmarshal` rejects one, and it searched the repository for fixtures relying
on the loose behavior and found none. The only two JSON files that reach the
loader, `labs/.../resume-mismatch.json` and
`docs/speed-up-scan-prepare/bucket.json`, are both object envelopes with a
trailing newline that `bytes.TrimSpace` removes.

Round 2 raised two non-blocking points, and both are fixed in this file and in
the tests:

1. One sentence overclaimed, saying two green spot checks "prove" nothing
   narrowed. Reworded, and the mechanical argument now carries that claim.
2. Only one branch of the new check was covered. A fifth test now covers the
   other, red-proved above.

Round 2 could not verify the seam agreement, which is a conversation fact rather
than a repository fact, and it did not re-run the round-trip probe, verifying the
mechanism by reading `state.go` instead.

## Deliberate non-change

The new error is `errors.New("unexpected trailing JSON content")` without the
file path, while neighbouring errors in the same function wrap the path. That
matches the base at `4febd75` and matches the other bare JSON decode errors in
this function. Issue #162 says not to invent a new rule, so the wording stays.
Adding path context to every JSON decode error in `LoadSnapshotWithLimits` is a
separate diagnostic improvement.
