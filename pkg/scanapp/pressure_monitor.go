package scanapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

func fetchPressure(client *http.Client, url string) (float64, error) {
	resp, err := client.Get(url)
	if err != nil {
		return 0.0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return 0.0, fmt.Errorf("pressure api status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0.0, err
	}
	raw, ok := body["pressure"]
	if !ok {
		return 0.0, fmt.Errorf("pressure field missing")
	}
	n, err := parsePressureValue(raw)
	if err != nil {
		return 0.0, err
	}
	return n, nil
}
