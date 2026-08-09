# Issue 79 Pressure Poll Test Determinism Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace fixed-sleep synchronization in all `TestPollPressureAPI_*` tests with observable events.

**Architecture:** Add one test-only HTTP server and one poller lifecycle helper. Each test controls responses and waits for controller, log, telemetry, or error events.

**Tech Stack:** Go 1.24.x, `net/http/httptest`, channels, `context`, `testing`, and `internal/testkit.WaitFor`.

---

## File Structure

- Create `pkg/scanapp/pressure_test_helpers_test.go` for scripted HTTP responses and poller lifecycle control.
- Modify `pkg/scanapp/pressure_monitor_test.go` for pressure values, transitions, retries, and manual pause interaction.
- Modify `pkg/scanapp/scan_observability_test.go` for JSON log and telemetry events.
- Modify `pkg/scanapp/scan_test.go` for pause and resume log transitions.
- Modify `pkg/scanapp/scan_helpers_test.go` for retry recovery.
- Create one GitHub issue for Native Windows validation.

Do not modify `pkg/scanapp/pressure_monitor.go` in the final change.

## Required Skills and Rules

- Use `@superpowers:test-driven-development` for every implementation task.
- Use `@simple-english` for new test comments, the tracking issue, and issue comments.
- Use `@superpowers:verification-before-completion` before completion claims.
- Use `@superpowers:requesting-code-review` for each review stage.
- Obey `.claude/rules/60-development-guidelines.md` and `.claude/rules/constitution.md`.

### Task 1: Add the test harness and repair the named regression

**Files:**

- Create: `pkg/scanapp/pressure_test_helpers_test.go`
- Modify: `pkg/scanapp/pressure_monitor_test.go:155-177`
- Test: `pkg/scanapp/pressure_monitor_test.go`

- [ ] **Step 1: Run the current named test**

Run:

```text
go test -race -count=1 ./pkg/scanapp -run '^TestPollPressureAPI_PressureAboveThreshold_Pauses$' -v
```

Expected: PASS on Linux. Record that the normal environment does not reproduce the Windows delay.

- [ ] **Step 2: Add a temporary slow response to prove red capability**

Add this temporary statement at the start of the named test HTTP handler:

```go
time.Sleep(100 * time.Millisecond)
```

Do not commit this statement.

- [ ] **Step 3: Run the named test and record the red result**

Run:

```text
go test -race -count=1 ./pkg/scanapp -run '^TestPollPressureAPI_PressureAboveThreshold_Pauses$' -v
```

Expected: FAIL with `expected controller to be paused when pressure=91 and threshold=90`.

- [ ] **Step 4: Replace the temporary delay with a scripted server**

Create these test-only types in `pressure_test_helpers_test.go`:

```go
const pressureTestTimeout = 5 * time.Second

type scriptedPressureHTTPResponse struct {
	statusCode int
	body       string
}

type scriptedPressureServer struct {
	server    *httptest.Server
	requests  chan struct{}
	responses chan scriptedPressureHTTPResponse
}
```

Implement `newScriptedPressureServer`. Its handler must use two context-aware selects:

```go
select {
case requests <- struct{}{}:
case <-r.Context().Done():
	return
}

select {
case response := <-responses:
	w.WriteHeader(response.statusCode)
	_, _ = io.WriteString(w, response.body)
case <-r.Context().Done():
	return
}
```

Add a `respond` method. It must wait for a request before it sends one response.

Each wait must use `pressureTestTimeout` only as a failure bound.

Register `server.Close` with `t.Cleanup` inside `newScriptedPressureServer`.

Do not use `defer server.Close()` with this helper.

- [ ] **Step 5: Add the poller lifecycle helper**

Add this test-only type:

```go
type testPressurePoller struct {
	cancel context.CancelFunc
	done   chan struct{}
	errCh  chan error
	once   sync.Once
}
```

Implement `startTestPressurePoller`. It must start `pollPressureAPI` and close `done` after the function returns.

Register an idempotent cleanup operation with `t.Cleanup`.

Each test must create the scripted server before it starts the poller. Cleanup uses LIFO order, so the poller stops before the server closes.

