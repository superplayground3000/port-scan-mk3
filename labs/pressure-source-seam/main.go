// PROTOTYPE: This program is throwaway design material. It is not production code.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

type design struct {
	name        string
	code        string
	ownership   string
	client      string
	depth       string
	migration   string
	mainCost    string
	recommended bool
}

type sourceResult struct {
	Name     string
	Pressure float64
	Err      error
}

type pressureSample struct {
	Maximum float64
	Sources []sourceResult
}

type poll struct {
	Sample pressureSample
	Err    error
}

type monitorState struct {
	FailureStreak int
	Paused        bool
	Stopped       bool
}

var designs = []design{
	{
		name: "Minimal module inside scanapp",
		code: `type PressureSource interface {
    Sample(context.Context) (PressureSample, error)
}`,
		ownership: "scanapp owns the interface, result types, factory, and private HTTP implementation.",
		client:    "scanapp.Run creates one client and passes it to the private implementation.",
		depth:     "The interface is deep, but HTTP and OAuth knowledge remains in the large scanapp package.",
		migration: "Moderate. No package move is necessary.",
		mainCost:  "Transport changes and scan orchestration changes remain in the same package.",
	},
	{
		name: "Extensible probes and aggregation policies",
		code: `type PressureSource interface {
    Sample(context.Context) (PressureSample, error)
}
type PressureProbe interface {
    Read(context.Context) (float64, error)
}
type AggregationPolicy interface {
    Aggregate([]SourceResult) (float64, error)
}`,
		ownership: "scanapp owns the consumer interface. An aggregate module owns probe and policy seams.",
		client:    "scanapp.Run creates one client and passes it to HTTP probe adapters.",
		depth:     "Transport and policy extensions are local, but the two extra seams have one current production rule.",
		migration: "High. Existing sources must move through probes and an aggregation policy.",
		mainCost:  "The design adds extension machinery before a second aggregation rule exists.",
	},
	{
		name: "Consumer interface with a deep pressure module (recommended)",
		code: `// package scanapp
type PressureSource interface {
    Sample(context.Context) (pressure.Sample, error)
}

// package pressure
type Sample struct {
    Maximum float64
    Sources []SourceResult
}
func NewSimpleHTTP(endpoint string, client *http.Client) (*SimpleHTTP, error)
func NewOAuthMulti(policy OAuthPolicy, client *http.Client) (*OAuthMulti, error)`,
		ownership:   "scanapp owns the interface. pkg/pressure owns the result model and two remote adapters.",
		client:      "A private scanapp factory creates one client. Adapter constructors reject a nil client.",
		depth:       "Protocol changes stay in pkg/pressure. Control and resume policy stay in scanapp.",
		migration:   "Moderate. HTTP/OAuth code and adapter tests move together.",
		mainCost:    "The design adds one package seam and a small shared result model.",
		recommended: true,
	},
}

var scenarios = map[string][]poll{
	"1": {
		{Sample: pressureSample{Maximum: 42}},
	},
	"2": {
		{Sample: pressureSample{Maximum: 92, Sources: []sourceResult{
			{Name: "src1", Pressure: 45},
			{Name: "src2", Pressure: 92},
		}}},
	},
	"3": {
		{Sample: pressureSample{Sources: []sourceResult{
			{Name: "src1", Pressure: 45},
			{Name: "src2", Err: errors.New("data request failed")},
		}}, Err: errors.New("src2: data request failed")},
	},
	"4": {
		{Err: errors.New("request failed 1")},
		{Err: errors.New("request failed 2")},
		{Sample: pressureSample{Maximum: 30}},
		{Err: errors.New("request failed 3")},
		{Err: errors.New("request failed 4")},
		{Err: errors.New("request failed 5")},
	},
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Pressure source seam prototype")
	fmt.Println()
	for i, candidate := range designs {
		fmt.Printf("%d  %s\n", i+1, candidate.name)
	}
	fmt.Println("a  Show all designs")
	fmt.Print("Design [3]: ")
	designChoice := readChoice(scanner, "3")
	fmt.Println()
	showDesigns(designChoice)

	fmt.Println("Pressure scenarios")
	fmt.Println("1  Simple HTTP success at 42 percent")
	fmt.Println("2  OAuth multi-source success at 92 percent")
	fmt.Println("3  Partial multi-source failure")
	fmt.Println("4  Failure streak with a reset")
	fmt.Print("Scenario [3]: ")
	scenarioChoice := readChoice(scanner, "3")
	fmt.Println()
	runScenario(scenarioChoice)
}

func readChoice(scanner *bufio.Scanner, fallback string) string {
	if !scanner.Scan() {
		return fallback
	}
	choice := strings.TrimSpace(scanner.Text())
	if choice == "" {
		return fallback
	}
	return choice
}

func showDesigns(choice string) {
	switch choice {
	case "1", "2", "3":
		showDesign(designs[int(choice[0]-'1')])
	case "a", "A", "all":
		for _, candidate := range designs {
			showDesign(candidate)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown design %q. Use 1, 2, 3, or a\n", choice)
		os.Exit(2)
	}
}

func showDesign(candidate design) {
	fmt.Println(candidate.name)
	fmt.Println(strings.Repeat("=", len(candidate.name)))
	fmt.Println(candidate.code)
	fmt.Println("Ownership:", candidate.ownership)
	fmt.Println("HTTP client:", candidate.client)
	fmt.Println("Depth and locality:", candidate.depth)
	fmt.Println("Migration:", candidate.migration)
	fmt.Println("Main cost:", candidate.mainCost)
	if candidate.recommended {
		fmt.Println("Verdict: recommended")
	}
	fmt.Println()
}

func runScenario(choice string) {
	polls, ok := scenarios[choice]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown scenario %q. Use 1, 2, 3, or 4\n", choice)
		os.Exit(2)
	}

	state := monitorState{}
	for index, result := range polls {
		if state.Stopped {
			break
		}

		if result.Err != nil {
			state.FailureStreak++
			if state.FailureStreak == 3 {
				state.Stopped = true
			}
		} else {
			state.FailureStreak = 0
			state.Paused = result.Sample.Maximum >= 90
		}

		fmt.Printf("Poll %d\n", index+1)
		fmt.Printf("  sample.maximum: %.1f\n", result.Sample.Maximum)
		fmt.Printf("  sample.sources: %s\n", formatSources(result.Sample.Sources))
		fmt.Printf("  sample.error: %v\n", result.Err)
		fmt.Printf("  monitor.failure_streak: %d\n", state.FailureStreak)
		fmt.Printf("  controller.paused: %t\n", state.Paused)
		fmt.Printf("  monitor.stopped: %t\n", state.Stopped)
		fmt.Println()
	}
}

func formatSources(sources []sourceResult) string {
	if len(sources) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(sources))
	for _, source := range sources {
		parts = append(parts, fmt.Sprintf("{%s pressure=%.1f error=%v}", source.Name, source.Pressure, source.Err))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
