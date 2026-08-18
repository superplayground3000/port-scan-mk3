# Issue 158 evidence

Makes `port-scan scan -progress-interval N` work. Before this change the scan
parser read the flag and threw the value away, so the cadence was always the
built-in 100.

## The mechanism

The break spanned two places, and each one alone was enough to lose the value.

1. `pkg/config/scan_config.go:222` called `fs.Int(...)`. That form returns a
   `*int` and the caller dropped it. `ScanValues` had no `ProgressInterval`
   field, so the parsed value had nowhere to go.
2. `pkg/scanapp/scan.go:169` read `progressInterval: opts.ProgressInterval`,
   which comes from `RunOptions`, not from the resolved configuration.
   `cmd/port-scan/command_handlers.go:117` passes `scanapp.RunOptions{}`, so
   the CLI always supplied `0`. `pkg/scanapp/scan_runtime.go:167` turns a value
   that is not positive into 100.

The result: the flag parsed, validated, and appeared in `-h`, and every scan ran
at cadence 100.

The two sibling workflows already do it the other way, and they are the pattern
this change copies:

- `pkg/config/pre_ping.go:83` binds with `fs.IntVar(&values.ProgressInterval, ...)`,
  and `pkg/scanapp/pre_ping.go:72` reads `values.ProgressInterval`.
- `pkg/config/generate_buckets.go:91` and `pkg/scanapp/bucketgen.go:122` do the
  same.

## The design

**The resolved configuration is the single source of truth.**
`RunOptions.ProgressInterval` is deleted rather than kept as a second input.

`RunOptions`'s own doc comment says it "customizes runtime behaviors that the
CLI does not expose as flags". Once the flag works, a `ProgressInterval` field
there contradicts that contract and gives the cadence two sources. Deleting it
makes the wrong thing impossible to write.

`cmd/port-scan/command_handlers.go` needed no change. `runScan` already hands
`cfg` to `scanapp.Run`, so the CLI path completed itself once `Run` read the
configuration.

## Validation rule for a value that is not positive

`NewScan` does **not** reject it. Neither `pre-ping` nor `generate-buckets`
rejects it: `pkg/progress/progress.go:54` falls back to 100, and
`pkg/scanapp/scan_runtime.go:167` does the same for scan. Consistency across the
three workflows is the point, so this change adds no validation error. A test
pins that decision, and a probe below shows the test catches an attempt to add
one.

## Seam

Agreed with the maintainer before any test was written:

1. **`config.ParseScan`** — a parsed `-progress-interval N` lands in
   `ScanValues.ProgressInterval`.
2. **`scanapp.Run`** — a configuration carrying `ProgressInterval: 1` produces
   one progress event for each written result; the default 100 produces none for
   a handful of results.

## RED proof

### Seam 1, step A: the field did not exist

Adding `ProgressInterval` to the two `want` literals in the existing tests fails
to build, because `ScanValues` had no such field:

```text
GOTOOLCHAIN=go1.24.4 go test -run 'TestParseScanReturnsDefaults|TestParseScanReturnsAcceptedFlagsAndAuthenticatedPolicy' ./pkg/config/

# github.com/xuxiping/port-scan-mk3/pkg/config_test [github.com/xuxiping/port-scan-mk3/pkg/config.test]
pkg/config/scan_config_test.go:46:3: unknown field ProgressInterval in struct literal of type config.ScanValues
pkg/config/scan_config_test.go:312:3: unknown field ProgressInterval in struct literal of type config.ScanValues
FAIL	github.com/xuxiping/port-scan-mk3/pkg/config [build failed]
```

### Seam 1, step B: the value was dropped

The field was then added as an inert data carrier, with `fs.Int` still in place.
A struct field cannot be asserted on before it exists, so this two-step order is
what TDD on a new field looks like. The second red is a real assertion failure:

```text
--- FAIL: TestParseScanReturnsDefaults (0.00s)
    scan_config_test.go:55: Resolve() = config.ScanValues{..., Format:"human", Quiet:false, ProgressInterval:0, ...}, want config.ScanValues{..., Format:"human", Quiet:false, ProgressInterval:100, ...}
--- FAIL: TestParseScanReturnsAcceptedFlagsAndAuthenticatedPolicy (0.00s)
    scan_config_test.go:324: Resolve() = config.ScanValues{..., Format:"json", Quiet:true, ProgressInterval:0, ...}, want config.ScanValues{..., Format:"json", Quiet:true, ProgressInterval:17, ...}
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/config	0.003s
```

