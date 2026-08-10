// PROTOTYPE: This program is throwaway design material. It is not production code.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type design struct {
	name      string
	api       string
	caller    string
	errorSite string
	knowledge string
	migration string
	tradeoff  string
}

var designs = []design{
	{
		name: "1. One generic parse operation with typed tokens",
		api: `type Parser[T any] struct { /* private parser */ }
func Parse[T any](parser Parser[T], args []string) (T, error)

var PrePing Parser[*prePingConfig]
var GenerateBuckets Parser[*bucketConfig]
var Scan Parser[*scanConfig]
var Validate Parser[*validateConfig]`,
		caller: `cfg, err := config.Parse(config.PrePing, args)
err = scanapp.RunPrePing(ctx, cfg, stdout, stderr, opts)`,
		errorSite: "Parse rejects CLI errors. Workflows reject only a nil configuration before side effects.",
		knowledge: "A handler knows one parser token. Consumers define narrow getter interfaces for the hidden result types.",
		migration: "Medium to high. Callers are short, but workflow interfaces and many getters must move together.",
		tradeoff:  "It has one public operation, but the generic-token idiom and hidden result types are unusual Go.",
	},
	{
		name: "2. Generic parser with typed command descriptors",
		api: `type Spec[T any] struct { /* private parser */ }
func Parse[T any](spec Spec[T], args []string) (T, error)

var PrePing Spec[PrePingConfig]
var GenerateBuckets Spec[GenerateBucketsConfig]
var Scan Spec[ScanConfig]
var Validate Spec[ValidateConfig]`,
		caller: `cfg, err := config.Parse(config.PrePing, args)
err = scanapp.RunPrePing(ctx, cfg, stdout, stderr, opts)`,
		errorSite: "The descriptor finalizer rejects CLI errors and incomplete variants. Workflows reject only an uninitialized zero value.",
		knowledge: "A handler knows one descriptor. Maintainers must understand the descriptor framework and its finalizers.",
		migration: "High. The parser framework and all four descriptors must land before callers can move.",
		tradeoff:  "It is easy to extend, but a registry and generic framework add machinery for four stable commands.",
	},
	{
		name: "3. Direct parsers returning opaque command values (recommended)",
		api: `func ParsePrePing(args []string) (PrePingConfig, error)
func ParseGenerateBuckets(args []string) (GenerateBucketsConfig, error)
func ParseScan(args []string) (ScanConfig, error)
func ParseValidate(args []string) (ValidateConfig, error)

func NewPrePing(PrePingValues) (PrePingConfig, error)
// The other commands have equivalent validated constructors.`,
		caller: `cfg, err := config.ParsePrePing(args)
err = scanapp.RunPrePing(ctx, cfg, stdout, stderr, opts)`,
		errorSite: "Each parser or constructor rejects invalid values. Each workflow rejects an uninitialized zero value before side effects.",
		knowledge: "A handler knows only its parser, workflow, and exit-code mapping.",
		migration: "Medium to high. Production callers are simple, but tests must replace Config literals with focused builders.",
		tradeoff:  "Four explicit APIs duplicate some structure. That duplication keeps flags and invariants local to each command.",
	},
}

func main() {
	fmt.Println("Command-specific configuration interface prototype")
	fmt.Println()
	fmt.Println("1  Minimal public API")
	fmt.Println("2  Maximum flexibility")
	fmt.Println("3  Lowest caller knowledge (recommended)")
	fmt.Println("a  Show all")
	fmt.Print("Selection [a]: ")

	selection := "a"
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() && strings.TrimSpace(scanner.Text()) != "" {
		selection = strings.TrimSpace(scanner.Text())
	}

	fmt.Println()
	switch selection {
	case "1", "2", "3":
		show(designs[int(selection[0]-'1')])
	case "a", "A", "all":
		for _, candidate := range designs {
			show(candidate)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown selection %q; use 1, 2, 3, or a\n", selection)
		os.Exit(2)
	}

	fmt.Println("Shared command shapes")
	fmt.Println("  pre-ping:        target input, output, workers, ping timeout, progress")
	fmt.Println("  generate-buckets: target input, ports, blocklist, snapshot, workers, progress")
	fmt.Println("  scan:            resume, output, dial/rate controls, logging, pressure policy")
	fmt.Println("  validate:        target input, optional ports, output format")
	fmt.Println()
	fmt.Println("Pressure policy is one validated variant: disabled, simple endpoint, or OAuth endpoints.")
	fmt.Println("Go still permits a zero value. Opaque fields prevent partial construction, and workflows reject zero before side effects.")
}

func show(candidate design) {
	fmt.Println(candidate.name)
	fmt.Println(strings.Repeat("=", len(candidate.name)))
	fmt.Println("Public API:")
	fmt.Println(candidate.api)
	fmt.Println()
	fmt.Println("Typical caller:")
	fmt.Println(candidate.caller)
	fmt.Println()
	fmt.Println("Error boundary:", candidate.errorSite)
	fmt.Println("Caller knowledge:", candidate.knowledge)
	fmt.Println("Migration:", candidate.migration)
	fmt.Println("Trade-off:", candidate.tradeoff)
	fmt.Println()
}
