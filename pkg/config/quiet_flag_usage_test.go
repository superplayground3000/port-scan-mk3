package config

import (
	"flag"
	"io"
	"strings"
	"testing"
)

// TestRegisterCommonFlags_QuietUsageDescribesProgressOnly pins the -quiet help
// text against the meaning the flag actually has. -quiet suppresses progress and
// per-result console output; -log-level owns log verbosity on its own. See
// issue #157.
func TestRegisterCommonFlags_QuietUsageDescribesProgressOnly(t *testing.T) {
	t.Parallel()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerCommonFlags(fs, &commonCLIValues{})

	quiet := fs.Lookup("quiet")
	if quiet == nil {
		t.Fatal("-quiet flag is not registered")
	}
	usage := strings.ToLower(quiet.Usage)
	if strings.Contains(usage, "pressure") {
		t.Fatalf("-quiet usage still promises pressure API logs, which the logger no longer special-cases; usage = %q", quiet.Usage)
	}
	if !strings.Contains(usage, "progress") {
		t.Fatalf("-quiet usage must say it suppresses progress output; usage = %q", quiet.Usage)
	}
	if !strings.Contains(usage, "-log-level") {
		t.Fatalf("-quiet usage must point the reader at -log-level for log verbosity; usage = %q", quiet.Usage)
	}
}