`ProgressInterval:0` against `100` and against the `-progress-interval 17` on
the command line is the defect stated exactly. Changing line 222 to `fs.IntVar`
turned both green.

### Seam 2: the runtime read the wrong source

`TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence` sets the cadence
on the configuration fixture and passes `RunOptions` without it:

```text
=== RUN   TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence
    scan_progress_interval_test.go:74: scan_progress events = 0, want 4 (one per written result)
--- FAIL: TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence (0.00s)
=== RUN   TestRun_WhenConfiguredProgressIntervalIsNotPositive_UsesDefaultCadence
--- PASS: TestRun_WhenConfiguredProgressIntervalIsNotPositive_UsesDefaultCadence (0.00s)
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/scanapp	0.033s
```

Changing `pkg/scanapp/scan.go:169` to `values.ProgressInterval` turned it green.

### One test was green from the start, and this file says so

`TestParseScanAcceptsNonPositiveProgressInterval` and
`TestRun_WhenConfiguredProgressIntervalIsNotPositive_UsesDefaultCadence` pin the
decision **not** to add validation. A pin on a non-change cannot be red, because
no production line accompanies it. Probe C below is what shows they are not
vacuous.

## Discrimination probes

All three ran in a throwaway `git worktree` under `/tmp`, never in the branch
tree, per the 2026-08-02 lesson about mutating a tree under review. The worktree
was created at base commit `4212577` and the changed files copied in.

### Probe A — revert the flag registration only

```text
git diff HEAD --stat -- pkg/config/scan_config.go
 pkg/config/scan_config.go | 5 ++++-
 1 file changed, 4 insertions(+), 1 deletion(-)
```

```text
--- FAIL: TestParseScanReturnsDefaults (0.00s)
    ... ProgressInterval:0, ... want ... ProgressInterval:100, ...
--- FAIL: TestParseScanReturnsAcceptedFlagsAndAuthenticatedPolicy (0.00s)
    ... ProgressInterval:0, ... want ... ProgressInterval:17, ...
--- FAIL: TestParseScanAcceptsNonPositiveProgressInterval (0.00s)
```

### Probe B — revert the runtime read only

The mutation put back `opts.ProgressInterval` and re-added the `RunOptions`
field so the package still builds. The result is stronger than a two-line diff:

```text
git diff HEAD --stat
 pkg/config/scan_config.go              |   7 +-
 pkg/config/scan_config_test.go         |  31 +++++++++
 pkg/scanapp/scan_configuration_test.go |   4 ++
 pkg/scanapp/scan_observability_test.go | 123 +++++++++++++++++----------------
 pkg/scanapp/scan_resume_output_test.go |   8 +--
 5 files changed, 106 insertions(+), 67 deletions(-)
```

`pkg/scanapp/scan.go` is **absent from that list**, which means the mutation
restored it byte for byte to the pre-fix version at `4212577`. Everything else,
including the whole config fix, stayed in place. The test still failed:

```text
=== RUN   TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence
    scan_progress_interval_test.go:74: scan_progress events = 0, want 4 (one per written result)
--- FAIL: TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence (0.00s)
```

So the failure attributes to those two lines of `scan.go` and to nothing else.

### Probe C — add the validation the design rejected

Inserting `if values.ProgressInterval <= 0 { return error }` into `NewScan`:

```text
=== RUN   TestParseScanAcceptsNonPositiveProgressInterval
    scan_config_test.go:344: ParseScan() with -progress-interval 0 error = validate scan arguments: -progress-interval must be > 0
--- FAIL: TestParseScanAcceptsNonPositiveProgressInterval (0.00s)
```

The pin test is therefore not vacuous. It defends the agreed contract against a
future change that would make `scan` reject what its two siblings accept.

The worktree was removed with `git worktree remove --force` and pruned.

## End-to-end proof through the real binary

Unit tests use a configuration fixture, so they do not prove the CLI path. This
run uses the built binary and loopback targets only, so constitution V holds.
Five closed ports on `127.0.0.1`, `-output-flush-results 1` so each result
flushes and the counter advances per result:

| Command | Progress lines on stdout |
| --- | --- |
| `-progress-interval 1` | 5 (`scanned=1/5` … `scanned=5/5`) |
| default cadence, flag omitted | 0 |

Before this change both rows would read the same, because both would run at 100.

Note one interaction worth knowing: the cadence counts **written** results, not
dispatched ones. `summary.written` advances only in `outputCommitter.commit`
(`pkg/scanapp/output_committer.go:118-137`), so `-output-flush-results` bounds
how often a progress line can appear at all. With its default of `1000`, a
five-target scan flushes once at the end, and `-progress-interval 1` therefore
emits one line rather than five.

