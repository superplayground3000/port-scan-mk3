package scanapp

import (
	"bytes"
	"testing"
	"time"
)

func TestDashboardRenderer_RenderIncludesRefreshAndFixedSections(t *testing.T) {
	snap := dashboardSnapshot{
		ScannedTasks:         3,
		TotalTasks:           8,
		Percent:              37.5,
		PressurePercent:      73,
		CurrentCIDR:          "10.0.0.0/24",
		BucketStatus:         "acquired",
		ControllerStatus:     "PAUSED(API)",
		DispatchPerSecond:    1.2,
		ResultsPerSecond:     0.8,
		APIHealthText:        "fail streak 2",
		LastPressureUpdateAt: time.Date(2026, 3, 18, 10, 4, 5, 0, time.UTC),
		APISources: []dashboardAPISourceSnapshot{
			{Name: "src1", PressurePercent: 73, HasPressure: true, HealthText: "ok", LastUpdatedAt: time.Date(2026, 3, 18, 10, 4, 5, 0, time.UTC)},
			{Name: "src2", PressurePercent: 0, HealthText: "fail streak 2", LastUpdatedAt: time.Date(2026, 3, 18, 10, 4, 7, 0, time.UTC)},
		},
	}

	var buf bytes.Buffer
	if err := (dashboardRenderer{}).Render(&buf, snap); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	want := "\x1b[2J\x1b[H" +
		"Progress: 3/8 (37.5%)\n" +
		"Current CIDR: 10.0.0.0/24\n" +
		"Bucket: acquired\n" +
		"Dispatch/s: 1.20\n" +
		"Results/s: 0.80\n" +
		"Controller: PAUSED(API)\n" +
		"API Pressure: 73%\n" +
		"Last Update: 2026-03-18T10:04:05Z\n" +
		"Health: fail streak 2\n" +
		"API Sources: src1=73% ok | src2=- fail streak 2\n"

	if got := buf.String(); got != want {
		t.Fatalf("unexpected render output\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestDashboardRenderer_RenderUsesFallbacksForEmptyFields(t *testing.T) {
	snap := dashboardSnapshot{}

	var buf bytes.Buffer
	if err := (dashboardRenderer{}).Render(&buf, snap); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	want := "\x1b[2J\x1b[H" +
		"Progress: 0/0 (0.0%)\n" +
		"Current CIDR: -\n" +
		"Bucket: -\n" +
		"Dispatch/s: 0.00\n" +
		"Results/s: 0.00\n" +
		"Controller: -\n" +
		"API Pressure: 0%\n" +
		"Last Update: -\n" +
		"Health: -\n" +
		"API Sources: -\n"

	if got := buf.String(); got != want {
		t.Fatalf("unexpected render output\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestDashboardRenderer_RenderPrefersLatestFailureTimestamp(t *testing.T) {
	snap := dashboardSnapshot{
		APIHealthText:         "fail streak 4",
		LastPressureUpdateAt:  time.Date(2026, 3, 18, 10, 4, 5, 0, time.UTC),
		LastPressureFailureAt: time.Date(2026, 3, 18, 10, 4, 7, 0, time.UTC),
	}

	var buf bytes.Buffer
	if err := (dashboardRenderer{}).Render(&buf, snap); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if got := buf.String(); !bytes.Contains([]byte(got), []byte("Last Update: 2026-03-18T10:04:07Z\n")) {
		t.Fatalf("expected failure timestamp to drive last update, got %q", got)
	}
}
