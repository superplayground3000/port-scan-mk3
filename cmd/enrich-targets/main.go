package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/enrich"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func runMain(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("enrich-targets", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: enrich-targets [flags]\n\n")
		fmt.Fprintf(stderr, "Enriches a minimal host,port CSV into a full rich CSV.\n\n")
		fs.PrintDefaults()
	}

	input := fs.String("input", "", "Path to opened targets CSV (host,port) [required]")
	cidrList := fs.String("cidr-list", "", "Path to CIDR reference CSV [required]")
	serviceMap := fs.String("service-map", "", "Path to port-to-service-label CSV [required]")
	output := fs.String("output", "", "Path to write enriched rich CSV [required]")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *input == "" || *cidrList == "" || *serviceMap == "" || *output == "" {
		fs.Usage()
		return errors.New("all flags --input, --cidr-list, --service-map, --output are required")
	}

	// Load CIDR reference list.
	cidrFile, err := os.Open(*cidrList)
	if err != nil {
		return fmt.Errorf("opening CIDR list: %w", err)
	}
	defer cidrFile.Close()
	tree, err := enrich.LoadCIDRList(cidrFile)
	if err != nil {
		return fmt.Errorf("loading CIDR list: %w", err)
	}

	// Load service map.
	svcFile, err := os.Open(*serviceMap)
	if err != nil {
		return fmt.Errorf("opening service map: %w", err)
	}
	defer svcFile.Close()
	svcMap, err := enrich.LoadServiceMap(svcFile)
	if err != nil {
		return fmt.Errorf("loading service map: %w", err)
	}

	// Open input CSV.
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

	hostIdx, portIdx := -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case strings.ToLower(preprocesscfg.ColHost):
			hostIdx = i
		case strings.ToLower(preprocesscfg.ColPortInput):
			portIdx = i
		}
	}
	if hostIdx < 0 || portIdx < 0 {
		return fmt.Errorf("input CSV missing required columns %q and %q",
			preprocesscfg.ColHost, preprocesscfg.ColPortInput)
	}

	// Create output file.
	outFile, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("creating output: %w", err)
	}
	defer outFile.Close()
	cw := csv.NewWriter(outFile)

	// Write header.
	if err := cw.Write(preprocesscfg.RichHeader()); err != nil {
		return fmt.Errorf("writing output header: %w", err)
	}

	enricher := enrich.NewEnricher(tree, svcMap)
	rowNum := 1
	enriched := 0

	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading input row %d: %w", rowNum+1, err)
		}
		rowNum++

		if len(row) <= hostIdx || len(row) <= portIdx {
			fmt.Fprintf(stderr, "row %d: too few columns, skipping\n", rowNum)
			continue
		}

		host := strings.TrimSpace(row[hostIdx])
		port, err := strconv.Atoi(strings.TrimSpace(row[portIdx]))
		if err != nil {
			fmt.Fprintf(stderr, "row %d: invalid port %q, skipping\n", rowNum, row[portIdx])
			continue
		}

		rich, err := enricher.Enrich(host, port)
		if err != nil {
			fmt.Fprintf(stderr, "row %d: enrichment failed: %v, skipping\n", rowNum, err)
			continue
		}

		if err := cw.Write(rich.ToSlice()); err != nil {
			return fmt.Errorf("writing output row %d: %w", rowNum, err)
		}
		enriched++
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}

	fmt.Fprintf(stderr, "Enriched %d rows from %d input rows\n", enriched, rowNum-1)
	return nil
}

func main() {
	if err := runMain(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