An earlier version of this paragraph said that scan emits one line "whatever the
cadence". That is wrong: the emitter fires only when `written/step` crosses a
boundary (`result_aggregator.go:123`), so at the default cadence of 100 a
five-target scan emits **zero** lines. The e2e table above shows exactly that.
One line is what you get for a cadence at or below the written count.

`docs/cli/flags.md` now states this interaction, because a user who sets
`-progress-interval 1` and sees one line would otherwise reasonably think the
flag is still broken.

## Quality gates

```text
GOTOOLCHAIN=go1.24.4 make verify

coverage gate passed: 85.6%

=== RESULT ===
All selected quality gates passed.
```

Exit code 0, confirmed separately.

## Validation triggers

- **Unit:** covered above, run under `-race -shuffle=on` through `make verify`.
- **e2e:** **not triggered**. This change touches flag binding and one field
  read in `Run`. It does not touch the scan pipeline, the writers, or pressure
  control. `make verify-e2e` was not run.
- **Performance:** **not triggered**. This is configuration wiring. The value
  reaches `progressStep` once, before the scan loop starts; no hot path changed.

## No gate or assertion was weakened

A plain `git diff -U0 -- '*_test.go' | grep '^-'` removes 65 lines, which looks
alarming and is not the right measurement: `ProgressInterval` is longer than the
previous longest field name in those struct literals, so `gofmt` rewrote the
padding on every neighbouring line. Ignore whitespace and the real picture is
six removals:

```text
git diff master...HEAD -w --ignore-blank-lines -U0 -- '*_test.go' \
  ':!pkg/scanapp/scan_progress_interval_test.go' | grep '^-' | grep -v '^---'

-	if err := Run(..., RunOptions{DisableKeyboard: true, ProgressInterval: 1}); err != nil {
-		ProgressInterval: 1,
-		ProgressInterval: 1,
-		ProgressInterval:          1,
-		ProgressInterval:          1,
-		ProgressInterval: 1, // tick every chunk/result so progress lines always emit
```

Every one is a `RunOptions.ProgressInterval` use, and each is replaced by the
same value set on the configuration fixture. No `t.Fatalf`, no condition, and no
test was removed or weakened.

One line that looks deleted is not: `cfg.LogLevel = "info" // bucket_parse_* …`
in `scan_resume_output_test.go:26` survives with only its comment padding
changed.

An earlier version of this section claimed the plain grep returned "exactly one
removed line". That was wrong by a factor of 65. The conclusion it supported
still holds, but the command cited did not support it.

## Documentation

Two statements were made false by this change and are corrected:

- `docs/apps/port-scan/SPEC.md:140` said "The scan parser accepts this
  compatibility flag but does not use its value."
- `docs/apps/port-scan/DESIGN.md:112` said "The parsed value does not cross the
  scan configuration seam and does not change behavior." It now records that the
  resolved configuration is the only source of the cadence.

`docs/cli/flags.md:124` said only "Progress line cadence." It now names the unit
and the stream, matching the `pre-ping` and `generate-buckets` rows at lines 56
and 84. It states stdout for the human line and stderr for the structured event,
which is what `emitCommittedProgress` does at
`pkg/scanapp/result_aggregator.go:131` (the stdout line) and `:136` (the
structured event).

An earlier version cited `result_aggregator.go:108` and `:113` instead. Those
lines are inside `emitScanResultEvents`, which **production never calls** — its
only caller is `scan_observability_test.go:631`. The statement about the two
streams was true, but the lines cited did not support it. See the note below.

`README.md` needed no correction. Lines 420 and 559 already described
`-progress-interval` as a working cadence flag for all three pipeline steps; the
code did not deliver it.

`docs/plans/**` and `docs/release-notes/**` were left alone. They record what was
true when written.

## Survey: other dropped flag return values in `pkg/config`

The brief asked to look for the same defect class and to fix nothing. Seven
registrations discard their return value, all in one block:

- `pkg/config/validate_config.go:122` `fs.String("output", ...)`
- `pkg/config/validate_config.go:123` `fs.Duration("timeout", ...)`
- `pkg/config/validate_config.go:124` `fs.Duration("delay", ...)`
- `pkg/config/validate_config.go:125` `fs.String("pressure-api", ...)`
- `pkg/config/validate_config.go:126` `fs.Bool("disable-api", ...)`
- `pkg/config/validate_config.go:127` `fs.Bool("disable-pre-scan-ping", ...)`
- `pkg/config/validate_config.go:128` `fs.String("resume", ...)`

