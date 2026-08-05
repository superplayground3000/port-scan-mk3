package main

import (
	"bytes"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
)

func TestRunMain_VersionRequest_PrintsBuildInfoToStdout(t *testing.T) {
	for _, arg := range []string{"version", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runMain([]string{arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("runMain(%q) exit code = %d, want 0 (stderr: %s)", arg, code, stderr.String())
			}
			testkit.AssertUnstampedVersionReport(t, "port-scan", stdout.String())
			if stderr.Len() != 0 {
				t.Errorf("version request wrote to stderr: %s", stderr.String())
			}
		})
	}
}
