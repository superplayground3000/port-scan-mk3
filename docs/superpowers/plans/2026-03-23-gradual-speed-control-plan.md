# Gradual Speed Control Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the binary pause/resume mechanism with proportional speed control driven by pressure API readings. Speed adjusts ±10% per poll toward a target rate, with a 20% floor and full pause at >= 60%.

**Architecture:** Speed multiplier (0.0–1.0) lives in `speedctrl.Controller`. A new `SetRateMultiplier(float64)` method on `LeakyBucket` adjusts the bucket's refill ticker interval dynamically. At dispatch time, `pollPressureAPI` calls `SetRateMultiplier` after each pressure poll. Pressure zones in `pressure_monitor.go` drive multiplier adjustments via `ctrl.AdjustSpeedMultiplier()`.

**Tech Stack:** Go 1.24.x, existing `pkg/speedctrl`, `pkg/scanapp`, `pkg/config`, `pkg/ratelimit`

---

## File Map

```
pkg/speedctrl/controller.go          MODIFY — add speedMultiplier + methods
pkg/speedctrl/controller_test.go     MODIFY — add speed multiplier tests
pkg/scanapp/pressure_monitor.go      MODIFY — zone-based multiplier adjustment
pkg/scanapp/task_dispatcher.go       MODIFY — apply multiplier as extra delay
pkg/config/config.go                 MODIFY — add PauseThreshold, SafeThreshold, RampStep
pkg/scanapp/scan.go                  MODIFY — pass new thresholds to RunOptions
```

---

## Task 1: Add speedMultiplier to Controller

**Files:**
- Modify: `pkg/speedctrl/controller.go`
- Modify: `pkg/speedctrl/controller_test.go`

- [ ] **Step 1: Add speedMultiplier field and constants to controller.go**

Add after the existing fields:

```go
type Controller struct {
    mu               sync.RWMutex
    apiPaused        bool
    manualPaused     bool
    speedMultiplier  float64  // 0.0 to 1.0, applied as dispatch throttle
    gate             chan struct{}
}

// Speed multiplier bounds
const (
    speedMultiplierMin = 0.20  // 20% floor during ramp-down/ramp-up
    speedMultiplierMax = 1.00  // 100% ceiling
)
```

**Note:** `ResetSpeedMultiplier()` sets to 0.0 (cold start for Zone C ramp-up). When entering Zone B from pause, use `SetSpeedMultiplier(speedMultiplierMin)` instead to allow downward ramp.

- [ ] **Step 2: Add speedMultiplier methods to controller.go**

Add after the existing methods (before the closing `}`):

```go
// SetSpeedMultiplier sets speed multiplier directly.
func (c *Controller) SetSpeedMultiplier(v float64) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.speedMultiplier = v
}

// GetSpeedMultiplier returns the current speed multiplier.
func (c *Controller) GetSpeedMultiplier() float64 {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.speedMultiplier
}

// AdjustSpeedMultiplier increments multiplier by delta, clamped to [min, max].
// delta should be positive for ramp-up, negative for ramp-down.
// Does not affect pause state.
func (c *Controller) AdjustSpeedMultiplier(delta float64) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.speedMultiplier += delta
    if c.speedMultiplier < speedMultiplierMin {
        c.speedMultiplier = speedMultiplierMin
    }
    if c.speedMultiplier > speedMultiplierMax {
        c.speedMultiplier = speedMultiplierMax
    }
}

// ResetSpeedMultiplier resets to 0.0 — called when entering pause zone.
func (c *Controller) ResetSpeedMultiplier() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.speedMultiplier = 0.0
}
```

- [ ] **Step 3: Add unit tests for speed multiplier in controller_test.go**

Add at the end of `controller_test.go`:

```go
func TestController_SpeedMultiplier_AdjustAndClamp(t *testing.T) {
    c := NewController()

    // Starts at 1.0
    if got := c.GetSpeedMultiplier(); got != 1.0 {
        t.Fatalf("expected initial multiplier 1.0, got %.2f", got)
    }

    // Adjust upward
    c.AdjustSpeedMultiplier(0.10)
    if got := c.GetSpeedMultiplier(); got != 1.10 {
        t.Fatalf("expected 1.10 before clamp, got %.2f", got)
    }
    if got := c.GetSpeedMultiplier(); got != 1.0 {
        t.Fatalf("expected clamped to 1.0, got %.2f", got)
    }

    // Adjust downward below floor
    c.SetSpeedMultiplier(0.25)
    c.AdjustSpeedMultiplier(-0.10) // 0.15 -> clamped to 0.20
    if got := c.GetSpeedMultiplier(); got != 0.20 {
        t.Fatalf("expected floor 0.20, got %.2f", got)
    }

    // Reset to 0
    c.ResetSpeedMultiplier()
    if got := c.GetSpeedMultiplier(); got != 0.0 {
        t.Fatalf("expected 0.0 after reset, got %.2f", got)
    }
}

func TestController_SpeedMultiplier_DoesNotAffectGate(t *testing.T) {
    c := NewController(WithAPIEnabled(false))

    // multiplier=0 should not block gate
    c.ResetSpeedMultiplier()
    select {
    case <-c.Gate():
        // expected — gate open regardless of multiplier
    default:
        t.Fatal("gate should be open when not paused")
    }

    // Pause should block gate
    c.SetAPIPaused(true)
    c.SetSpeedMultiplier(1.0) // full speed, but paused
    select {
    case <-c.Gate():
        t.Fatal("gate should block when paused")
    case <-time.After(10 * time.Millisecond):
        // expected
    }
}
```

