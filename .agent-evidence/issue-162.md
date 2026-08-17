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

Cases 2 and 4 green before the change prove the fix does not narrow what the base
accepted.

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

Cross-provider review by Codex, on commit `6967f8c` in a separate worktree.

First verdict: **BLOCK**, on two process findings and **zero** correctness
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

## Deliberate non-change

The new error is `errors.New("unexpected trailing JSON content")` without the
file path, while neighbouring errors in the same function wrap the path. That
matches the base at `4febd75` and matches the other bare JSON decode errors in
this function. Issue #162 says not to invent a new rule, so the wording stays.
Adding path context to every JSON decode error in `LoadSnapshotWithLimits` is a
separate diagnostic improvement.
