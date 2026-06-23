# Gradual Speed Control with Pressure Feedback

**Date:** 2026-03-23  
**Topic:** Gradual Ramp-Up/Ramp-Down Speed Control  
**Status:** Draft

---

## 1. Overview

Replace the existing binary pause/resume mechanism with a **proportional speed controller** that responds to pressure API readings with gradual speed transitions.

### Current Behavior (to be replaced)

| Pressure | Behavior |
|----------|----------|
| `>= 90%` | Gate blocks — full pause |
| `< 90%` | Gate opens — full speed |

### New Behavior

| Pressure Range | Behavior |
|---------------|----------|
| `>= 60%` | **Paused** — gate blocks, speed reset to 0 |
| `30% – 59%` | **Ramp-Down** — speed decreases by 10% per poll, floor at 20% |
| `< 30%` | **Ramp-Up** — speed increases by 10% per poll, ceiling at 100% |

---

## 2. Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Pause threshold = 60%** | Replaces hardcoded 90% with a lower, more sensitive threshold |
| **Safe zone = 30%** | Below this, scanner may safely ramp up toward full speed |
| **Fixed ±10% per poll** | Simple, predictable, stable correction per interval |
| **20% speed floor** | Prevents complete stall during sustained elevated pressure |
| **Speed starts at 0 after pause** | Cold start ensures pressure stabilizes before sending load |
| **Speed multiplier applied to bucket rate** | Workers and delay remain unaffected; bucket is the control point |

---

## 3. Three Pressure Zones

### Zone A: Paused (`pressure >= 60%`)

- `speedMultiplier = 0.0`
- Gate blocks all new dispatches
- Speed resets to 0 (cold resume)

### Zone B: Ramp-Down (`30% <= pressure < 60%`)

- `speedMultiplier -= 0.10` per poll
- Clamped to `[0.20, 1.00]`
- Gate open but dispatch slowed

### Zone C: Ramp-Up (`pressure < 30%`)

- `speedMultiplier += 0.10` per poll
- Clamped to `[0.20, 1.00]`
- Gate open and dispatch accelerating

---

## 4. Speed Multiplier

A `speedMultiplier float64` (range 0.0–1.0) is introduced to the speed controller:

```go
effectiveRate := configuredBucketRate * speedMultiplier
```

| Speed Multiplier | Effective Dispatch Rate |
|------------------|------------------------|
| 1.00 | 100% of configured bucket rate |
| 0.50 | 50% of configured bucket rate |
| 0.20 | 20% of configured bucket rate (floor) |
| 0.00 | Paused — gate blocks |

---

## 5. State Machine

```
        ┌─────────────────────────────────────────────┐
        │                                             │
        ▼                                             │
   ┌─────────┐  pressure >= 60%   ┌──────────┐        │
   │ Ramp-Up │ ──────────────────►│  Paused  │        │
   │ < 30%   │                    │ >= 60%   │        │
   └────┬────┘                    └──────────┘        │
        │                              ▲              │
        │ pressure >= 30%               │              │
        │ speedMultiplier -= 0.10       │              │
        │                              │              │
        │     ┌────────────────────────┘              │
        │     │                                       │
        │     ▼                                       │
        │  ┌────────────┐                             │
        │  │ Ramp-Down  │                             │
        └──│ 30%-59%    │─────────────────────────────┘
           └────────────┘   pressure < 30%
                          speedMultiplier += 0.10
```

---

## 6. Component Changes

### 6.1 `pkg/speedctrl/controller.go`

**New fields on `Controller`:**

```go
type Controller struct {
    mu               sync.RWMutex
    apiPaused        bool        // unchanged — true when pressure >= 60%
    manualPaused     bool        // unchanged
    speedMultiplier  float64     // NEW: 0.0 to 1.0, applied to bucket rate
    gate             chan struct{}
}
```

**New methods:**

| Method | Description |
|--------|-------------|
| `SetSpeedMultiplier(v float64)` | Sets speed multiplier directly (for pressure-based control) |
| `GetSpeedMultiplier() float64` | Returns current speed multiplier |
| `AdjustSpeedMultiplier(delta float64)` | Increments by delta, clamps to [0.20, 1.00] |
| `ResetSpeedMultiplier()` | Resets to 0.0 (called on pause entry) |

**Modified `recomputeLocked()`:**

```go
func (c *Controller) recomputeLocked() {
    // gate logic unchanged — blocks when apiPaused OR manualPaused
    // speedMultiplier managed separately via AdjustSpeedMultiplier
}
```

**Modified `Gate()` behavior:**

- Gate blocks when `apiPaused || manualPaused` (unchanged)
- Speed multiplier does NOT affect gate — only the bucket rate applied at dispatch time

### 6.2 `pkg/scanapp/pressure_monitor.go`

**Modified `pollPressureAPI()`:**

```go
func pollPressureAPI(...) {
    thresholdPause := 60.0   // replaces hardcoded 90
    thresholdSafe := 30.0    // new safe zone boundary
    rampStep := 0.10         // 10% per poll

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            pressure, err := fetcher.Fetch(ctx)
            // ... failure handling unchanged (3-strike fail-fast)

            if pressure >= thresholdPause {
                // Zone A: Paused
                ctrl.SetAPIPaused(true)
                ctrl.ResetSpeedMultiplier()  // cold start at 0
            } else if pressure >= thresholdSafe {
                // Zone B: Ramp-Down
                ctrl.SetAPIPaused(false)
                ctrl.AdjustSpeedMultiplier(-rampStep)
            } else {
                // Zone C: Ramp-Up
                ctrl.SetAPIPaused(false)
                ctrl.AdjustSpeedMultiplier(rampStep)
            }
        }
    }
}
```

