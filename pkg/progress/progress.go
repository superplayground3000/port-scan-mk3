// Package progress provides a small progress reporter with no dependencies.
// The reporter writes periodic percentage lines to an io.Writer, which is
// stderr in production.
//
// The long-running pre-scan phases (pre-ping, generate-buckets) use this package
// to give an operator visible feedback over otherwise silent periods. The
// reporter writes one line each time progress advances by a configurable number
// of units. Done always writes a final 100% summary line. All methods are safe
// for concurrent use, so worker goroutines that fan out work can share one
// Reporter.
package progress

import (
	"fmt"
	"io"
	"sync"
)

// defaultInterval is the emit cadence used when a non-positive interval is
// supplied to New. It mirrors the historical progressStep default in
// pkg/scanapp/scan.go so callers see the same cadence they did before.
const defaultInterval = 100

// Reporter advances a counter toward a known total and writes progress lines.
//
// Inc advances the counter by one. Add advances the counter by n. Done writes
// the final "<label>: <total>/<total> (100.0%)" summary. An implementation must
// be safe for concurrent Inc and Add calls.
type Reporter interface {
	Inc()      // advance by 1
	Add(n int) // advance by n
	Done()     // emit final "X/X (100.0%)" summary line
}

// reporter is the default Reporter implementation. It writes count-based
// percentage lines to w and guards its counter and writes with a mutex so
// concurrent callers (e.g. generate-buckets worker goroutines) are race-free.
type reporter struct {
	label    string
	total    int
	interval int

	mu       sync.Mutex
	done     int // units of progress accumulated so far
	lastEmit int // value of done at the last emitted interval line
	w        io.Writer
}

// New returns a Reporter that writes progress lines for label toward total.
// The Reporter writes one line each time progress advances by interval units.
// If interval is zero or less, New uses defaultInterval. The Reporter writes to
// w, which is stderr in production. The returned Reporter is safe for
// concurrent use.
func New(label string, total int, interval int, w io.Writer) Reporter {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &reporter{
		label:    label,
		total:    total,
		interval: interval,
		w:        w,
	}
}

// Inc advances progress by one unit and can write an interval line.
func (r *reporter) Inc() { r.Add(1) }

// Add advances progress by n units. If the counter advanced at least interval
// units since the last line, Add writes one interval line. If n is zero or
// less, Add does nothing.
func (r *reporter) Add(n int) {
	if n <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done += n
	if r.done-r.lastEmit >= r.interval {
		r.lastEmit = r.done
		r.emitLocked(r.done, percent(r.done, r.total))
	}
}

// Done writes the final summary line at 100%. Done writes this line even when
// the last increment did not align to the interval. It is safe to call Done one
// time at completion.
func (r *reporter) Done() {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Always report total/total at exactly 100.0%, even when total == 0 (avoids
	// a divide-by-zero and still gives the operator a clear completion line).
	r.emitLocked(r.total, 100.0)
}

// emitLocked writes one formatted progress line. Callers must hold r.mu.
func (r *reporter) emitLocked(done int, pct float64) {
	_, _ = fmt.Fprintf(r.w, "%s: %d/%d (%.1f%%)\n", r.label, done, r.total, pct)
}

// percent returns done/total as a percentage, guarding against total == 0.
func percent(done, total int) float64 {
	if total <= 0 {
		return 100.0
	}
	return float64(done) / float64(total) * 100.0
}
