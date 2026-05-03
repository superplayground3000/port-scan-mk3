package scanapp

import (
	"testing"
	"time"
)

func TestDashboardState_ProgressUpdates(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(4, func() time.Time { return current })

	state.OnTaskEnqueued("10.0.0.0/24")
	state.OnTaskEnqueued("10.0.0.0/24")
	state.OnResult()

	snap := state.Snapshot()
	if snap.ScannedTasks != 1 {
		t.Fatalf("expected ScannedTasks=1, got %d", snap.ScannedTasks)
	}
	if snap.TotalTasks != 4 {
		t.Fatalf("expected TotalTasks=4, got %d", snap.TotalTasks)
	}
	if snap.Percent != 25 {
		t.Fatalf("expected Percent=25, got %v", snap.Percent)
	}
}

func TestDashboardState_CurrentCIDRAndBucketStatusTransitions(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(2, func() time.Time { return current })

	state.OnTaskEnqueued("10.0.0.0/24")
	snap := state.Snapshot()
	if snap.CurrentCIDR != "10.0.0.0/24" {
		t.Fatalf("expected CurrentCIDR to follow task enqueue, got %q", snap.CurrentCIDR)
	}
	if snap.BucketStatus != "" {
		t.Fatalf("expected empty BucketStatus before updates, got %q", snap.BucketStatus)
	}

	state.OnBucketStatus("10.0.0.0/24", "waiting")
	snap = state.Snapshot()
	if snap.CurrentCIDR != "10.0.0.0/24" || snap.BucketStatus != "waiting" {
		t.Fatalf("unexpected first bucket snapshot: %#v", snap)
	}

	state.OnBucketStatus("10.0.1.0/24", "acquired")
	snap = state.Snapshot()
	if snap.CurrentCIDR != "10.0.1.0/24" || snap.BucketStatus != "acquired" {
		t.Fatalf("unexpected second bucket snapshot: %#v", snap)
	}
}

func TestDashboardState_ControllerStatusMapping(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(1, func() time.Time { return current })

	cases := []struct {
		name         string
		manualPaused bool
		apiPaused    bool
		want         string
	}{
		{name: "running", want: "RUNNING"},
		{name: "api paused", apiPaused: true, want: "PAUSED(API)"},
		{name: "manual paused", manualPaused: true, want: "PAUSED(MANUAL)"},
		{name: "api and manual paused", manualPaused: true, apiPaused: true, want: "PAUSED(API+MANUAL)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state.OnController(tc.manualPaused, tc.apiPaused)
			snap := state.Snapshot()
			if snap.ControllerStatus != tc.want {
				t.Fatalf("expected ControllerStatus=%q, got %q", tc.want, snap.ControllerStatus)
			}
		})
	}
}

func TestDashboardState_SlidingWindowSpeeds(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(10, func() time.Time { return current })

	current = current.Add(-6 * time.Second)
	state.OnTaskEnqueued("10.0.0.0/24")

	current = current.Add(2 * time.Second)
	state.OnTaskEnqueued("10.0.0.0/24")
	state.OnResult()

	current = current.Add(3 * time.Second)
	state.OnTaskEnqueued("10.0.0.0/24")
	state.OnResult()

	current = current.Add(1 * time.Second)
	snap := state.Snapshot()
	if snap.DispatchPerSecond != 0.4 {
		t.Fatalf("expected DispatchPerSecond=0.4, got %v", snap.DispatchPerSecond)
	}
	if snap.ResultsPerSecond != 0.4 {
		t.Fatalf("expected ResultsPerSecond=0.4, got %v", snap.ResultsPerSecond)
	}
}

func TestDashboardState_APIHealthTextAndTimestamps(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(1, func() time.Time { return current })

	okAt := current.Add(2 * time.Second)
	failAt := current.Add(7 * time.Second)

	state.OnPressureSample(81, okAt)
	snap := state.Snapshot()
	if snap.PressurePercent != 81 {
		t.Fatalf("expected PressurePercent=81 after sample, got %d", snap.PressurePercent)
	}
	if snap.APIHealthText != "ok" {
		t.Fatalf("expected APIHealthText=ok after sample, got %q", snap.APIHealthText)
	}
	if !snap.LastPressureUpdateAt.Equal(okAt) {
		t.Fatalf("expected LastPressureUpdateAt=%v, got %v", okAt, snap.LastPressureUpdateAt)
	}
	if !snap.LastPressureFailureAt.IsZero() {
		t.Fatalf("expected zero LastPressureFailureAt, got %v", snap.LastPressureFailureAt)
	}

	state.OnPressureFailure(3, failAt)
	snap = state.Snapshot()
	if snap.PressurePercent != 81 {
		t.Fatalf("expected PressurePercent to retain last sample value, got %d", snap.PressurePercent)
	}
	if snap.APIHealthText != "fail streak 3" {
		t.Fatalf("expected APIHealthText=fail streak 3 after failure, got %q", snap.APIHealthText)
	}
	if !snap.LastPressureUpdateAt.Equal(okAt) {
		t.Fatalf("expected LastPressureUpdateAt to retain last success timestamp, got %v", snap.LastPressureUpdateAt)
	}
	if !snap.LastPressureFailureAt.Equal(failAt) {
		t.Fatalf("expected LastPressureFailureAt=%v, got %v", failAt, snap.LastPressureFailureAt)
	}
}

func TestDashboardState_APISourceHealthTracksEachSource(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(1, func() time.Time { return current })

	src1At := current.Add(2 * time.Second)
	src2FailAt := current.Add(3 * time.Second)
	src2OKAt := current.Add(4 * time.Second)

	state.OnPressureSourceSample("src1", 61, src1At)
	state.OnPressureSourceFailure("src2", src2FailAt)

	snap := state.Snapshot()
	if len(snap.APISources) != 2 {
		t.Fatalf("expected two source snapshots, got %#v", snap.APISources)
	}
	if got := snap.APISources[0]; got.Name != "src1" || got.PressurePercent != 61 || got.HealthText != "ok" || !got.LastUpdatedAt.Equal(src1At) {
		t.Fatalf("unexpected src1 snapshot: %#v", got)
	}
	if got := snap.APISources[1]; got.Name != "src2" || got.PressurePercent != 0 || got.HealthText != "fail streak 1" || !got.LastUpdatedAt.Equal(src2FailAt) {
		t.Fatalf("unexpected src2 failure snapshot: %#v", got)
	}

	state.OnPressureSourceSample("src2", 42, src2OKAt)
	snap = state.Snapshot()
	if got := snap.APISources[1]; got.Name != "src2" || got.PressurePercent != 42 || got.HealthText != "ok" || !got.LastUpdatedAt.Equal(src2OKAt) {
		t.Fatalf("expected src2 to recover, got %#v", got)
	}
}

func TestDashboardState_SetScannedTasks_ClampsWithinBounds(t *testing.T) {
	current := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := newDashboardState(4, func() time.Time { return current })

	state.SetScannedTasks(-3)
	if snap := state.Snapshot(); snap.ScannedTasks != 0 || snap.Percent != 0 {
		t.Fatalf("expected negative seed to clamp to zero, got %#v", snap)
	}

	state.SetScannedTasks(9)
	if snap := state.Snapshot(); snap.ScannedTasks != 4 || snap.Percent != 100 {
		t.Fatalf("expected oversized seed to clamp to total, got %#v", snap)
	}
}
