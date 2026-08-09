package main

import (
	"fmt"
	"io"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/buildinfo"
)

// Build metadata, stamped at link time by the Makefile's LDFLAGS. These must be
// declared in package main: `go tool link -X` can only write to a variable in
// the binary's own main package, and it silently does nothing when the target
// does not exist — which is how the stamps went unnoticed as dead flags until
// issue #70. tests/release rebuilds this binary with real ldflags and asserts
// the values come back out, so a rename here fails a test instead of going
// quiet.
var (
	version   string
	buildTime string
	commit    string
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
	if buildinfo.IsVersionRequest(args) {
		fmt.Fprint(stdout, buildinfo.Resolve("port-scan", version, buildTime, commit).String())
		return 0
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return handleHelpCommand(stdout)
	}

	switch args[0] {
	case "validate":
		return handleValidateCommand(args[1:], stdout, stderr)
	case "pre-ping":
		return handlePrePingCommand(args[1:], stdout, stderr)
	case "generate-buckets":
		return handleGenerateBucketsCommand(args[1:], stdout, stderr)
	case "scan":
		return handleScanCommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}
