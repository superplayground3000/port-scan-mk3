package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

var (
	denyFile = flag.String("deny-file", "", "Path to deny CSV file (or CIDR_COMPARE_DENY_FILE)")
	openFile = flag.String("open-file", "", "Path to open CSV file (or CIDR_COMPARE_OPEN_FILE)")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	// Check env vars if flags not set
	if *denyFile == "" {
		*denyFile = os.Getenv("CIDR_COMPARE_DENY_FILE")
	}
	if *openFile == "" {
		*openFile = os.Getenv("CIDR_COMPARE_OPEN_FILE")
	}

	if *denyFile == "" || *openFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Load deny file into interval tree
	tree := &cidrutil.IntervalTree{}
	denyF, err := os.Open(*denyFile)
	if err != nil {
		log.Fatalf("failed to open deny file: %v", err)
	}
	defer denyF.Close()

	denyReader := cidrutil.NewDenyCSVReader(denyF)
	denyEntries, err := denyReader.ReadAll()
	if err != nil {
		log.Fatalf("failed to read deny file: %v", err)
	}
	for _, entry := range denyEntries {
		tree.Insert(entry)
	}

	// Stream open file and query
	openF, err := os.Open(*openFile)
	if err != nil {
		log.Fatalf("failed to open open file: %v", err)
	}
	defer openF.Close()

	fmt.Println("deny_cidr,open_cidr")

	openReader := cidrutil.NewOpenCSVReader(openF)
	openEntries, err := openReader.ReadAll()
	if err != nil {
		log.Fatalf("failed to read open file: %v", err)
	}
	for _, entry := range openEntries {
		matches := tree.Query(entry)
		for _, deny := range matches {
			fmt.Printf("%s,%s\n", deny.Network, entry.Network)
		}
	}
}