- [ ] **Step 4: Run controller tests**

```bash
go test ./pkg/speedctrl/... -v
```
Expected: PASS on all tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/speedctrl/controller.go pkg/speedctrl/controller_test.go
git commit -m "feat(speedctrl): add speedMultiplier field and adjustment methods"
```

---

## Task 2: Add new CLI flags

**Files:**
- Modify: `pkg/config/config.go`

- [ ] **Step 1: Add new fields to Config struct**

Add after `PressureInterval` field:

```go
type Config struct {
    // ... existing fields ...

    PauseThreshold int     // NEW: pressure % to trigger full pause (default 60)
    SafeThreshold  int     // NEW: pressure % for safe/ramp-up zone (default 30)
    RampStep       float64 // NEW: speed adjustment fraction per poll (default 0.10)
}
```

- [ ] **Step 2: Register new flags in Parse()**

Add after the existing pressure API flag registrations (after line 56):

```go
fs.IntVar(&cfg.PauseThreshold, "pause-threshold", 60, "pressure percentage to trigger full pause")
fs.IntVar(&cfg.SafeThreshold, "safe-threshold", 30, "pressure percentage for safe/ramp-up zone")
fs.Float64Var(&cfg.RampStep, "ramp-step", 0.10, "speed adjustment fraction per poll (e.g., 0.10 = ±10%)")
```

- [ ] **Step 3: Add validation**

Add after the existing `PressureInterval <= 0` validation block:

```go
if cfg.PauseThreshold <= 0 || cfg.PauseThreshold > 100 {
    return Config{}, errors.New("-pause-threshold must be between 1 and 100")
}
if cfg.SafeThreshold <= 0 || cfg.SafeThreshold >= cfg.PauseThreshold {
    return Config{}, errors.New("-safe-threshold must be positive and less than -pause-threshold")
}
if cfg.RampStep <= 0 || cfg.RampStep > 1 {
    return Config{}, errors.New("-ramp-step must be between 0 and 1")
}
```

- [ ] **Step 4: Run config tests**

```bash
go test ./pkg/config/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/config/config.go pkg/config/config_test.go
git commit -m "feat(config): add -pause-threshold, -safe-threshold, -ramp-step flags"
```

---

## Task 3: Update pressure_monitor.go with zone-based logic

**⚠️ Known Spec Issue — Zone B Entry from Pause:**

When resuming from pause (pressure drops from >=60% to 30-59%), the spec says "speed starts at 0, then ramps up." But Zone B is ramp-*down*. Starting at 0 and then calling `AdjustSpeedMultiplier(-0.10)` clamps immediately to 0.20 — the multiplier never decreases below 0.20.

**Resolution:** When exiting pause into Zone B (30-59%), set multiplier to 0.20 (floor) instead of 0.0. This lets ramp-down actually decrease: 0.20 → 0.10 → 0.20 (floor). When exiting pause into Zone C (<30%), multiplier starts at 0.0 and ramps up normally.

**Files:**
- Modify: `pkg/scanapp/pressure_monitor.go`

- [ ] **Step 1: Review current pollPressureAPI implementation**

Lines 38–107 of `pressure_monitor.go`. Note the current hardcoded `thresholdValue` (90) used for pause comparison.

- [ ] **Step 2: Update pollPressureAPI to use zone-based logic**

Replace the pressure comparison block (lines 88–104) with:

```go
thresholdPause := float64(opts.PauseThreshold)  // default 60
thresholdSafe := float64(opts.SafeThreshold)   // default 30
rampStep := opts.RampStep                      // default 0.10

consecutiveFailures = 0
logger.infof("[API] pressure api status=ok pressure=%.1f%% pause_thresh=%.1f safe_thresh=%.1f", pressure, thresholdPause, thresholdSafe)

sampledAt := time.Now()
if pressureObserver != nil {
    pressureObserver.OnPressureSample(int(pressure), sampledAt)
}