Implement `stop`. It must cancel the context and wait for `done` with `pressureTestTimeout`.

Add `makeSureNoError`. It must read `errCh` only after the poller stops.

- [ ] **Step 6: Repair the named test**

Use the scripted server and poller helper. Release `{"pressure":91}` after the first request.

Wait for the state with this assertion:

```go
testkit.WaitFor(t, pressureTestTimeout,
	"controller to pause when pressure=91 and threshold=90",
	ctrl.IsPaused)
```

Stop the poller. Then make sure that `errCh` contains no error.

- [ ] **Step 7: Run the repaired named test**

Run:

```text
go test -race -shuffle=on -count=20 ./pkg/scanapp -run '^TestPollPressureAPI_PressureAboveThreshold_Pauses$'
```

Expected: PASS.

- [ ] **Step 8: Make sure that the temporary delay is absent**

Run:

```text
git diff --check
git diff -- pkg/scanapp/pressure_monitor_test.go pkg/scanapp/pressure_test_helpers_test.go
```

Expected: The diff contains no temporary `time.Sleep(100 * time.Millisecond)` statement.

- [ ] **Step 9: Commit Task 1**

```text
git add pkg/scanapp/pressure_test_helpers_test.go pkg/scanapp/pressure_monitor_test.go
git commit -m "test(scanapp): add deterministic pressure poll harness (#79)"
```

### Task 2: Repair the pressure monitor test group

**Files:**

- Modify: `pkg/scanapp/pressure_monitor_test.go:23-442`
- Reuse: `pkg/scanapp/pressure_test_helpers_test.go`
- Test: `pkg/scanapp/pressure_monitor_test.go`

- [ ] **Step 1: Convert the fatal failure test**

Release three HTTP 500 responses. Wait for the fatal result from `errCh`.

Make sure that the error contains `pressure api failed 3 times`. Then wait for poller completion.

- [ ] **Step 2: Convert the recovery test**

Set API pause to true before the first request. Release two HTTP 500 responses and one pressure 50 response.

Wait until `ctrl.APIPaused()` returns false. Stop the poller and make sure that no fatal error exists.

- [ ] **Step 3: Convert the threshold tests**

Use one controlled response for each test:

- Pressure 90 must change the controller to paused.
- Pressure 89 must change a pre-paused controller to active.
- Pressure 91 uses the repaired Task 1 test.

If an existing name conflicts with the stronger transition assertion, rename the test.

- [ ] **Step 4: Convert the pause and resume test**

Release pressure 95 and wait for pause. Then release pressure 30 and wait for resume.

- [ ] **Step 5: Convert the oscillation test**

Release pressures `95`, `30`, `95`, and `30` in order. Wait for the expected state after each response.

Remove the periodic transition counter and its sampling loop.

- [ ] **Step 6: Convert the low-pressure tests**

Set API pause to true before each test. Release these values separately:

- `0`
- `-1`
- `89.94`

Wait until API pause becomes false after each response.

- [ ] **Step 7: Normalize the fractional pause test lifecycle**

Replace its direct HTTP server with the scripted server. Keep its `testkit.WaitFor` state assertion.

Replace its direct goroutine and cancel calls with the poller helper.

- [ ] **Step 8: Convert the manual and API pause test**

Set manual pause before the first response. Release pressure 95 and wait until API pause becomes true.

Stop the poller before direct controller state changes. Remove all lifecycle sleeps.

Clear API pause and make sure that manual pause still blocks. Clear manual pause and make sure that the controller becomes active.

- [ ] **Step 9: Run the pressure monitor tests**

Run:

```text
go test -race -shuffle=on -count=20 ./pkg/scanapp -run '^TestPollPressureAPI_'
```

Expected: PASS.

- [ ] **Step 10: Review remaining sleeps in the file**

Run:

```text
rg -n 'time\.Sleep' pkg/scanapp/pressure_monitor_test.go
```

Expected: No result.

- [ ] **Step 11: Commit Task 2**

```text
git add pkg/scanapp/pressure_monitor_test.go pkg/scanapp/pressure_test_helpers_test.go
git commit -m "test(scanapp): wait for pressure state events (#79)"
```

### Task 3: Repair the remaining pressure poll tests

