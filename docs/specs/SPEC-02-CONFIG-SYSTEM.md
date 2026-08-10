# SPEC-02: Configuration System Specification

## Overview

`pkg/config` owns CLI parsing, defaults, ranges, and command-specific input
rules. It does not read files or use the network.

The package exposes one opaque configuration type for each command:

```go
type PrePingConfig struct{ state *prePingState }
type GenerateBucketsConfig struct{ state *generateBucketsState }
type ScanConfig struct{ state *scanState }
type ValidateConfig struct{ state *validateState }
```

Each private state contains a verified values structure. A caller cannot create
a partial non-zero configuration.

## 1. Parsers

The CLI uses these command-specific parsers:

```go
func ParsePrePing(args []string) (PrePingConfig, error)
func ParseGenerateBuckets(args []string) (GenerateBucketsConfig, error)
func ParseScan(args []string) (ScanConfig, error)
func ParseValidate(args []string) (ValidateConfig, error)
```

Each parser applies command defaults and input rules. It returns an error for
an unknown flag, a missing required value, or an invalid value.

`ParseValidate` is a compatibility exception. It accepts and verifies every
flag that the legacy `config.Parse` function accepts. It discards values that
the validate workflow does not use.

## 2. Constructors

Tests and non-CLI callers use these constructors:

```go
func NewPrePing(values PrePingValues) (PrePingConfig, error)
func NewGenerateBuckets(values GenerateBucketsValues) (GenerateBucketsConfig, error)
func NewScan(values ScanValues) (ScanConfig, error)
func NewValidate(values ValidateValues) (ValidateConfig, error)
```

Each constructor verifies the fields for its command. A constructor returns an
error for a missing required value, an invalid range, or an invalid variant.

## 3. Resolution

Each opaque type has a `Resolve` method. The method returns the values for one
workflow.

```go
func (PrePingConfig) Resolve() (PrePingValues, error)
func (GenerateBucketsConfig) Resolve() (GenerateBucketsValues, error)
func (ScanConfig) Resolve() (ScanValues, error)
func (ValidateConfig) Resolve() (ValidateValues, error)
```

A zero configuration returns `config.ErrUninitializedConfiguration`. Each
workflow resolves its configuration before file, process, or network work.

## 4. Consumer Interfaces

The consuming package owns each configuration interface. Each interface has
one `Resolve` method.

```go
// package scanapp
type PrePingConfiguration interface {
    Resolve() (config.PrePingValues, error)
}

type GenerateBucketsConfiguration interface {
    Resolve() (config.GenerateBucketsValues, error)
}

type ScanConfiguration interface {
    Resolve() (config.ScanValues, error)
}

// package validate
type Configuration interface {
    Resolve() (config.ValidateValues, error)
}
```

This direction keeps workflow packages independent from concrete
configuration types. A command configuration cannot satisfy the interface of
another workflow.

## 5. Command Values

`PrePingValues` contains the CIDR input, column names, output path, worker
count, ping timeout, and progress interval.

`GenerateBucketsValues` contains the CIDR and port inputs, column names,
blocklist path, snapshot output, worker count, and progress interval.

`ScanValues` contains scan inputs, output paths, concurrency values, time
limits, rate limits, log values, and an opaque pressure policy.

`ValidateValues` contains the CIDR and port inputs, column names, and output
format. The port path can be empty because the workflow detects rich input.

## 6. Pressure Policy

`ScanValues.Pressure` is an opaque `PressurePolicy`. The policy has one of
these verified variants:

- Disabled pressure polling.
- One simple HTTP endpoint and a positive interval.
- OAuth endpoints, credentials, and a positive interval.

The constructors are `PressureDisabled`, `SimplePressure`, and
`AuthenticatedPressure`. Invalid OAuth field combinations cannot enter the
scan workflow.

## 7. Input Rules

All commands require `-cidr-file` and non-empty CIDR column names. The output
format is `human` or `json`.

The pipeline commands apply these additional rules:

- `pre-ping` requires valid workers and a positive ping timeout.
- `generate-buckets` requires `-buckets-out` and valid workers.
- `scan` requires `-resume`, valid workers, rate limits, and a valid pressure
  policy.

The validate parser also verifies the legacy worker, rate, pressure, ping, and
OAuth values. These values do not enter `ValidateValues`.

## 8. Usage

```go
cfg, err := config.ParseValidate(os.Args[1:])
if err != nil {
    return 2
}

result := validate.Inputs(cfg)
```

## 9. Add a Configuration Field

1. Add the field to the values type for one command.
2. Add the flag to that command parser.
3. Add the input rule to the command constructor.
4. Add a parser test and a constructor test before production code changes.
5. Add the field to the consumer workflow only when that workflow uses it.

## 10. Implementation Files

| File | Responsibility |
|------|----------------|
| `pkg/config/pre_ping.go` | Pre-ping values, parser, constructor, and opaque type |
| `pkg/config/generate_buckets.go` | Bucket values, parser, constructor, and opaque type |
| `pkg/config/scan_config.go` | Scan values, parser, constructor, and pressure policy |
| `pkg/config/validate_config.go` | Validate values, parser, constructor, and legacy flag compatibility |
| `pkg/config/bounds.go` | Shared worker and rate limits |
