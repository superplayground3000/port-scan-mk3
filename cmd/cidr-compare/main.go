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
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			os.Exit(1)
		}
		os.Exit(1)
	}
}

func runMain(args []string, stdout io.Writer, stderr io.Writer) error {
	if buildinfo.IsVersionRequest(args) {
		if _, err := fmt.Fprint(stdout, buildinfo.Resolve("cidr-compare", version, buildTime, commit).String()); err != nil {
			return fmt.Errorf("write version: %w", err)
		}
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

	denyF, err := os.Open(*denyFile)
	if err != nil {
		return fmt.Errorf("open deny CSV %s: %w", *denyFile, err)
	}
	denyReader := cidrutil.NewDenyCSVReader(denyF)
	denyEntries, err := denyReader.ReadAll()
	closeErr := denyF.Close()
	if err != nil {
		return fmt.Errorf("read deny CSV %s: %w", *denyFile, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close deny CSV %s: %w", *denyFile, closeErr)
	}

	openF, err := os.Open(*openFile)
	if err != nil {
		return fmt.Errorf("open open CSV %s: %w", *openFile, err)
	}
	openReader := cidrutil.NewOpenCSVReader(openF)
	openEntries, err := openReader.ReadAll()
	closeErr = openF.Close()
	if err != nil {
		return fmt.Errorf("read open CSV %s: %w", *openFile, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close open CSV %s: %w", *openFile, closeErr)
	}

	tree := &cidrutil.IntervalTree{}
	for _, entry := range denyEntries {
		tree.Insert(entry)
	}

	if _, err := fmt.Fprintln(stdout, "deny_cidr,open_cidr"); err != nil {
		return fmt.Errorf("write output header: %w", err)
	}
	for _, entry := range openEntries {
		matches := tree.Query(entry)
		for _, deny := range matches {
			if _, err := fmt.Fprintf(stdout, "%s,%s\n", deny.Network, entry.Network); err != nil {
				return fmt.Errorf("write comparison result: %w", err)
			}
		}
	}

	return nil
}