**Files:**

- Modify: `pkg/scanapp/scan_observability_test.go:546-649`
- Modify: `pkg/scanapp/scan_test.go:854-896`
- Modify: `pkg/scanapp/scan_helpers_test.go:762-809`
- Reuse: `pkg/scanapp/pressure_test_helpers_test.go`

- [ ] **Step 1: Convert the text log transition test**

In `scan_test.go`, release pressure 95 and wait for the pause state and pause log.

Then release pressure 20 and wait for the active state and resume log. Stop the poller before final assertions.

- [ ] **Step 2: Convert the retry helper test**

In `scan_helpers_test.go`, set API pause to true. Release HTTP statuses 500, 500, and 200.

Use pressure 10 in the successful response. Wait for active state, stop the poller, and make sure that no fatal error exists.

Make sure that the log contains `(1/3)` and `(2/3)`.

- [ ] **Step 3: Convert the JSON log transition test**

In `scan_observability_test.go`, release pressure 95 and wait for the JSON pause message.

Then release pressure 20 and wait for the JSON resume message. Stop the poller before final assertions.

- [ ] **Step 4: Convert the telemetry recorder test**

Keep the existing `scriptedPressureFetcher`. Start it through the poller helper.

Use `testkit.WaitFor` with a mutex-protected condition. Wait for two failures and one sample.

Stop the poller before the final recorder assertions. Remove its deadline loop and lifecycle sleep.

- [ ] **Step 5: Run every pressure poll test**

Run:

```text
go test -race -shuffle=on -count=50 ./pkg/scanapp -run '^TestPollPressureAPI_'
```

Expected: PASS.

- [ ] **Step 6: Review fixed sleeps in the modified regions**

Run:

```text
rg -n 'time\.Sleep' \
  pkg/scanapp/pressure_monitor_test.go \
  pkg/scanapp/scan_observability_test.go \
  pkg/scanapp/scan_test.go \
  pkg/scanapp/scan_helpers_test.go
```

Expected: No result inside any `TestPollPressureAPI_*` function. Unrelated tests can retain justified sleeps.

- [ ] **Step 7: Commit Task 3**

```text
git add \
  pkg/scanapp/pressure_test_helpers_test.go \
  pkg/scanapp/scan_observability_test.go \
  pkg/scanapp/scan_test.go \
  pkg/scanapp/scan_helpers_test.go
git commit -m "test(scanapp): remove pressure poll clock races (#79)"
```

### Task 4: Prove discrimination and run local gates

**Files:**

- Temporarily modify: `pkg/scanapp/pressure_monitor.go:93`
- Make no final production changes.

- [ ] **Step 1: Run the full focused stress test**

Run:

```text
go test -race -shuffle=on -count=100 ./pkg/scanapp -run '^TestPollPressureAPI_'
```

Expected: PASS.

- [ ] **Step 2: Apply the temporary mutation**

Temporarily replace this production statement:

```go
ctrl.SetAPIPaused(paused)
```

with this statement:

```go
// Temporary mutation for issue #79 discrimination proof.
```

Do not commit this mutation.

- [ ] **Step 3: Run the named test against the mutation**

Run:

```text
go test -race -count=1 ./pkg/scanapp -run '^TestPollPressureAPI_PressureAboveThreshold_Pauses$' -v
```

Expected: FAIL after the bounded wait because the controller never pauses.

- [ ] **Step 4: Restore the production statement**

Restore `ctrl.SetAPIPaused(paused)` with `apply_patch`.

Run:

```text
git diff --exit-code -- pkg/scanapp/pressure_monitor.go
```

Expected: exit 0. No production change remains.

- [ ] **Step 5: Run the named test after restoration**

Run:

```text
go test -race -count=20 ./pkg/scanapp -run '^TestPollPressureAPI_PressureAboveThreshold_Pauses$'
```

Expected: PASS.

- [ ] **Step 6: Run the full repository gate**

Run:

```text
make verify
```

Expected final line: `All selected quality gates passed.`

- [ ] **Step 7: Run the isolated pressure-control gate**

Run:

```text
make verify-e2e
```

Expected: exit 0 with the final e2e success line.

