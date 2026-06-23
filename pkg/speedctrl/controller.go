// Package speedctrl provides pause/resume control for the scan dispatcher.
// It exposes a gate mechanism that the dispatcher waits on before dispatching
// each task. The gate is open by default and closes when either API-based
// pressure throttling or manual user input (space bar) requests a pause.
//
// # Function Flow
//
//	NewController
//	  |
//	  v
//	IsPaused() == false?  ── yes ── Gate() open
//	  | no
//	  v
//	API sets SetAPIPaused(true) / SetAPIPaused(false)
//	User presses space: ToggleManualPaused()
//	  |
//	  v
//	Gate() blocks until IsPaused() becomes false again
package speedctrl

import "sync"

// Option configures a Controller at construction time.
type Option func(*Controller)

// WithAPIEnabled controls whether the API-based pause mechanism is initially
// enabled. When false, SetAPIPaused has no effect on the gate.
func WithAPIEnabled(enabled bool) Option {
	return func(c *Controller) {
		if !enabled {
			c.apiPaused = false
		}
	}
}

// Controller manages the dispatch gate for scan throttling. It is safe for
// concurrent use. The gate blocks task dispatch when either apiPaused or
// manualPaused is true.
type Controller struct {
	mu           sync.RWMutex
	apiPaused    bool
	manualPaused bool
	gate         chan struct{}
}

// NewController creates a Controller with an open gate (dispatch allowed by default).
// The gate is closed when either apiPaused or manualPaused becomes true and
// reopens when both are false.
//
// # Parameters
//
//	opts: Optional configuration functions (e.g., WithAPIEnabled).
//
// # Example
//
//	ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(true))
func NewController(opts ...Option) *Controller {
	c := &Controller{gate: make(chan struct{})}
	close(c.gate)
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Controller) recomputeLocked() {
	paused := c.apiPaused || c.manualPaused
	if paused {
		select {
		case <-c.gate:
			c.gate = make(chan struct{})
		default:
		}
		return
	}
	select {
	case <-c.gate:
	default:
		close(c.gate)
	}
}

// SetAPIPaused sets whether the API-driven pause is active. When true, the
// dispatch gate closes and task dispatch is halted until SetAPIPaused(false)
// is called.
func (c *Controller) SetAPIPaused(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiPaused = v
	c.recomputeLocked()
}

// SetManualPaused sets whether manual (keyboard) pause is active.
func (c *Controller) SetManualPaused(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manualPaused = v
	c.recomputeLocked()
}

// ToggleManualPaused flips the manual paused state and returns the new state.
func (c *Controller) ToggleManualPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manualPaused = !c.manualPaused
	c.recomputeLocked()
	return c.manualPaused
}

// ManualPaused returns true when the manual pause (keyboard space bar) is active.
func (c *Controller) ManualPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.manualPaused
}

// APIPaused returns true when the API-driven pause is active.
func (c *Controller) APIPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiPaused
}

// IsPaused returns true when either API-driven or manual pause is active.
func (c *Controller) IsPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiPaused || c.manualPaused
}

// Gate returns a channel that blocks when the scan is paused. Dispatchers
// select on this channel: when it is open (receivable), dispatch may proceed.
// When closed, the dispatcher waits until it is reopened.
func (c *Controller) Gate() <-chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gate
}
