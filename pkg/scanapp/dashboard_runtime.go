package scanapp

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const defaultDashboardRefreshInterval = 500 * time.Millisecond

type dashboardRenderLoop interface {
	Render(io.Writer, dashboardSnapshot) error
}

type dashboardRuntimeOptions struct {
	refreshInterval time.Duration
	renderer        dashboardRenderLoop
	logger          *scanLogger
}

type dashboardRuntime struct {
	state    *dashboardState
	out      io.Writer
	interval time.Duration
	renderer dashboardRenderLoop
	logger   *scanLogger

	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
}

func newDashboardRuntime(state *dashboardState, out io.Writer, opts dashboardRuntimeOptions) *dashboardRuntime {
	if state == nil {
		state = newDashboardState(0, time.Now)
	}
	if opts.refreshInterval <= 0 {
		opts.refreshInterval = defaultDashboardRefreshInterval
	}
	if opts.renderer == nil {
		opts.renderer = dashboardRenderer{}
	}
	return &dashboardRuntime{
		state:    state,
		out:      out,
		interval: opts.refreshInterval,
		renderer: opts.renderer,
		logger:   opts.logger,
	}
}

func (r *dashboardRuntime) Start(parent context.Context) {
	if r == nil {
		return
	}

	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.done = make(chan struct{})

	go func() {
		defer close(r.done)

		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.renderer.Render(r.out, r.state.Snapshot()); err != nil {
					if r.logger != nil {
						r.logger.errorf("dashboard render failed: %v", err)
					}
					return
				}
			}
		}
	}()
}

func (r *dashboardRuntime) Stop() {
	if r == nil {
		return
	}

	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		if r.done != nil {
			<-r.done
		}
	})
}

func shouldEnableDashboard(format string, stderr io.Writer, opts RunOptions) bool {
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		return false
	}

	detector := opts.dashboardTerminalDetector
	if detector == nil {
		detector = dashboardWriterIsTerminal
	}
	return detector(stderr)
}

func dashboardWriterIsTerminal(w io.Writer) bool {
	type fdWriter interface {
		Fd() uintptr
	}

	terminalWriter, ok := w.(fdWriter)
	if !ok {
		return false
	}
	return term.IsTerminal(int(terminalWriter.Fd()))
}

func dashboardTotalTasks(runtimes []*chunkRuntime) int {
	total := 0
	for _, rt := range runtimes {
		if rt == nil || rt.state == nil {
			continue
		}
		total += rt.state.TotalCount
	}
	return total
}

func dashboardScannedTasks(runtimes []*chunkRuntime) int {
	scanned := 0
	for _, rt := range runtimes {
		if rt == nil || rt.state == nil {
			continue
		}
		scanned += dashboardClampTaskCount(rt.state.ScannedCount, rt.state.TotalCount)
	}
	return scanned
}
