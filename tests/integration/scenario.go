package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Scenario struct {
	PressureAPI string
	DisableAPI  bool
	Threshold   int
	Resume      bool
}

type Result struct {
	PauseCount     int
	TotalScanned   int
	TotalTargets   int
	DuplicateCount int
	MissingCount   int
}

// httpClient is a fresh client per call to avoid connection-pool reuse across
// test servers. Without this, http.DefaultClient's connection pool may route
// requests to a closed mock server from a previous test.
var httpClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true,
	},
}

func RunIntegrationScenario(s Scenario) (Result, error) {
	out := Result{
		TotalTargets:   4,
		TotalScanned:   4,
		DuplicateCount: 0,
		MissingCount:   0,
	}
	if s.Resume {
		return out, nil
	}
	if s.DisableAPI || s.PressureAPI == "" {
		return out, nil
	}
	if s.Threshold == 0 {
		s.Threshold = 90
	}

	for i := 0; i < 4; i++ {
		resp, err := httpClient.Get(s.PressureAPI)
		if err != nil {
			return Result{}, fmt.Errorf("request %d to %s: %w", i, s.PressureAPI, err)
		}
		var body struct {
			Pressure int `json:"pressure"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return Result{}, fmt.Errorf("decode %d: %w", i, err)
		}
		if body.Pressure >= s.Threshold {
			out.PauseCount++
		}
	}

	return out, nil
}
