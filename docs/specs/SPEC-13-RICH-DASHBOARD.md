# SPEC-13: Rich Scan Dashboard Specification

## Overview

The Rich Dashboard provides a real-time terminal UI during scan execution, showing progress, rate metrics, and system status.

> **Status:** The dashboard is implemented for the `scan` command.

## 1. Design Principles

- **Default enabled**: Rich dashboard activates automatically in TTY mode
- **Non-TTY fallback**: Automatically falls back to plain output when not in terminal
- **Format-aware**: Disabled when `-format=json` to maintain machine-readable output
- **Failure isolation**: Dashboard failures must not interrupt the scan

## 2. Enabling Conditions

Rich dashboard activates only when ALL conditions are met:

| Condition | Check |
|-----------|-------|
| Command | `scan` |
| stderr is TTY | `term.IsTerminal(stderr)` |
| Format is human | `cfg.Format != "json"` |

Otherwise: falls back to existing output behavior.

## 3. Dashboard Components

### 3.1 Progress Section

```
Progress: ████████████░░░░░░░ 60% (600/1000)
```

Fields:
- `TotalTasks`: Total probe tasks
- `ScannedTasks`: Committed results
- `Percent`: Calculated percentage

### 3.2 Current CIDR Section

```
Current CIDR: 10.0.0.0/24
Bucket: waiting_bucket | waiting_gate | enqueued
```

Fields:
- `CIDR`: Current CIDR being scanned
- `BucketStatus`: `waiting_bucket` | `waiting_gate` | `enqueued`

### 3.3 Speed Section

```
Speed: dispatch=150.2/s | results=148.5/s
```

Fields:
- `DispatchPerSec`: Tasks dispatched per second
- `ResultsPerSec`: Results received per second
- **Calculation**: 5-second sliding window

### 3.4 Controller Section

```
Status: RUNNING
```

Status values:
| Status | Meaning |
|--------|---------|
| `RUNNING` | Not paused |
| `PAUSED(API)` | Paused by pressure API |
| `PAUSED(MANUAL)` | Paused by user (spacebar) |
| `PAUSED(API+MANUAL)` | Paused by both |

### 3.5 API Section

```
API: pressure=85% | last_update=2s ago | health=ok
API Sources: src1=85% ok | src2=42% ok | src3=- fail streak 1
```

Fields:
- `PressurePercent`: Current pressure percentage
- `LastUpdatedAt`: Time since last update
- `HealthText`: `ok` or `fail streak N`
- `APISources`: Ordered compact source health states for multi-source pressure APIs.
  Each source uses the configured data URL order (`src1`, `src2`, ...), preserves
  the last successful pressure value, and shows per-source failure streaks when a
  source fails.

## 4. Architecture Components

### 4.1 dashboard_state

```go
type DashboardState struct {
    mu sync.RWMutex
    
    // Progress
    TotalTasks   int
    ScannedTasks int
    
    // Current CIDR
    CurrentCIDR    string
    BucketStatus   string  // waiting_bucket | waiting_gate | enqueued
    
    // Speed (5-second sliding window)
    DispatchPerSec float64
    ResultsPerSec float64
    
    // Controller
    ControllerStatus string  // RUNNING | PAUSED(...)
    
    // API
    PressurePercent int
    LastUpdatedAt  time.Time
    APIFailStreak  int
    APISources      []DashboardAPISource
}
```

**Thread-safe**: All reads/writes protected by mutex.

### 4.2 dashboard_observer

Receives events from scan pipeline:

| Event | Updates |
|-------|---------|
| `OnBucketWaitStart` | `BucketStatus = "waiting_bucket"` |
| `OnGateWaitStart` | `BucketStatus = "waiting_gate"` |
| `OnTaskEnqueued` | `BucketStatus = "enqueued"`, increment dispatch counter |
| On committed result | Increment result counter |

### 4.3 dashboard_renderer

Renders state to ANSI escape sequences:

```go
type Renderer interface {
    Render(state DashboardState) string
}
```

Output format:
- Uses ANSI escape codes for cursor positioning
- Clears screen on each refresh
- 500ms refresh interval

### 4.4 dashboard_runtime

Manages dashboard lifecycle:

```go
type DashboardRuntime struct {
    state    *DashboardState
    observer *DashboardObserver
    renderer *Renderer
    ticker   *time.Ticker
    done     chan struct{}
}

func (d *DashboardRuntime) Start(ctx context.Context, stderr io.Writer) error
func (d *DashboardRuntime) Stop() error
```

## 5. Event Flow

```
dispatchTasks
    │
    ├── OnBucketWaitStart ──► dashboard_observer ──┐
    ├── OnGateWaitStart ──────────────────────────┤
    └── OnTaskEnqueued ────────────────────────────┤
                                                   ▼
result_aggregator                          dashboard_state
    │                                               │
    └── OnResult ──────────► increment counter ◄─────┘
                                                   │
                                                   ▼
                                    dashboard_renderer (every 500ms)
                                                   │
                                                   ▼
                                            stderr (TTY)
```

## 6. API State Observable (Controller Extension)

The controller exposes its API pause state for the dashboard:

```go
func (c *Controller) APIPaused() bool
```

The dashboard uses this value to display the pause status.

## 7. Error Handling

| Scenario | Behavior |
|----------|----------|
| Render to stderr fails | Log warning, disable dashboard, continue scan |
| Ticker goroutine panic | Recover, log error, stop dashboard |
| Context canceled | Stop dashboard gracefully |

## 8. Refresh Rate

- **Interval**: 500ms
- **Implementation**: `time.NewTicker(500 * time.Millisecond)`

## 9. Output Location

- **Destination**: stderr only (not stdout)
- **Reason**: stdout reserved for CSV output

## 10. File Structure

```
pkg/scanapp/
├── dashboard_state.go        # State management
├── dashboard_state_test.go  # State tests
├── dashboard_renderer.go    # ANSI rendering
├── dashboard_renderer_test.go # Renderer tests
├── dashboard_runtime.go     # Lifecycle management
├── dashboard_runtime_test.go # Runtime tests
└── dashboard_telemetry.go   # Event adapters
```

## 11. Integration Points

- **scan.go**: Creates and starts dashboard runtime
- **task_dispatcher.go**: Reports bucket/gate events via observer
- **result_aggregator.go**: Reports result events
- **pressure_monitor.go**: Reports pressure telemetry
- **speedctrl/controller.go**: Exposes `APIPaused()` accessor

## 12. Non-Goals

This version does NOT include:
- `-ui` or `-ui-refresh` CLI flags
- Display of all CIDR list
- Bucket token water level visibility
- Custom color schemes