- [ ] **Step 8: Record the performance decision**

Record that no production hot path changed. Do not add or run a benchmark for this test-only change.

### Task 5: Create the Native Windows tracking issue

**External state:**

- Create one issue in `superplayground3000/port-scan-mk3`.
- Add one cross-reference comment to issue #79.
- Do not close issue #79.

- [ ] **Step 1: Make sure that no open tracker exists**

Run:

```text
gh issue list --repo superplayground3000/port-scan-mk3 --state open \
  --search 'in:title "Track changes that require Native Windows validation"' \
  --json number,title,url
```

Expected: No exact-title issue exists. If one exists, update it instead of creating a duplicate.

- [ ] **Step 2: Create the tracking issue**

Use this title:

```text
Track changes that require Native Windows validation
```

Use the `ready-for-agent` label. Use this body:

```markdown
## Purpose

This issue tracks changes that require validation in a Native Windows environment.

If a new change needs Native Windows, add one checklist entry to this issue.

## Agent procedure

1. Select the oldest pending entry.
2. Check out the source pull request or commit.
3. Run the listed setup and validation commands.
4. Add a comment with command output and run URLs.
5. Mark the entry complete.
6. If no entry is pending, remove `ready-for-agent`.

Keep this issue open for later validation entries.

## Pending validation

- [ ] Issue #79 pressure poll test determinism.
  - Source: #79 and its implementation pull request or commit.
  - Reason: The original failure occurred on a Native Windows runner.
  - Setup: `./scripts/windows_setup_mingw.ps1`
  - Focused command: `go test -race -shuffle=on -count=100 ./pkg/scanapp -run '^TestPollPressureAPI_'`
  - Full gate: `./scripts/windows_gate.ps1`
  - Expected result: Both commands exit 0. No `TestPollPressureAPI_*` test fails.
  - Evidence: Pending.
```

Create the issue with `gh issue create --body-file -` and a quoted heredoc.

- [ ] **Step 3: Add the cross-reference to issue #79**

Add this comment after the tracker number is known:

```text
Native Windows validation for this change is tracked in #<tracker-number>. A Windows agent will add the result evidence there.
```

- [ ] **Step 4: Read back both GitHub items**

Run `gh issue view` for the tracker and issue #79. Make sure that the body, label, and cross-reference are correct.

### Task 6: Obtain independent reviews and prepare handoff

**Files:**

- Review all commits after `af65db2`.
- Review the GitHub tracking issue and issue #79 comment.

- [ ] **Step 1: Make sure that the worktree is clean**

Run:

```text
git status --short
git diff --check master...HEAD
```

Expected: No uncommitted files and no whitespace errors.

- [ ] **Step 2: Run the final repository gate again after review fixes**

Run:

```text
make verify
```

Expected final line: `All selected quality gates passed.`

- [ ] **Step 3: Run the final pressure-control gate again after review fixes**

Run:

```text
make verify-e2e
```

Expected: exit 0 with the final e2e success line.

- [ ] **Step 4: Obtain final cross-provider review**

The reviewer must inspect the complete diff from `master` to `HEAD`.

Use this reviewer order:

1. Use a different provider when one is available.
2. Otherwise, use a different model.
3. Otherwise, use a fresh-context agent.

The reviewer must run these commands without trusting prior output:

```text
make verify
make verify-e2e
```

The review verdict must include the final output from both commands.

The reviewer must make sure that:

- No production code changed.
- Every `TestPollPressureAPI_*` synchronization sleep is removed.
- Negative-state tests cannot pass from their initial state.
- Poller and HTTP handler goroutines stop during cleanup.
- Red, green, mutation, race, repository, and e2e evidence exists.
- The Native Windows tracker contains the pending #79 entry.

- [ ] **Step 5: Apply all blocking review fixes**

Use the same red-green and gate sequence for each fix. Request review again until the verdict is approved.

- [ ] **Step 6: Record the handoff state**

Record these items:

- The branch and final commit.
- The tracking issue URL.
- The red failure message.
- The mutation failure message.
- The focused 100-run result.
- The final `make verify` line.
- The final `make verify-e2e` line.
- The independent review verdict.
- Native Windows status as pending.