### 6.3 `pkg/scanapp/task_dispatcher.go`

**Modified `dispatchTasks()` bucket usage:**

```go
func dispatchTasks(..., ctrl *speedctrl.Controller, ...) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-ctrl.Gate():
            // Apply speed multiplier to bucket rate
            multiplier := ctrl.GetSpeedMultiplier()
            effectiveRate := cfg.BucketRate * multiplier
            effectiveCapacity := int(float64(cfg.BucketCapacity) * multiplier)

            bkt := ratelimit.NewLeakyBucket(effectiveRate, effectiveCapacity)
            if err := bkt.Acquire(ctx); err != nil {
                // ... handle error
            }
            // ... dispatch task
        }
    }
}
```

> **Note:** `effectiveCapacity` at low multiplier may reduce burst. This is acceptable — burst reduction is consistent with the goal of lowering pressure.

---

## 7. Data Flow

```
Pressure API Poll (every -pressure-interval, default 5s)
        │
        ▼
┌───────────────────────────────────────┐
│  Zone Classification                  │
│  - >= 60%: Paused                     │
│  - 30-59%: Ramp-Down                  │
│  - < 30%: Ramp-Up                     │
└───────────────────┬───────────────────┘
                    │
                    ▼
┌───────────────────────────────────────┐
│  Speed Multiplier Update               │
│  - Paused: reset to 0.0                │
│  - Ramp-Down: -= 0.10, clamp [0.20,1] │
│  - Ramp-Up:   += 0.10, clamp [0.20,1] │
└───────────────────┬───────────────────┘
                    │
                    ▼
┌───────────────────────────────────────┐
│  Bucket Rate Adjustment               │
│  effectiveRate = bucketRate * mult    │
│  effectiveCapacity = bucketCap * mult  │
└───────────────────┬───────────────────┘
                    │
                    ▼
┌───────────────────────────────────────┐
│  Dispatch Loop (per task)             │
│  - Wait on gate (if paused)           │
│  - Acquire from adjusted bucket       │
│  - Send to worker pool                │
└───────────────────────────────────────┘
```

---

## 8. CLI Flag Changes

| Flag | Default | Description |
|------|---------|-------------|
| `-pressure-api` | unchanged | Pressure API URL |
| `-pressure-interval` | unchanged | Poll interval |
| `-pause-threshold` | `60` | New: pressure percentage to trigger full pause |
| `-safe-threshold` | `30` | New: pressure percentage for safe/ramp-up zone |
| `-ramp-step` | `0.10` | New: speed adjustment per poll (fraction) |

> **Note:** Existing `-disable-api` flag unchanged.

---

## 9. Error Handling

| Scenario | Behavior |
|----------|----------|
| API 3 consecutive failures | Fail-fast, scan exits — unchanged |
| Pressure returns to safe zone | Ramp-up begins from current multiplier |
| Pressure returns to high zone | Ramp-down resumes from current multiplier |
| Manual pause toggled | Speed multiplier preserved during manual pause |

---

## 10. Testing Implications

### 10.1 New Test Cases

| Test | Scenario |
|------|----------|
| Ramp-down zone | Pressure 45% → speed multiplier decreases each poll |
| Ramp-down floor | Pressure stays 30-59% → multiplier clamps at 0.20 |
| Ramp-up zone | Pressure 20% → speed multiplier increases each poll |
| Ramp-up ceiling | Pressure stays < 30% → multiplier clamps at 1.00 |
| Pause entry | Pressure >= 60% → multiplier resets to 0.0 |
| Pause to ramp-up | Pressure drops to 20% after pause → ramp from 0 upward |
| Pause to ramp-down | Pressure drops to 45% after pause → ramp from 0 downward |
| Boundary 60% | Pressure exactly 60% → triggers pause |
| Boundary 30% | Pressure exactly 30% → triggers ramp-down (not ramp-up) |

### 10.2 Files to Modify

| File | Changes |
|------|---------|
| `pkg/speedctrl/controller.go` | Add speedMultiplier, new methods |
| `pkg/speedctrl/controller_test.go` | Add unit tests for new methods |
| `pkg/scanapp/pressure_monitor.go` | Update poll logic for zones |
| `pkg/scanapp/task_dispatcher.go` | Apply multiplier to bucket |
| `pkg/scanapp/scan_helpers_test.go` | Update pressure scenario tests |
| `pkg/scanapp/scan_observability_test.go` | Update telemetry expectations |
| `pkg/config/config.go` | Add new CLI flags |

---

## 11. Out of Scope

- Changing worker pool size dynamically
- Modifying delay-based rate limiting
- External metrics export beyond existing telemetry
- Multi-tenant or priority-based speed allocation

---

## 12. Rollout Considerations

1. **Backward compatibility:** Default values preserve existing 90% pause behavior until user adopts new flags
2. **Gradual adoption:** Users can set `-pause-threshold=90` to retain old behavior
3. **Observability:** Existing pressure logging unchanged; speed multiplier can be added to dashboard state later
