package config

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const defaultProgressInterval = 100

type commonCLIValues struct {
	cidrFile      string
	cidrIPCol     string
	cidrIPCidrCol string
	logLevel      string
	format        string
	quiet         bool
}

func registerCommonFlags(fs *flag.FlagSet, values *commonCLIValues) {
	fs.StringVar(&values.cidrFile, "cidr-file", "", "CIDR CSV path")
	fs.StringVar(&values.cidrIPCol, "cidr-ip-col", "ip", "cidr csv ip column name")
	fs.StringVar(&values.cidrIPCidrCol, "cidr-ip-cidr-col", "ip_cidr", "cidr csv ip_cidr column name")
	fs.StringVar(&values.logLevel, "log-level", "info", "debug|info|error")
	fs.StringVar(&values.format, "format", "human", "human|json")
	fs.BoolVar(&values.quiet, "quiet", false, "suppress the periodic progress output; use -log-level for log verbosity")
}

func (v commonCLIValues) validate() error {
	if v.cidrFile == "" {
		return errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(v.cidrIPCol) == "" || strings.TrimSpace(v.cidrIPCidrCol) == "" {
		return errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if v.format != "human" && v.format != "json" {
		return errors.New("-format must be human or json")
	}
	return nil
}

func parsePressureInterval(raw string) (time.Duration, error) {
	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("-pressure-interval must be duration like 5s or integer seconds: %w", err)
	}
	return interval, nil
}

func parsePressureDataURLs(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var endpoints []string
	for _, endpoint := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(endpoint); trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	if len(endpoints) == 0 {
		return nil, errors.New("-pressure-data-url contains only empty values after trimming")
	}
	return endpoints, nil
}
