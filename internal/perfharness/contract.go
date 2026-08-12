package perfharness

import "time"

// Required fixture counts use decimal units.
const (
	FullItemCount        uint64 = 10_000_000
	SmokeItemCount       uint64 = 100_000
	SmokeSnapshotBytes   uint64 = 100_000_000
	DefaultGeneratorSeed uint64 = 150
)

const (
	FamilyCandidateHeavy  Family = "candidate-heavy"
	FamilyPortHeavy       Family = "port-heavy"
	FamilyTaskHeavy       Family = "task-heavy"
	FamilyOutputHeavy     Family = "output-heavy"
	FamilySnapshotHeavy   Family = "snapshot-heavy"
	FamilyResumeHeavy     Family = "resume-heavy"
	FamilyRichRecordMixed Family = "rich-record-mixed"
	FamilyRichUniqueKey   Family = "rich-unique-key"
	FamilyRichHotKey      Family = "rich-hot-key"
	FamilyRichPrecheck    Family = "rich-precheck"
	FamilyRichDeny        Family = "rich-deny"
)

// CancellationStage identifies a production stage that accepts cancellation.
type CancellationStage string

const (
	CancellationInputParsing  CancellationStage = "input-parsing"
	CancellationRichExpansion CancellationStage = "rich-expansion"
	CancellationBucketBuild   CancellationStage = "bucket-generation"
	CancellationResumeRebuild CancellationStage = "resume-rebuild"
	CancellationResultOutput  CancellationStage = "result-output"
)

// BypassKind identifies one required limit case.
type BypassKind string

const (
	BypassExactDefault     BypassKind = "exact-default"
	BypassDefaultPlusOne   BypassKind = "default-plus-one"
	BypassPositiveOverride BypassKind = "positive-override"
	BypassDisabledTwice    BypassKind = "disabled-two-times"
	BypassNegative         BypassKind = "negative-before-io"
	BypassOverflow         BypassKind = "overflow-before-allocation"
)

// BypassCase defines one reusable limit test.
type BypassCase struct {
	Kind       BypassKind `json:"kind"`
	Multiplier uint64     `json:"multiplier,omitempty"`
}

// LimitCases binds a CLI flag to its required limit tests.
type LimitCases struct {
	Flag  string       `json:"flag"`
	Cases []BypassCase `json:"cases"`
}

// CaseBudget binds an absolute budget to a case-name prefix.
type CaseBudget struct {
	NamePrefix string         `json:"name_prefix"`
	Budget     AbsoluteBudget `json:"budget"`
}

// FixtureCaseMapping connects one required fixture to its production cases.
type FixtureCaseMapping struct {
	Fixture   FixtureSpec `json:"fixture"`
	CaseNames []string    `json:"case_names"`
}

// Contract is the versioned matrix definition shared by both OS adapters.
type Contract struct {
	SchemaVersion        string                  `json:"schema_version"`
	FullFixtures         []FixtureSpec           `json:"full_fixtures"`
	FixtureCases         []FixtureCaseMapping    `json:"fixture_cases"`
	Limits               []LimitCases            `json:"limits"`
	FakeWorkers          []int                   `json:"fake_workers"`
	LoopbackWorkers      []int                   `json:"loopback_workers"`
	CancelStages         []CancellationStage     `json:"cancel_stages"`
	CancelProgress       []int                   `json:"cancel_progress"`
	StopWithin           time.Duration           `json:"stop_within_ns"`
	ForceWithin          time.Duration           `json:"force_within_ns"`
	ForceExitCode        int                     `json:"force_exit_code"`
	InputLineEndings     []string                `json:"input_line_endings"`
	FailureScenarios     []string                `json:"failure_scenarios"`
	RichOversizeCases    []string                `json:"rich_oversize_cases"`
	OutputFlushIntervals []int                   `json:"output_flush_intervals"`
	AbsoluteBudgets      []CaseBudget            `json:"absolute_budgets"`
	RegressionBenchmark  RegressionBenchmarkSpec `json:"regression_benchmark"`
	SmokeItems           uint64                  `json:"smoke_items"`
	SmokeSnapshotBytes   uint64                  `json:"smoke_snapshot_bytes"`
}

