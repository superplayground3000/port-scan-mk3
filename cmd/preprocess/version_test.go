package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
)

func TestRunMain_VersionRequest_PrintsBuildInfoToStdout(t *testing.T) {
	for _, arg := range []string{"version", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runMain([]string{arg}, &stdout, &stderr, time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("runMain(%q) returned error %v (stderr: %s)", arg, err, stderr.String())
			}
			testkit.AssertUnstampedVersionReport(t, "preprocess", stdout.String())
			if stderr.Len() != 0 {
				t.Errorf("version request wrote to stderr: %s", stderr.String())
			}
		})
	}
}
