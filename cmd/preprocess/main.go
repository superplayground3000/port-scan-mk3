package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/buildinfo"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocess"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// Build metadata, stamped at link time by the Makefile's LDFLAGS. Must be
// declared in package main — see the note in cmd/port-scan/main.go.
var (
	version   string
	buildTime string
	commit    string
)

func runMain(args []string, stdout, stderr io.Writer, now time.Time) error {
	if buildinfo.IsVersionRequest(args) {
		fmt.Fprint(stdout, buildinfo.Resolve("preprocess", version, buildTime, commit).String())
		return nil
	}

	fs := flag.NewFlagSet("preprocess", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: preprocess [flags]\n\n")
		fmt.Fprintf(stderr, "Filters a rich CSV by removing targets in closed CIDRs.\n\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\n  version | --version | -version\n"+
			"\tPrint version, commit and build time (must be the first argument).\n")
	}

	input := fs.String("input", "", "Path to rich CSV [required]")
	cleanedCIDRs := fs.String("cleaned-cidrs", "", "Path to cleaned CIDRs CSV (fab,segment,status) [required]")
	fabName := fs.String("fab-name", "", "Data center / fabric name [required]")
	outputDir := fs.String("output-dir", "", "Base output directory [required]")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *input == "" || *cleanedCIDRs == "" || *fabName == "" || *outputDir == "" {
		fs.Usage()
		return errors.New("all flags --input, --cleaned-cidrs, --fab-name, --output-dir are required")
	}

	// The fab name becomes a directory under --output-dir, so it is checked
	// before any input is opened: an unsafe name must fail immediately rather
	// than late in os.MkdirAll, or worse, place output outside the base
	// directory.
	validatedFabName, err := preprocess.ParseFabName(*fabName)
	if err != nil {
		return fmt.Errorf("--fab-name: %w", err)
	}

	// Load closed CIDRs for this fab.
	cidrsFile, err := os.Open(*cleanedCIDRs)
	if err != nil {
		return fmt.Errorf("opening cleaned CIDRs: %w", err)
	}
	defer cidrsFile.Close()
	closedTree, err := preprocess.LoadCleanedCIDRs(cidrsFile, validatedFabName.String(), stderr)
	if err != nil {
		return fmt.Errorf("loading cleaned CIDRs: %w", err)
	}

	filter := preprocess.NewFilter(closedTree)

	// Open input rich CSV.
	inFile, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("opening input: %w", err)
	}
	defer inFile.Close()

	cr := csv.NewReader(inFile)
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("reading input header: %w", err)
	}

	// Find dst_network_segment column.
	segIdx := -1
	for i, col := range header {
		if strings.TrimSpace(strings.ToLower(col)) == strings.ToLower(preprocesscfg.ColDstNetworkSegment) {
			segIdx = i
			break
		}
	}
	if segIdx < 0 {
		return fmt.Errorf("input CSV missing required column %q", preprocesscfg.ColDstNetworkSegment)
	}

	// Create output.
	outPath, err := preprocess.OutputPathForFabName(*outputDir, validatedFabName, now)
	if err != nil {
		return fmt.Errorf("creating output path: %w", err)
	}
	cw, outFile, err := preprocess.CreateOutputWriter(outPath)
	if err != nil {
		return err
	}
	defer func() {
		// Close is checked explicitly after Flush; defer is a safety net.
		outFile.Close()
	}()

	// Write header (pass through input header).
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("writing output header: %w", err)
	}

	total, kept, dropped := 0, 0, 0
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading input row %d: %w", total+2, err)
		}
		total++

		if len(row) <= segIdx {
			fmt.Fprintf(stderr, "row %d: too few columns, skipping\n", total+1)
			dropped++
			continue
		}

		seg := strings.TrimSpace(row[segIdx])
		ok, err := filter.Keep(seg)
		if err != nil {
			fmt.Fprintf(stderr, "row %d: filter error: %v, dropping\n", total+1, err)
			dropped++
			continue
		}

		if ok {
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("writing output row: %w", err)
			}
			kept++
		} else {
			dropped++
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("closing output: %w", err)
	}

	if err := preprocess.PrintSummary(stderr, total, kept, dropped); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}
	fmt.Fprintf(stderr, "Output written to: %s\n", outPath)
	return nil
}

func main() {
	if err := runMain(os.Args[1:], os.Stdout, os.Stderr, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
