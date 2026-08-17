package perfharness

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
)

// CancellationInjector cancels one stage at a deterministic progress point.
type CancellationInjector struct {
	threshold uint64
	cancel    func()
	once      sync.Once
	completed atomic.Uint64
}

// NewCancellationInjector returns one deterministic cancellation injector.
func NewCancellationInjector(stage CancellationStage, percent int, total uint64, cancel func()) (*CancellationInjector, error) {
	if !slices.Contains(DefaultContract().CancelStages, stage) {
		return nil, fmt.Errorf("unsupported cancellation stage %q", stage)
	}
	if percent <= 0 || percent >= 100 {
		return nil, fmt.Errorf("cancellation percent must be between 1 and 99")
	}
	if total == 0 {
		return nil, fmt.Errorf("cancellation total must be positive")
	}
	if cancel == nil {
		return nil, fmt.Errorf("cancellation function is required")
	}
	whole := total / 100 * uint64(percent)
	remainder := (total%100*uint64(percent) + 99) / 100
	return &CancellationInjector{threshold: whole + remainder, cancel: cancel}, nil
}

// Tick cancels the stage when completed reaches the configured threshold.
func (injector *CancellationInjector) Tick(completed uint64) {
	if injector == nil || completed < injector.threshold {
		return
	}
	injector.once.Do(func() {
		injector.completed.Store(completed)
		injector.cancel()
	})
}

func (injector *CancellationInjector) completedAtInjection() uint64 {
	if injector == nil {
		return 0
	}
	return injector.completed.Load()
}
