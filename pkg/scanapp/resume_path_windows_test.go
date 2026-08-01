//go:build windows

package scanapp

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

// TestResumePath_WindowsPathShapes guards resumePath against Windows-specific
// path shapes: drive letters, directories containing spaces, and UNC paths. It
// covers both the pass-through sources (explicit option, config value) and the
// derived source, which is the one that actually composes a path via
// filepath.Dir + filepath.Join (resume_path.go).
//
// This is a regression guard, not a bug hunt. Production already uses the
// filepath helpers, so every case below is expected to pass today. The value is
// catching a future change that derives the path by hand -- something like
// outputDir + "/" + defaultResumeStateFile would still look correct on Linux
// while producing a broken path for every shape asserted here.
//
// The expected values are deliberately literal strings rather than filepath.Join
// calls. Building the expectation with the same function production calls makes
// the assertion tautological -- it would pass by construction and could never
// disagree with the code (see docs/MAINTENANCE.md section 6). This file only
// compiles on Windows, so literal backslash expectations are exact and readable.
func TestResumePath_WindowsPathShapes(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.Config
		opts     RunOptions
		expected string
	}{
		{
			name:     "explicit option path wins and is returned verbatim",
			cfg:      config.Config{Resume: `C:\ignored\cfg.json`, Output: `C:\out\scan.csv`},
			opts:     RunOptions{ResumeStatePath: `C:\state\resume.json`},
			expected: `C:\state\resume.json`,
		},
		{
			name:     "config UNC resume path is returned verbatim",
			cfg:      config.Config{Resume: `\\server\share\state\resume.json`, Output: `C:\out\scan.csv`},
			expected: `\\server\share\state\resume.json`,
		},
		{
			name:     "derived from a drive letter output path",
			cfg:      config.Config{Output: `C:\out\scan.csv`},
			expected: `C:\out\resume_state.json`,
		},
		{
			name:     "derived from an output path at the drive root",
			cfg:      config.Config{Output: `C:\scan.csv`},
			expected: `C:\resume_state.json`,
		},
		{
			name:     "derived from an output path containing spaces",
			cfg:      config.Config{Output: `C:\Program Files\portscan\out\scan.csv`},
			expected: `C:\Program Files\portscan\out\resume_state.json`,
		},
		{
			name:     "derived from a UNC output path below the share",
			cfg:      config.Config{Output: `\\server\share\out\scan.csv`},
			expected: `\\server\share\out\resume_state.json`,
		},
		{
			name:     "derived from a UNC output path at the share root",
			cfg:      config.Config{Output: `\\server\share\scan.csv`},
			expected: `\\server\share\resume_state.json`,
		},
		{
			name:     "bare output filename falls back to the default state file",
			cfg:      config.Config{Output: `scan.csv`},
			expected: `resume_state.json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumePath(tt.cfg, tt.opts)
			if got != tt.expected {
				t.Errorf("resumePath(Output=%q, Resume=%q, ResumeStatePath=%q) = %q, want %q",
					tt.cfg.Output, tt.cfg.Resume, tt.opts.ResumeStatePath, got, tt.expected)
			}
		})
	}
}
