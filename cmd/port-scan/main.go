package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return handleHelpCommand(stdout)
	}

	switch args[0] {
	case "validate":
		return handleValidateCommand(args[1:], stdout, stderr)
	case "preping":
		return handlePrepingCommand(args[1:], stdout, stderr)
	case "generate-buckets":
		return handleGenerateBucketsCommand(args[1:], stdout, stderr)
	case "scan":
		return handleScanCommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}