These are a **different case** from #158, not more instances of it. The block
carries an explicit comment at `validate_config.go:120`: "Validate keeps these
flags for CLI compatibility. The workflow does not use their values." The value
is dropped on purpose and the intent is stated at the site. #158 was a silent
drop whose documentation claimed the same thing but whose flag users reasonably
expected to work.

No other `fs.Int`/`fs.String`/`fs.Bool`/`fs.Duration`/`fs.Var` call in `pkg/`,
`cmd/`, or `internal/` discards its return. Nothing was changed here.

## What is NOT proven

- **Windows is unobserved.** The change is pure Go with no platform code, and
  the native Windows gate runs the same unit tests, but nobody ran the flag on a
  Windows console.
- **No e2e or benchmark evidence exists**, by the trigger analysis above. If a
  reviewer disagrees that the triggers are absent, that is the thing to
  challenge, not a missing run.
- **The progress line format is unchanged and untested here.** It was out of
  scope.
- **No external importer of `pkg/scanapp` can be checked from here.** Deleting
  `RunOptions.ProgressInterval` breaks any caller that set it. See the
  release-notes section below.

## Found during review, deliberately not fixed here

`emitScanResultEvents` (`pkg/scanapp/result_aggregator.go:94`) has **no
production caller**. The only call is `scan_observability_test.go:631`, and the
comment at `scan_runtime.go:174` claims the function "keeps the result
progress", which is false. The live emitter is `emitCommittedProgress`.

This is dead production code carrying test coverage, and it is what made the
first version of this file cite the wrong lines. It is out of scope for #158 and
is filed separately rather than fixed here.

## Release notes and the version bump: a maintainer decision, still open

Deleting the public field `scanapp.RunOptions.ProgressInterval` is a breaking Go
API change. Any external caller that sets it stops compiling.

The repo has a convention for exactly this. `docs/release-notes/4.0.0.md:64` has
a "Breaking Go API changes" section that records the same class of removal
(`RunOptions.ResumeStatePath`, `RunOptions.PressureFetcher`,
`RunOptions.PressureHTTP`). Constitution VII then requires a MAJOR bump.

There is no unreleased notes file: `4.0.0.md` is the newest and `v4.0.0` is
tagged at `4febd75`. Before that tag, PRs edited the upcoming notes file as they
merged; after it, PRs (#167, #170) have not created a successor. So current
practice defers the notes to release preparation.

**This change does not create a release-notes file.** Choosing the next version
is the maintainer's call, not the implementer's, and nothing here cuts a
release. What must not happen is the break going unrecorded when the next
version is prepared.

## Independent review

Cross-provider review was unavailable: Codex is still rate limited, with credits
returning 2026-08-20 11:35 (verified by a live probe, not assumed). Rule G2 ranks
reviewers as different provider, then different Claude model, then any
fresh-context agent, so this used the second rank: a different Claude model with
no knowledge of the implementing conversation.

**State this plainly when citing this file: the change has one same-provider,
different-model review round. It has no cross-provider round.**

First verdict: **BLOCK**, with six findings and no defect in the production code
itself. The reviewer re-ran `make verify` and re-derived both red proofs and the
discrimination probe in its own throwaway worktree rather than trusting the
paste above.

What it found, and what changed as a result:

1. `docs/apps/port-scan/DESIGN.md:134` still listed `ProgressInterval` as a
   `RunOptions` seam that adjusts runtime "without adding CLI flags" — a direct
   self-contradiction 22 lines below a paragraph this change had edited. Fixed.
2. `docs/specs/SPEC-06-SCAN-ORCHESTRATION.md:51` reproduced the `RunOptions`
   struct including the deleted field. Fixed.
3. The committed version of this file carried the false "exactly one removed
   line" claim while the correction sat uncommitted. Now committed together.
4. The breaking-change record was missing. Documented above as an open
   maintainer decision.
5. `TestParseScanAcceptsNonPositiveProgressInterval` derived its expected value
   with `strconv.Atoi(raw)`. The reviewer judged it not tautological in the
   damning sense, but a literal table is strictly simpler and complies with the
   TDD skill to the letter. Rewritten as a `{raw, want}` table; the `strconv`
   import is gone.
6. The `-progress-interval` row in `docs/cli/flags.md` did not mention that
   `-output-flush-results` bounds the cadence. Added.

It also caught two more overclaims in this file, both corrected in place above:
the "whatever the cadence" sentence, and the `result_aggregator.go:108/:113`
citation into a function production never calls.
