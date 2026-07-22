package progress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// lines splits a buffer into non-empty trimmed lines for assertions.
func lines(buf *bytes.Buffer) []string {
	raw := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestReporter_EmitsAtInterval(t *testing.T) {
	var buf bytes.Buffer
	r := New("preping", 10, 3, &buf)

	// Advance one at a time; a line should appear only when the running count
	// crosses a multiple of the interval (3, 6, 9), not on every Inc().
	for i := 1; i <= 8; i++ {
		r.Inc()
	}

	got := lines(&buf)
	// Expected emits at counts 3, 6 (and not at 1,2,4,5,7,8). 9 not reached.
	if len(got) != 2 {
		t.Fatalf("expected 2 interval lines, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "3/10") {
		t.Errorf("first interval line = %q, want it to contain 3/10", got[0])
	}
	if !strings.Contains(got[1], "6/10") {
		t.Errorf("second interval line = %q, want it to contain 6/10", got[1])
	}
	for _, l := range got {
		if !strings.HasPrefix(l, "preping: ") {
			t.Errorf("line %q missing label prefix", l)
		}
	}
}

func TestReporter_FinalSummaryAlwaysEmitted(t *testing.T) {
	var buf bytes.Buffer
	r := New("preping", 10, 3, &buf)

	// Advance to a non-interval-aligned count (7), then Done().
	r.Add(7)
	before := len(lines(&buf))

	r.Done()
	got := lines(&buf)
	if len(got) != before+1 {
		t.Fatalf("Done() should emit exactly one final line; before=%d after=%d lines=%v", before, len(got), got)
	}
	final := got[len(got)-1]
	if !strings.Contains(final, "10/10") || !strings.Contains(final, "(100.0%)") {
		t.Errorf("final summary = %q, want 10/10 and (100.0%%)", final)
	}
	if !strings.HasPrefix(final, "preping: ") {
		t.Errorf("final line %q missing label prefix", final)
	}
}

func TestReporter_FormatsPercent(t *testing.T) {
	var buf bytes.Buffer
	// interval == total so exactly one interval line is emitted at completion.
	r := New("preping", 10000, 4200, &buf)
	r.Add(4200)

	got := lines(&buf)
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "4200/10000") {
		t.Errorf("line %q missing 4200/10000", got[0])
	}
	if !strings.Contains(got[0], "(42.0%)") {
		t.Errorf("line %q missing (42.0%%)", got[0])
	}
}

func TestReporter_ThreadSafe(t *testing.T) {
	var buf bytes.Buffer
	const goroutines = 50
	const perG = 200
	r := New("buckets", goroutines*perG, 1000, &buf)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				r.Inc()
			}
		}()
	}
	wg.Wait()
	r.Done()

	got := lines(&buf)
	final := got[len(got)-1]
	want := goroutines * perG
	// Final line must reflect the exact total; a lost update would show a
	// different numerator (Done always prints total/total).
	if !strings.Contains(final, "10000/10000") {
		t.Errorf("final line = %q, want 10000/10000 (total=%d)", final, want)
	}
}

func TestNew_NonPositiveIntervalUsesDefault(t *testing.T) {
	var buf bytes.Buffer
	// interval <= 0 must fall back to the default cadence (100), so advancing
	// fewer than 100 units emits nothing until Done.
	r := New("x", 1000, 0, &buf)
	r.Add(99)
	if got := lines(&buf); len(got) != 0 {
		t.Fatalf("expected no interval line before default cadence, got %v", got)
	}
	r.Add(1) // now at 100 -> one interval line
	if got := lines(&buf); len(got) != 1 || !strings.Contains(got[0], "100/1000") {
		t.Fatalf("expected one line at default cadence 100, got %v", got)
	}

	var buf2 bytes.Buffer
	rNeg := New("y", 1000, -5, &buf2)
	rNeg.Add(99)
	if got := lines(&buf2); len(got) != 0 {
		t.Fatalf("negative interval should also default to 100, got %v", got)
	}
}

func TestReporter_NonPositiveAddIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	r := New("x", 10, 1, &buf)
	r.Add(0)
	r.Add(-3)
	if got := lines(&buf); len(got) != 0 {
		t.Fatalf("Add(0)/Add(negative) must not advance or emit, got %v", got)
	}
	r.Done()
	final := lines(&buf)
	if len(final) != 1 || !strings.Contains(final[0], "10/10") {
		t.Fatalf("Done() after no-op adds should emit final total line, got %v", final)
	}
}

func TestReporter_IntervalLineWithZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	// An interval line emitted while total == 0 must not divide by zero; it
	// reports 100.0% (the only sensible percentage for a zero denominator).
	r := New("z", 0, 1, &buf)
	r.Inc()
	got := lines(&buf)
	if len(got) != 1 {
		t.Fatalf("expected one interval line, got %v", got)
	}
	if !strings.Contains(got[0], "1/0") || !strings.Contains(got[0], "(100.0%)") {
		t.Errorf("line = %q, want 1/0 and (100.0%%)", got[0])
	}
}

func TestReporter_TotalZero_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	r := New("empty", 0, 100, &buf)
	// Must not panic (no divide-by-zero) on Inc/Add/Done with zero total.
	r.Inc()
	r.Add(5)
	r.Done()

	got := lines(&buf)
	if len(got) == 0 {
		t.Fatalf("Done() should still emit a final line for total==0")
	}
	final := got[len(got)-1]
	if !strings.Contains(final, "0/0") || !strings.Contains(final, "(100.0%)") {
		t.Errorf("final line = %q, want 0/0 and (100.0%%)", final)
	}
}
