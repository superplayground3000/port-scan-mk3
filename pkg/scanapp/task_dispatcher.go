package scanapp

import (
	"context"
	"fmt"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

type dispatchPolicy struct {
	delay    time.Duration
	observer dispatchObserver
}

func dispatchPolicyFromConfig(cfg config.Config) dispatchPolicy {
	return dispatchPolicy{
		delay:    cfg.Delay,
		observer: noopDispatchObserver{},
	}
}

// dispatchTasks iterates over runtimes and dispatches scan tasks through taskCh.
// It enforces rate limiting via bucket acquisition and pressure control via gate signaling.
//
// For bucket and gate wait events, the actual target IP and port are not yet determined
// (they are derived from the task index after gate release). Per constitution observability
// requirements, ch.CIDR is used as the target field and 0 is used as the port field,
// with the note that actual target/port are determined post-gate-release.
func dispatchTasks(ctx context.Context, policy dispatchPolicy, ctrl *speedctrl.Controller, logger *scanLogger, runtimes []*chunkRuntime, taskCh chan<- scanTask) (err error) {
	obs := policy.observer
	if obs == nil {
		obs = noopDispatchObserver{}
	}

	// Recover from panics in the dispatcher goroutine
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("task dispatcher panic: %v", r)
			logger.errorf("%v", err)
		}
	}()

	for idx := range runtimes {
		rt := runtimes[idx]
		ch := rt.state
		snap := rt.tracker.Snapshot()
		// Index at or past total — tracker is idle; advance to signal no more work.
		if snap.NextIndex >= snap.TotalCount {
			rt.tracker.AdvanceNextIndex(snap.NextIndex)
			continue
		}
		// Active scan — advance index and transition tracker to "scanning" state.
		rt.tracker.AdvanceNextIndex(snap.NextIndex)
		for i := snap.NextIndex; i < snap.TotalCount; i++ {
			obs.OnBucketWaitStart(ch.CIDR, i)
			// Note: target/port use ch.CIDR and 0 because actual target/port are not yet
			// determined at bucket wait; they are derived from index after gate release.
			logger.eventf(LogEventBucketWaitStart, ch.CIDR, 0, LogEventBucketWaitStart, LogEventNone, nil)
			if err := rt.bkt.Acquire(ctx); err != nil {
				logger.eventf(LogEventBucketAcquireError, ch.CIDR, 0, LogEventBucketAcquireError, err.Error(), nil)
				return err
			}
			obs.OnBucketAcquired(ch.CIDR, i)
			logger.eventf(LogEventBucketAcquired, ch.CIDR, 0, LogEventBucketAcquired, LogEventNone, nil)

			obs.OnGateWaitStart(ch.CIDR, i)
			logger.eventf(LogEventGateWaitStart, ch.CIDR, 0, LogEventGateWaitStart, LogEventNone, nil)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ctrl.Gate():
			}
			obs.OnGateReleased(ch.CIDR, i)
			logger.eventf(LogEventGateReleased, ch.CIDR, 0, LogEventGateReleased, LogEventNone, nil)

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
			if policy.delay > 0 {
				time.Sleep(policy.delay)
			}
		}
	}
	return nil
}