if pressure >= thresholdPause {
    // Zone A: Paused
    ctrl.SetAPIPaused(true)
    ctrl.ResetSpeedMultiplier() // cold start at 0
} else if pressure >= thresholdSafe {
    // Zone B: Ramp-Down
    ctrl.SetAPIPaused(false)
    if ctrl.GetSpeedMultiplier() == 0.0 {
        // Entering ramp-down from pause: start at floor so we can decrease
        ctrl.SetSpeedMultiplier(speedMultiplierMin) // 0.20
    }
    ctrl.AdjustSpeedMultiplier(-rampStep)
} else {
    // Zone C: Ramp-Up
    ctrl.SetAPIPaused(false)
    ctrl.AdjustSpeedMultiplier(rampStep)
}
```

- [ ] **Step 3: Ensure RunOptions has the new fields**

In `pkg/scanapp/scan.go`, add to `RunOptions`:

```go
type RunOptions struct {
    // ... existing fields ...
    PauseThreshold int     // NEW: passed from config to pollPressureAPI
    SafeThreshold  int     // NEW: safe zone boundary
    RampStep       float64  // NEW: speed adjustment per poll
}
```

Then in `scan.go` where `runOpts` is built (around line 107), set:

```go
runOpts.PauseThreshold = cfg.PauseThreshold
runOpts.SafeThreshold = cfg.SafeThreshold
runOpts.RampStep = cfg.RampStep
```

- [ ] **Step 4: Run scanapp tests**

```bash
go test ./pkg/scanapp/... -v -run 'pressure|pause|PollPressure'
```
Expected: PASS. May need to update test assertions that reference the old 90 threshold.

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/pressure_monitor.go pkg/scanapp/scan.go pkg/scanapp/runopts.go  # whichever files changed
git commit -m "feat(scanapp): zone-based speed control via pressure thresholds"
```

---

## Task 4: Add SetRateMultiplier to LeakyBucket

**Files:**
- Modify: `pkg/ratelimit/leaky_bucket.go`
- Modify: `pkg/ratelimit/leaky_bucket_test.go`

- [ ] **Step 1: Add SetRateMultiplier method to leaky_bucket.go**

Add after the `Close()` method:

```go
// SetRateMultiplier scales the bucket refill rate.
// multiplier must be in (0.0, 1.0]. 1.0 = no change, 0.5 = half rate.
func (b *LeakyBucket) SetRateMultiplier(multiplier float64) {
    if multiplier <= 0 || multiplier > 1.0 {
        return
    }
    // Note: we don't actually change the ticker interval since it's
    // already created. Instead we use the multiplier in Acquire to
    // add a proportional wait. This approach avoids ticker restart.
    // The bucket still provides burst capacity; multiplier throttles
    // the effective rate by adding wait time in Acquire.
}
```

**Actually**, since the ticker can't be easily restarted, the cleanest approach is to handle rate reduction in the dispatcher via delay. The bucket remains burst-only. This is simpler than restarting tickers.

**Revised approach for Task 4B below.**

- [ ] **Step 2: Run ratelimit tests**

```bash
go test ./pkg/ratelimit/... -v
```

- [ ] **Step 3: Commit**

```bash
git add pkg/ratelimit/leaky_bucket.go
git commit -m "feat(ratelimit): prepare for rate multiplier (no-op stub)"
```

---

## Task 4B: Apply speed multiplier via delay in task_dispatcher

**Files:**
- Modify: `pkg/scanapp/task_dispatcher.go`

**Design decision:** Since `LeakyBucket` uses a fixed ticker that can't easily change rate at runtime, the speed multiplier is applied as an **extra delay per dispatch** rather than changing bucket refill rate. The bucket still provides burst capacity (initial tokens). The extra delay throttles the effective dispatch rate proportionally.

- [ ] **Step 1: Update dispatchTasks loop to apply speed multiplier as extra delay**

Replace the dispatch inner loop (around lines 37–73) with:

