package scanapp

import (
	"fmt"
	"sync"
	"time"
)

const dashboardRateWindow = 5 * time.Second

type dashboardSnapshot struct {
	ScannedTasks          int
	TotalTasks            int
	Percent               float64
	PressurePercent       int
	CurrentCIDR           string
	BucketStatus          string
	ControllerStatus      string
	DispatchPerSecond     float64
	ResultsPerSecond      float64
	APIHealthText         string
	LastPressureUpdateAt  time.Time
	LastPressureFailureAt time.Time
	APISources            []dashboardAPISourceSnapshot
}

type dashboardAPISourceSnapshot struct {
	Name            string
	PressurePercent int
	HasPressure     bool
	HealthText      string
	LastUpdatedAt   time.Time
}

type dashboardState struct {
	mu sync.Mutex

	totalTasks   int
	scannedTasks int

	currentCIDR  string
	bucketStatus string

	manualPaused bool
	apiPaused    bool

	pressurePercent int

	dispatchEvents []time.Time
	resultEvents   []time.Time

	apiHealthText         string
	lastPressureUpdateAt  time.Time
	lastPressureFailureAt time.Time

	apiSourceOrder []string
	apiSources     map[string]dashboardAPISourceState

	now func() time.Time
}

type dashboardAPISourceState struct {
	pressurePercent int
	hasPressure     bool
	failStreak      int
	healthText      string
	lastUpdatedAt   time.Time
}

func newDashboardState(total int, now func() time.Time) *dashboardState {
	total = dashboardClampTaskCount(total, total)
	if now == nil {
		now = time.Now
	}
	return &dashboardState{
		totalTasks:    total,
		apiHealthText: "ok",
		apiSources:    make(map[string]dashboardAPISourceState),
		now:           now,
	}
}

func (s *dashboardState) SetScannedTasks(scanned int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scannedTasks = dashboardClampTaskCount(scanned, s.totalTasks)
}

func (s *dashboardState) OnTaskEnqueued(cidr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cidr != "" {
		s.currentCIDR = cidr
	}
	now := s.now()
	s.dispatchEvents = append(s.dispatchEvents, now)
	s.dispatchEvents = pruneDashboardEvents(s.dispatchEvents, now)
}

func (s *dashboardState) OnResult() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scannedTasks++
	now := s.now()
	s.resultEvents = append(s.resultEvents, now)
	s.resultEvents = pruneDashboardEvents(s.resultEvents, now)
}

func (s *dashboardState) OnBucketStatus(cidr, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cidr != "" {
		s.currentCIDR = cidr
	}
	s.bucketStatus = status
}

func (s *dashboardState) OnController(manualPaused, apiPaused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.manualPaused = manualPaused
	s.apiPaused = apiPaused
}

func (s *dashboardState) OnPressureSample(pressure int, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pressurePercent = pressure
	s.apiHealthText = "ok"
	s.lastPressureUpdateAt = t
}

func (s *dashboardState) OnPressureFailure(streak int, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.apiHealthText = fmt.Sprintf("fail streak %d", streak)
	s.lastPressureFailureAt = t
}

func (s *dashboardState) OnPressurePoll(poll pressurePoll) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, source := range poll.sample.Sources {
		if source.Err != nil {
			s.onPressureSourceFailureLocked(source.Name, poll.sampledAt)
			continue
		}
		s.onPressureSourceSampleLocked(source.Name, int(source.Pressure), poll.sampledAt)
	}
	if poll.err != nil {
		s.apiHealthText = fmt.Sprintf("fail streak %d", poll.failureCount)
		s.lastPressureFailureAt = poll.sampledAt
		return
	}
	s.pressurePercent = int(poll.sample.Maximum)
	s.apiHealthText = "ok"
	s.lastPressureUpdateAt = poll.sampledAt
}

func (s *dashboardState) OnPressureSourceSample(source string, pressure int, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPressureSourceSampleLocked(source, pressure, t)
}

func (s *dashboardState) onPressureSourceSampleLocked(source string, pressure int, t time.Time) {
	source = dashboardSourceName(source)
	s.ensureAPISourceLocked(source)
	s.apiSources[source] = dashboardAPISourceState{
		pressurePercent: pressure,
		hasPressure:     true,
		healthText:      "ok",
		lastUpdatedAt:   t,
	}
}

