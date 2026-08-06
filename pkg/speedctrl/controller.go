// Package speedctrl provides pause and resume control for the scan dispatcher.
// The package gives a gate that the dispatcher waits on before it dispatches
// each task. The gate is open by default. The gate closes when API pressure
// throttling or manual user input (space bar) requests a pause.
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

// WithAPIEnabled controls whether the API pause mechanism starts enabled. If
// enabled is false, SetAPIPaused has no effect on the gate.
func WithAPIEnabled(enabled bool) Option {
	return func(c *Controller) {
		if !enabled {
			c.apiPaused = false
		}
	}
}

// Controller manages the dispatch gate for scan throttling. Controller is safe
// for concurrent use. The gate blocks task dispatch when apiPaused or
// manualPaused is true.
type Controller struct {
	mu           sync.RWMutex
	apiPaused    bool
	manualPaused bool
	gate         chan struct{}
}

// NewController creates a Controller with an open gate. The Controller allows
// dispatch by default. The gate closes when apiPaused or manualPaused becomes
// true. The gate opens again when both are false.
//
// # Parameters
//
//	opts: Optional configuration functions, for example WithAPIEnabled.
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

// SetAPIPaused sets whether the API pause is active. If v is true, the dispatch
// gate closes. Task dispatch then stops until a call to SetAPIPaused(false).
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

// ToggleManualPaused sets the manual paused state to the opposite value and
// returns the new state.
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

// APIPaused returns true when the API pause is active.
func (c *Controller) APIPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiPaused
}

// IsPaused returns true when the API pause or the manual pause is active.
func (c *Controller) IsPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiPaused || c.manualPaused
}

// Gate returns a channel that a dispatcher receives from before it sends the
// next task. While the scan runs, the receive succeeds immediately. While the
// scan is paused, the receive blocks until the gate opens again.
//
// Internally an open gate is a CLOSED channel, because a receive on a closed
// channel never blocks. A paused gate is an open channel with no sender.
func (c *Controller) Gate() <-chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gate
}
