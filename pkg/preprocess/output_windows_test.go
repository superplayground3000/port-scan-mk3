//go:build windows

package preprocess

import (
	"testing"
	"time"
)

// TestOutputPath_WindowsPathShapes guards OutputPath against Windows-specific
// path shapes: drive letters, directories containing spaces, and UNC paths.
//
// This is a regression guard, not a bug hunt. Production composes the path with
// filepath.Join (output.go), so every case below is expected to pass today. The
// value is catching a future change that replaces the Join with manual
// concatenation such as dir + "/" + name: that would still look correct on
// Linux while breaking every shape asserted here.
//
// The expected values are deliberately literal strings rather than filepath.Join
// calls. Building the expectation with the same function production calls makes
// the assertion tautological -- it would pass by construction and could never
// disagree with the code (see docs/MAINTENANCE.md section 6). This file only
// compiles on Windows, so literal backslash expectations are exact and readable.
func TestOutputPath_WindowsPathShapes(t *testing.T) {
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		baseDir  string
		fabName  string
		expected string
	}{
		{
			name:     "drive letter directory",
			baseDir:  `C:\data\out`,
			fabName:  "dc-east",
			expected: `C:\data\out\dc-east\20260416T153000Z\input.csv`,
		},
		{
			name:     "drive root",
			baseDir:  `C:\`,
			fabName:  "dc-east",
			expected: `C:\dc-east\20260416T153000Z\input.csv`,
		},
		{
			name:     "trailing separator does not double up",
			baseDir:  `C:\data\out\`,
			fabName:  "dc-east",
			expected: `C:\data\out\dc-east\20260416T153000Z\input.csv`,
		},
		{
			name:     "base directory containing spaces",
			baseDir:  `C:\Program Files\portscan\out`,
			fabName:  "dc-east",
			expected: `C:\Program Files\portscan\out\dc-east\20260416T153000Z\input.csv`,
		},
		{
			name:     "fab name containing spaces",
			baseDir:  `C:\out`,
			fabName:  "dc east 1",
			expected: `C:\out\dc east 1\20260416T153000Z\input.csv`,
		},
		{
			name:     "UNC directory below the share",
			baseDir:  `\\server\share\out`,
			fabName:  "dc-east",
			expected: `\\server\share\out\dc-east\20260416T153000Z\input.csv`,
		},
		{
			name:     "UNC share root keeps its double-backslash prefix",
			baseDir:  `\\server\share`,
			fabName:  "dc-east",
			expected: `\\server\share\dc-east\20260416T153000Z\input.csv`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OutputPath(tt.baseDir, tt.fabName, ts)
			if got != tt.expected {
				t.Errorf("OutputPath(%q, %q, ts) = %q, want %q", tt.baseDir, tt.fabName, got, tt.expected)
			}
		})
	}
}