func (s *dashboardState) OnPressureSourceFailure(source string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPressureSourceFailureLocked(source, t)
}

func (s *dashboardState) onPressureSourceFailureLocked(source string, t time.Time) {
	source = dashboardSourceName(source)
	s.ensureAPISourceLocked(source)
	sourceState := s.apiSources[source]
	sourceState.failStreak++
	sourceState.healthText = fmt.Sprintf("fail streak %d", sourceState.failStreak)
	sourceState.lastUpdatedAt = t
	s.apiSources[source] = sourceState
}

func (s *dashboardState) Snapshot() dashboardSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.dispatchEvents = pruneDashboardEvents(s.dispatchEvents, now)
	s.resultEvents = pruneDashboardEvents(s.resultEvents, now)

	return dashboardSnapshot{
		ScannedTasks:          s.scannedTasks,
		TotalTasks:            s.totalTasks,
		Percent:               dashboardPercent(s.scannedTasks, s.totalTasks),
		PressurePercent:       s.pressurePercent,
		CurrentCIDR:           s.currentCIDR,
		BucketStatus:          s.bucketStatus,
		ControllerStatus:      dashboardControllerStatus(s.manualPaused, s.apiPaused),
		DispatchPerSecond:     float64(len(s.dispatchEvents)) / dashboardRateWindow.Seconds(),
		ResultsPerSecond:      float64(len(s.resultEvents)) / dashboardRateWindow.Seconds(),
		APIHealthText:         s.apiHealthText,
		LastPressureUpdateAt:  s.lastPressureUpdateAt,
		LastPressureFailureAt: s.lastPressureFailureAt,
		APISources:            s.apiSourceSnapshotsLocked(),
	}
}

func (s *dashboardState) ensureAPISourceLocked(source string) {
	if _, ok := s.apiSources[source]; ok {
		return
	}
	s.apiSourceOrder = append(s.apiSourceOrder, source)
	s.apiSources[source] = dashboardAPISourceState{healthText: "ok"}
}

func (s *dashboardState) apiSourceSnapshotsLocked() []dashboardAPISourceSnapshot {
	if len(s.apiSourceOrder) == 0 {
		return nil
	}
	sources := make([]dashboardAPISourceSnapshot, 0, len(s.apiSourceOrder))
	for _, name := range s.apiSourceOrder {
		sourceState := s.apiSources[name]
		sources = append(sources, dashboardAPISourceSnapshot{
			Name:            name,
			PressurePercent: sourceState.pressurePercent,
			HasPressure:     sourceState.hasPressure,
			HealthText:      sourceState.healthText,
			LastUpdatedAt:   sourceState.lastUpdatedAt,
		})
	}
	return sources
}

func dashboardSourceName(source string) string {
	if source == "" {
		return "src?"
	}
	return source
}

func pruneDashboardEvents(events []time.Time, now time.Time) []time.Time {
	if len(events) == 0 {
		return events
	}
	cutoff := now.Add(-dashboardRateWindow)
	keep := 0
	for keep < len(events) && events[keep].Before(cutoff) {
		keep++
	}
	if keep == 0 {
		return events
	}
	pruned := make([]time.Time, len(events)-keep)
	copy(pruned, events[keep:])
	return pruned
}

func dashboardPercent(scanned, total int) float64 {
	if total <= 0 {
		return 0
	}
	scanned = dashboardClampTaskCount(scanned, total)
	return (float64(scanned) / float64(total)) * 100
}

func dashboardClampTaskCount(value, total int) int {
	if total < 0 {
		total = 0
	}
	if value < 0 {
		return 0
	}
	if value > total {
		return total
	}
	return value
}

func dashboardControllerStatus(manualPaused, apiPaused bool) string {
	switch {
	case manualPaused && apiPaused:
		return "PAUSED(API+MANUAL)"
	case apiPaused:
		return "PAUSED(API)"
	case manualPaused:
		return "PAUSED(MANUAL)"
	default:
		return "RUNNING"
	}
}
