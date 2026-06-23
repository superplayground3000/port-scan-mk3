package scanapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
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

func pollPressureAPI(ctx context.Context, cfg config.Config, opts RunOptions, ctrl *speedctrl.Controller, logger *scanLogger, errCh chan<- error) {
	interval := cfg.PressureInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	threshold := opts.PressureLimit
	if threshold <= 0 {
		threshold = defaultPressureLimit
	}
	thresholdValue := float64(threshold)

	// Use PressureFetcher if provided, otherwise create SimplePressureFetcher for backward compatibility
	var fetcher PressureFetcher
	if opts.PressureFetcher != nil {
		fetcher = opts.PressureFetcher
	} else {
		client := opts.PressureHTTP
		if client == nil {
			client = &http.Client{Timeout: 2 * time.Second}
		}
		fetcher = NewSimplePressureFetcher(cfg.PressureAPI, client)
	}

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
			pressure, err := fetchPressureWithSourceTelemetry(ctx, fetcher, pressureObserver, sampledAt)
			if err != nil {
				consecutiveFailures++
				if pressureObserver != nil {
					pressureObserver.OnPressureFailure(consecutiveFailures, sampledAt)
				}
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
			logger.infof("[API] pressure api status=ok pressure=%.1f%% threshold=%.1f", pressure, thresholdValue)

			if pressureObserver != nil {
				pressureObserver.OnPressureSample(int(pressure), sampledAt)
			}
			paused := pressure >= thresholdValue
			ctrl.SetAPIPaused(paused)
			if paused != prevPaused {
				if paused {
					logger.infof("[API] router pressure overload — scan automatically paused pressure=%.1f threshold=%.1f", pressure, thresholdValue)
				} else {
					logger.infof("[API] router pressure recovered — scan automatically resumed pressure=%.1f threshold=%.1f", pressure, thresholdValue)
				}
				prevPaused = paused
			}
		}
	}
}

func fetchPressureWithSourceTelemetry(ctx context.Context, fetcher PressureFetcher, observer pressureTelemetryObserver, sampledAt time.Time) (float64, error) {
	sourceFetcher, ok := fetcher.(pressureSourceStatusFetcher)
	if !ok {
		return fetcher.Fetch(ctx)
	}

	pressure, sources, err := sourceFetcher.FetchWithSourceStatuses(ctx)
	reportPressureSourceResults(observer, sources, sampledAt)
	return pressure, err
}

func reportPressureSourceResults(observer pressureTelemetryObserver, sources []PressureSourceResult, sampledAt time.Time) {
	sourceObserver, ok := observer.(pressureSourceTelemetryObserver)
	if !ok {
		return
	}
	for _, source := range sources {
		if source.Err != nil {
			sourceObserver.OnPressureSourceFailure(source.Name, sampledAt)
			continue
		}
		sourceObserver.OnPressureSourceSample(source.Name, int(source.Pressure), sampledAt)
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
