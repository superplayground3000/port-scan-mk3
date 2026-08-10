package scanapp

import (
	"context"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

const defaultControllerSyncInterval = 50 * time.Millisecond

type resultTelemetryObserver interface {
	OnResult()
}

type pressurePoll struct {
	sample       pressure.Sample
	err          error
	failureCount int
	sampledAt    time.Time
}

type pressureTelemetryObserver interface {
	OnPressurePoll(pressurePoll)
}

type controllerTelemetryObserver interface {
	OnController(manualPaused, apiPaused bool)
}

type dashboardDispatchObserver struct {
	state *dashboardState
}

func newDashboardDispatchObserver(state *dashboardState) dispatchObserver {
	if state == nil {
		return noopDispatchObserver{}
	}
	return dashboardDispatchObserver{state: state}
}

func (o dashboardDispatchObserver) OnGateWaitStart(cidr string, _ int) {
	o.state.OnBucketStatus(cidr, "waiting_gate")
}

func (o dashboardDispatchObserver) OnGateReleased(string, int) {}

func (o dashboardDispatchObserver) OnBucketWaitStart(cidr string, _ int) {
	o.state.OnBucketStatus(cidr, "waiting_bucket")
}

func (o dashboardDispatchObserver) OnBucketAcquired(string, int) {}

func (o dashboardDispatchObserver) OnTaskEnqueued(cidr string, _ int) {
	o.state.OnBucketStatus(cidr, "enqueued")
	o.state.OnTaskEnqueued(cidr)
}

type pressureTelemetryObservers []pressureTelemetryObserver

func appendPressureTelemetryObservers(observers ...pressureTelemetryObserver) pressureTelemetryObserver {
	filtered := make(pressureTelemetryObservers, 0, len(observers))
	for _, observer := range observers {
		if observer == nil {
			continue
		}
		filtered = append(filtered, observer)
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return filtered
	}
}

func (o pressureTelemetryObservers) OnPressurePoll(poll pressurePoll) {
	for _, observer := range o {
		observer.OnPressurePoll(poll)
	}
}

type controllerTelemetryObservers []controllerTelemetryObserver

func appendControllerTelemetryObservers(observers ...controllerTelemetryObserver) controllerTelemetryObserver {
	filtered := make(controllerTelemetryObservers, 0, len(observers))
	for _, observer := range observers {
		if observer == nil {
			continue
		}
		filtered = append(filtered, observer)
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return filtered
	}
}

func (o controllerTelemetryObservers) OnController(manualPaused, apiPaused bool) {
	for _, observer := range o {
		observer.OnController(manualPaused, apiPaused)
	}
}

func startControllerTelemetrySync(ctx context.Context, ctrl *speedctrl.Controller, interval time.Duration, observer controllerTelemetryObserver) {
	if ctrl == nil || observer == nil {
		return
	}
	if interval <= 0 {
		interval = defaultControllerSyncInterval
	}

	sync := func() {
		observer.OnController(ctrl.ManualPaused(), ctrl.APIPaused())
	}
	sync()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sync()
			}
		}
	}()
}

func controllerTelemetryInterval(refreshInterval time.Duration) time.Duration {
	if refreshInterval > 0 && refreshInterval < defaultControllerSyncInterval {
		return refreshInterval
	}
	return defaultControllerSyncInterval
}