// DefaultContract returns the complete approved performance matrix.
func DefaultContract() Contract {
	fixtures := []FixtureSpec{
		{Family: FamilyRecordHeavy, Shape: "one-megabyte", Scale: Scale{InputRecords: 10_000, TargetBytes: 1_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRecordHeavy, Shape: "ten-megabytes", Scale: Scale{InputRecords: 100_000, TargetBytes: 10_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRecordHeavy, Shape: "one-hundred-megabytes", Scale: Scale{InputRecords: 1_000_000, TargetBytes: 100_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRecordHeavy, Shape: "one-gigabyte", Scale: Scale{InputRecords: FullItemCount, TargetBytes: 1_000_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilyCandidateHeavy, Scale: Scale{CandidateAddresses: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyPortHeavy, Scale: Scale{ProbeTasks: 65_535}, Seed: DefaultGeneratorSeed},
		{Family: FamilyTaskHeavy, Scale: Scale{ProbeTasks: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyOutputHeavy, Scale: Scale{ProbeTasks: FullItemCount, ExpectedOutputs: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "chunk-heavy", Scale: Scale{TargetBytes: 1_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "port-heavy", Scale: Scale{TargetBytes: 10_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "unreachable-heavy", Scale: Scale{TargetBytes: 100_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "mixed", Scale: Scale{TargetBytes: 1_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "mixed", Scale: Scale{TargetBytes: 10_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "mixed", Scale: Scale{TargetBytes: 100_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilySnapshotHeavy, Shape: "mixed", Scale: Scale{TargetBytes: 1_000_000_000}, Seed: DefaultGeneratorSeed},
		{Family: FamilyResumeHeavy, CompletionPercent: 0, Scale: Scale{ProbeTasks: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyResumeHeavy, CompletionPercent: 50, Scale: Scale{ProbeTasks: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyResumeHeavy, CompletionPercent: 99, Scale: Scale{ProbeTasks: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRichRecordMixed, Scale: Scale{InputRecords: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRichUniqueKey, Scale: Scale{InputRecords: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRichHotKey, Scale: Scale{InputRecords: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRichPrecheck, Scale: Scale{InputRecords: FullItemCount, CandidateAddresses: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRichDeny, Shape: "deny-only", Scale: Scale{InputRecords: FullItemCount}, Seed: DefaultGeneratorSeed},
		{Family: FamilyRichDeny, Shape: "accept-deny-conflict", Scale: Scale{InputRecords: FullItemCount}, Seed: DefaultGeneratorSeed},
	}
	flags := []string{
		"-target-count-limit",
		"-target-memory-limit-gb",
		"-cidr-input-size-limit-gb",
		"-cidr-input-record-limit",
		"-port-input-size-limit-mb",
		"-port-input-record-limit",
		"-snapshot-size-limit-gb",
		"-snapshot-chunk-limit",
		"-snapshot-port-entry-limit",
		"-snapshot-unreachable-ip-limit",
		"-pressure-response-size-limit-mb",
		"-pressure-response-entry-limit",
	}
	limits := make([]LimitCases, 0, len(flags))
	for _, flag := range flags {
		limits = append(limits, LimitCases{Flag: flag, Cases: defaultBypassCases()})
	}
	return Contract{
		SchemaVersion:        SchemaVersion,
		FullFixtures:         fixtures,
		FixtureCases:         fixtureCaseMappings(fixtures),
		Limits:               limits,
		FakeWorkers:          []int{1, 16, 256},
		LoopbackWorkers:      []int{1, 32},
		CancelStages:         []CancellationStage{CancellationInputParsing, CancellationRichExpansion, CancellationBucketBuild, CancellationResumeRebuild, CancellationResultOutput},
		CancelProgress:       []int{1, 50, 99},
		StopWithin:           time.Second,
		ForceWithin:          2 * time.Second,
		ForceExitCode:        130,
		InputLineEndings:     []string{"LF", "CRLF"},
		FailureScenarios:     []string{"output-failure", "snapshot-save-failure", "pressure-fatal-error", "rewind", "resume"},
		RichOversizeCases:    []string{"default-reject", "override-complete"},
		OutputFlushIntervals: []int{1, 1000, 0},
		AbsoluteBudgets: []CaseBudget{
			{NamePrefix: "record-heavy", Budget: AbsoluteBudget{MaxWallTime: 5 * time.Minute, MaxCommittedBytes: 8_000_000_000}},
			{NamePrefix: "candidate-heavy", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "port-heavy", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "task-heavy", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "rich-record-mixed", Budget: AbsoluteBudget{MaxWallTime: 10 * time.Minute, MaxCommittedBytes: 8_000_000_000}},
			{NamePrefix: "rich-unique-key", Budget: AbsoluteBudget{MaxWallTime: 10 * time.Minute, MaxCommittedBytes: 8_000_000_000}},
			{NamePrefix: "rich-hot-key", Budget: AbsoluteBudget{MaxWallTime: 10 * time.Minute, MaxCommittedBytes: 8_000_000_000}},
			{NamePrefix: "rich-precheck", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "rich-deny", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "snapshot-load", Budget: AbsoluteBudget{MaxWallTime: 2 * time.Minute, MaxCommittedBytes: 6_000_000_000}},
			{NamePrefix: "snapshot-save", Budget: AbsoluteBudget{MaxWallTime: 2 * time.Minute, MaxCommittedBytes: 6_000_000_000}},
			{NamePrefix: "output-heavy", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 4_000_000_000}},
			{NamePrefix: "resume-heavy", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "scan-orchestration", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 4_000_000_000}},
			{NamePrefix: "production-workflow", Budget: AbsoluteBudget{MaxWallTime: 45 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "production-rich-deny", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "native-loopback", Budget: AbsoluteBudget{MaxWallTime: 45 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
			{NamePrefix: "", Budget: AbsoluteBudget{MaxWallTime: 15 * time.Minute, MaxCommittedBytes: 24_000_000_000}},
		},
		RegressionBenchmark: RegressionBenchmarkSpec{
			TargetBytes:   1_000_000,
			BeforeNSPerOp: 7_762_347,
			BeforeBPerOp:  1_134_359,
		},
		SmokeItems:         SmokeItemCount,
		SmokeSnapshotBytes: SmokeSnapshotBytes,
	}
}

func defaultBypassCases() []BypassCase {
	return []BypassCase{
		{Kind: BypassExactDefault, Multiplier: 1},
		{Kind: BypassDefaultPlusOne},
		{Kind: BypassPositiveOverride, Multiplier: 2},
		{Kind: BypassDisabledTwice, Multiplier: 2},
		{Kind: BypassNegative},
		{Kind: BypassOverflow},
	}
}
