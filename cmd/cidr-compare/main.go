package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/buildinfo"
	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

// Build metadata, stamped at link time by the Makefile's LDFLAGS. Must be
// declared in package main — see the note in cmd/port-scan/main.go.
var (
	version   string
	buildTime string
	commit    string
)

func main() {
	if err := runMain(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

func runMain(args []string, stdout io.Writer, stderr io.Writer) error {
	if buildinfo.IsVersionRequest(args) {
		fmt.Fprint(stdout, buildinfo.Resolve("cidr-compare", version, buildTime, commit).String())
		return nil
	}

	fs := flag.NewFlagSet("cidr-compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [flags]\n", os.Args[0])
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\n  version | --version | -version\n"+
			"\tPrint version, commit and build time (must be the first argument).\n")
	}

	denyFile := fs.String("deny-file", "", "Path to deny CSV file (or CIDR_COMPARE_DENY_FILE)")
	openFile := fs.String("open-file", "", "Path to open CSV file (or CIDR_COMPARE_OPEN_FILE)")

	// Check env vars if flags not set
	if *denyFile == "" {
		*denyFile = os.Getenv("CIDR_COMPARE_DENY_FILE")
	}
	if *openFile == "" {
		*openFile = os.Getenv("CIDR_COMPARE_OPEN_FILE")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *denyFile == "" || *openFile == "" {
		fs.Usage()
		return errors.New("missing required flags")
	}

	// Load deny file into interval tree
	tree := &cidrutil.IntervalTree{}
	denyF, err := os.Open(*denyFile)
	if err != nil {
		return fmt.Errorf("failed to open deny file: %w", err)
	}
	defer denyF.Close()

	denyReader := cidrutil.NewDenyCSVReader(denyF)
	denyEntries, err := denyReader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read deny file: %w", err)
	}
	for _, entry := range denyEntries {
		tree.Insert(entry)
	}

	// Stream open file and query
	openF, err := os.Open(*openFile)
	if err != nil {
		return fmt.Errorf("failed to open open file: %w", err)
	}
	defer openF.Close()

	fmt.Fprintln(stdout, "deny_cidr,open_cidr")

	openReader := cidrutil.NewOpenCSVReader(openF)
	openEntries, err := openReader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read open file: %w", err)
	}
	for _, entry := range openEntries {
		matches := tree.Query(entry)
		for _, deny := range matches {
			fmt.Fprintf(stdout, "%s,%s\n", deny.Network, entry.Network)
		}
	}

	return nil
}
