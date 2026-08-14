package scanapp

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

func startManualPauseMonitor(ctx context.Context, ctrl *speedctrl.Controller, logger *scanLogger) {
	go func() {
		prev := ctrl.ManualPaused()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				curr := ctrl.ManualPaused()
				if curr != prev {
					if curr {
						logger.infof("[Manual] received keyboard command — scan manually paused")
					} else {
						logger.infof("[Manual] scan manually resumed")
					}
					prev = curr
				}
			}
		}
	}()
}

func pollPressureAPI(ctx context.Context, interval time.Duration, source PressureSource, opts RunOptions, ctrl *speedctrl.Controller, logger *scanLogger, errCh chan<- error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	threshold := opts.PressureLimit
	if threshold <= 0 {
		threshold = defaultPressureLimit
	}
	thresholdValue := float64(threshold)

	var consecutiveFailures int
	var prevPaused bool
	pressureObserver := opts.pressureObserver
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sampledAt := time.Now()
			sample, err := source.Sample(ctx)
			if ctx.Err() != nil {
				return
			}
			sample, validationErr := validatePressureSample(sample)
			if err == nil {
				err = validationErr
			}
			pressureValue := sample.Maximum
			failureCount := 0
			if err != nil {
				failureCount = consecutiveFailures + 1
			}
			if pressureObserver != nil {
				pressureObserver.OnPressurePoll(pressurePoll{
					sample:       sample,
					err:          err,
					failureCount: failureCount,
					sampledAt:    sampledAt,
				})
			}
			if err != nil {
				consecutiveFailures = failureCount
				if consecutiveFailures <= 2 {
					logger.errorf("pressure api request failed (%d/3): %v", consecutiveFailures, err)
					continue
				}
				select {
				case errCh <- fmt.Errorf("pressure api failed 3 times: %w", err):
				default:
				}
				return
			}
			consecutiveFailures = 0
			logger.infof("[API] pressure api status=ok pressure=%.1f%% threshold=%.1f", pressureValue, thresholdValue)

			paused := pressureValue >= thresholdValue
			ctrl.SetAPIPaused(paused)
			if paused != prevPaused {
				if paused {
					logger.infof("[API] router pressure overload — scan automatically paused pressure=%.1f threshold=%.1f", pressureValue, thresholdValue)
				} else {
					logger.infof("[API] router pressure recovered — scan automatically resumed pressure=%.1f threshold=%.1f", pressureValue, thresholdValue)
				}
				prevPaused = paused
			}
		}
	}
}

func validatePressureSample(sample pressure.Sample) (pressure.Sample, error) {
	var firstError error
	for index := range sample.Sources {
		source := &sample.Sources[index]
		if source.Err != nil {
			continue
		}
		if err := nonFinitePressureError("Pressure", source.Pressure); err != nil {
			source.Err = fmt.Errorf("%s: %w", source.Name, err)
			if firstError == nil {
				firstError = source.Err
			}
		}
	}
	if err := nonFinitePressureError("Maximum", sample.Maximum); err != nil && firstError == nil {
		firstError = err
	}
	return sample, firstError
}

func nonFinitePressureError(field string, value float64) error {
	switch {
	case math.IsNaN(value):
		return fmt.Errorf("pressure sample %s is NaN", field)
	case math.IsInf(value, 1):
		return fmt.Errorf("pressure sample %s is positive infinity", field)
	case math.IsInf(value, -1):
		return fmt.Errorf("pressure sample %s is negative infinity", field)
	default:
		return nil
	}
}