```go
for i := snap.NextIndex; i < snap.TotalCount; i++ {
    obs.OnBucketWaitStart(ch.CIDR, i)
    if err := rt.bkt.Acquire(ctx); err != nil {
        return err
    }
    obs.OnBucketAcquired(ch.CIDR, i)

    obs.OnGateWaitStart(ch.CIDR, i)
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-ctrl.Gate():
    }
    obs.OnGateReleased(ch.CIDR, i)

    target, port, err := indexToRuntimeTarget(rt.targets, rt.ports, i)
    if err != nil {
        return err
    }
    select {
    case <-ctx.Done():
        return ctx.Err()
    case taskCh <- scanTask{
        chunkIdx: idx,
        ipCidr:   defaultString(target.ipCidr, ch.CIDR),
        ip:       target.ip,
        port:     port,
        meta:     target.meta,
    }:
    }
    obs.OnTaskEnqueued(ch.CIDR, i)
    rt.tracker.AdvanceNextIndex(i + 1)
    logger.debugf("dispatch cidr=%s target=%s:%d next_index=%d/%d", ch.CIDR, target.ip, port, i+1, snap.TotalCount)

    // Apply speed multiplier as extra delay
    multiplier := ctrl.GetSpeedMultiplier()
    if multiplier < 1.0 && policy.delay > 0 {
        // extraDelay = baseDelay * (1/multiplier - 1)
        // At multiplier=1.0: extraDelay=0 (no throttle)
        // At multiplier=0.5: 10ms -> 20ms effective
        // At multiplier=0.2: 10ms -> 50ms effective
        extraDelay := time.Duration(float64(policy.delay) * (1.0/multiplier - 1.0))
        time.Sleep(extraDelay)
    } else if policy.delay > 0 {
        time.Sleep(policy.delay)
    }
}
```

- [ ] **Step 2: Handle multiplier == 0 (pause zone)**

When `multiplier == 0`, gate blocks before reaching the delay code, so no special handling needed.

- [ ] **Step 3: Run dispatcher tests**

```bash
go test ./pkg/scanapp/... -v -run 'dispatch|Dispatch'
```
Expected: PASS. Existing tests with no pressure control will have multiplier=1.0, so no extra delay.

- [ ] **Step 4: Commit**

```bash
git add pkg/scanapp/task_dispatcher.go
git commit -m "feat(scanapp): apply speed multiplier as extra dispatch delay"
```

---

## Task 5: Update existing pressure-related tests

**Files:**
- Modify: `pkg/scanapp/scan_helpers_test.go`
- Modify: `pkg/scanapp/scan_observability_test.go`

- [ ] **Step 1: Update TestPollPressureAPI_WhenFirstTwoRequestsFail**

The test at line 710 uses `PressureLimit: 90`. Update to use new zones:

```go
// Old: PressureLimit: 90
// New: pressure 10 is in safe zone (< 30), should trigger ramp-up
RunOptions{
    PressureLimit:   60, // used as pause threshold
    PauseThreshold:  60, // new field
    SafeThreshold:   30,
    RampStep:        0.10,
    PressureHTTP:    &http.Client{Timeout: time.Second},
}
```

- [ ] **Step 2: Review scan_observability_test.go pressure telemetry tests**

Check tests that assert on `OnPressureSample` calls. The zone logic changes when `SetAPIPaused` is called — it's now called in all three zones (pause, ramp-down, ramp-up), but with different multiplier adjustments.

- [ ] **Step 3: Run all scanapp tests**

```bash
go test ./pkg/scanapp/... -v
```
Expected: All pass. Fix any test that fails due to changed pause/resume thresholds.

- [ ] **Step 4: Commit**

```bash
git add pkg/scanapp/scan_helpers_test.go pkg/scanapp/scan_observability_test.go
git commit -m "test(scanapp): update pressure tests for zone-based speed control"
```

---

## Task 6: Integration test with new zones

**Files:**
- Create: `pkg/scanapp/speed_control_test.go` (or add to existing test file)

- [ ] **Step 1: Write integration test for zone transitions**

```go
func TestPressureZones_Transitions(t *testing.T) {
    // Test: pressure 45% (Zone B: Ramp-Down)
    // After 3 polls at 45%: multiplier = 1.0 - 3*0.10 = 0.70

    // Test: pressure 20% (Zone C: Ramp-Up)
    // After 5 polls at 20%: multiplier = 0.20 + 5*0.10 = 0.70 (floor at 0.20)
    // Wait, 0.20 + 5*0.10 = 0.70, no floor hit
    // After 9 polls: multiplier = 0.20 + 9*0.10 = 1.10 -> clamped to 1.0

    // Test: pressure 65% (Zone A: Paused)
    // multiplier resets to 0.0
}
```

- [ ] **Step 2: Run and verify**

```bash
go test ./pkg/scanapp/... -v -run 'Zone|SpeedControl'
```

---

## Task 7: Verify end-to-end

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -count=1
```

- [ ] **Step 2: Run e2e if available**

```bash
bash e2e/run_e2e.sh
```

---

## Summary of Commits

| # | Message |
|---|---------|
| 1 | `feat(speedctrl): add speedMultiplier field and adjustment methods` |
| 2 | `feat(config): add -pause-threshold, -safe-threshold, -ramp-step flags` |
| 3 | `feat(scanapp): zone-based speed control via pressure thresholds` |
| 4 | `feat(scanapp): apply speed multiplier as extra dispatch delay` |
| 5 | `test(scanapp): update pressure tests for zone-based speed control` |
| 6 | `test(scanapp): add zone transition integration tests` |
