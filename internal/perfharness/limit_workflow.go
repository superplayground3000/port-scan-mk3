package perfharness

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// TargetLimitSpec defines one target count or memory limit case.
type TargetLimitSpec struct {
	OutputDir string     `json:"output_dir"`
	Flag      string     `json:"flag"`
	Case      BypassCase `json:"case"`
}

// ResourceLimitSpec defines one non-target data-limit case.
type ResourceLimitSpec struct {
	Flag string     `json:"flag"`
	Case BypassCase `json:"case"`
}

// RunResourceLimitCase runs one resource-limit case through command parsing.
func (suite Suite) RunResourceLimitCase(ctx context.Context, spec ResourceLimitSpec) (CaseResult, error) {
	if !isResourceLimitFlag(spec.Flag) {
		return CaseResult{}, fmt.Errorf("unsupported resource limit flag %q", spec.Flag)
	}
	observations := make([]Observation, 0, 6)
	detail := "configuration rejected the value before production I/O"
	for run := 0; run < 6; run++ {
		observation, err := suite.Measure(ctx, 0, 1, func(context.Context) (uint64, error) {
			caseDetail, caseErr := executeResourceLimitCase(ctx, spec)
			detail = caseDetail
			return 0, caseErr
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run %s %s observation %d: %w", spec.Flag, spec.Case.Kind, run+1, err)
		}
		observations = append(observations, observation)
	}
	result, err := SummarizeCase("limit/"+strings.TrimPrefix(spec.Flag, "-")+"/"+string(spec.Case.Kind), observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Correctness = Correctness{ExpectedValues: true, Detail: detail}
	result.Verdict = Verdict{Passed: true}
	result.Semantic = &SemanticArtifact{Status: "passed"}
	return result, nil
}

func executeResourceLimitCase(ctx context.Context, spec ResourceLimitSpec) (string, error) {
	if spec.Case.Kind == BypassNegative {
		return "", expectResourceLimitParseFailure(spec.Flag, "-1")
	}
	if spec.Case.Kind == BypassOverflow {
		if strings.HasSuffix(spec.Flag, "-gb") || strings.HasSuffix(spec.Flag, "-mb") {
			return "", expectResourceLimitParseFailure(spec.Flag, strconv.FormatInt(math.MaxInt64, 10))
		}
		return "", expectResourceLimitParseFailure(spec.Flag, strconv.FormatUint(uint64(math.MaxInt64)+1, 10))
	}
	_, err := parsedResourceLimits(spec.Flag, resourceLimitCaseFlagValue(spec.Case.Kind))
	if err != nil {
		return "", err
	}
	wantReject := spec.Case.Kind == BypassDefaultPlusOne
	limit := uint64(2)
	if wantReject {
		limit = 1
	}
	if spec.Case.Kind == BypassDisabledTwice {
		limit = 0
	}
	if err := runResourceProductionProbe(ctx, spec.Flag, limit, wantReject); err != nil {
		return "", err
	}
	action := "accepted"
	if wantReject {
		action = "rejected limit plus one"
	}
	return "production enforcement " + action, nil
}

func resourceLimitCaseFlagValue(kind BypassKind) string {
	switch kind {
	case BypassExactDefault, BypassDefaultPlusOne:
		return ""
	case BypassPositiveOverride:
		return "2"
	case BypassDisabledTwice:
		return "0"
	default:
		return ""
	}
}

func runResourceProductionProbe(ctx context.Context, flagName string, limit uint64, wantReject bool) error {
	switch flagName {
	case "-cidr-input-size-limit-gb":
		data := "ip,ip_cidr\n192.0.2.1,192.0.2.0/24\n"
		_, err := input.LoadCIDRsWithColumnsContextAndLimits(ctx, strings.NewReader(data), "ip", "ip_cidr", input.CIDRLimits{MaxBytes: probeBoundary(limit, uint64(len(data)))})
		return expectProbeResult(err, wantReject)
	case "-cidr-input-record-limit":
		data := "ip,ip_cidr\n192.0.2.1,192.0.2.0/24\n192.0.2.2,192.0.2.0/24\n"
		_, err := input.LoadCIDRsWithColumnsContextAndLimits(ctx, strings.NewReader(data), "ip", "ip_cidr", input.CIDRLimits{MaxRecords: limit})
		return expectProbeResult(err, wantReject)
	case "-port-input-size-limit-mb":
		data := "80/tcp\n81/tcp\n"
		_, err := input.LoadPortsContextWithLimits(ctx, strings.NewReader(data), input.PortLimits{MaxBytes: probeBoundary(limit, uint64(len(data)))})
		return expectProbeResult(err, wantReject)
	case "-port-input-record-limit":
		_, err := input.LoadPortsContextWithLimits(ctx, strings.NewReader("80/tcp\n81/tcp\n"), input.PortLimits{MaxRecords: limit})
		return expectProbeResult(err, wantReject)
	case "-snapshot-size-limit-gb", "-snapshot-chunk-limit", "-snapshot-port-entry-limit", "-snapshot-unreachable-ip-limit":
		return runSnapshotLimitProbe(flagName, limit, wantReject)
	case "-pressure-response-size-limit-mb":
		return runPressureSizeProbe(ctx, limit, wantReject)
	case "-pressure-response-entry-limit":
		return runPressureEntryProbe(ctx, limit, wantReject)
	default:
		return fmt.Errorf("unsupported resource limit flag %q", flagName)
	}
}

func probeBoundary(limit, actual uint64) uint64 {
	if limit == 0 {
		return 0
	}
	if limit == 1 {
		return actual - 1
	}
	return actual
}

func expectProbeResult(err error, wantReject bool) error {
	if wantReject && err == nil {
		return fmt.Errorf("production enforcement accepted limit plus one")
	}
	if !wantReject && err != nil {
		return fmt.Errorf("production enforcement rejected allowed value: %w", err)
	}
	return nil
}

func runSnapshotLimitProbe(flagName string, limit uint64, wantReject bool) error {
	dir, err := os.MkdirTemp("", "port-scan-resource-limit-")
	if err != nil {
		return fmt.Errorf("create snapshot probe directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	snapshot := state.Snapshot{
		Chunks: []task.Chunk{
			{CIDR: "192.0.2.0/24", Ports: []string{"80/tcp"}},
			{CIDR: "198.51.100.0/24", Ports: []string{"81/tcp"}},
		},
		PreScanPing: state.PreScanPingState{UnreachableIPv4U32: []uint32{1, 2}},
	}
	source := filepath.Join(dir, "source.json")
	if err := state.SaveSnapshotWithLimits(source, snapshot, state.SnapshotLimits{}); err != nil {
		return fmt.Errorf("create snapshot probe: %w", err)
	}
	limits := state.SnapshotLimits{}
	switch flagName {
	case "-snapshot-size-limit-gb":
		info, err := os.Stat(source)
		if err != nil {
			return fmt.Errorf("stat snapshot probe: %w", err)
		}
		limits.MaxBytes = probeBoundary(limit, uint64(info.Size()))
	case "-snapshot-chunk-limit":
		limits.MaxChunks = limit
	case "-snapshot-port-entry-limit":
		limits.MaxPortEntries = limit
	case "-snapshot-unreachable-ip-limit":
		limits.MaxUnreachableIPs = limit
	}
	_, loadErr := state.LoadSnapshotWithLimits(source, limits)
	if err := expectProbeResult(loadErr, wantReject); err != nil {
		return err
	}
	destination := filepath.Join(dir, "destination.json")
	saveErr := state.SaveSnapshotWithLimits(destination, snapshot, limits)
	if err := expectProbeResult(saveErr, wantReject); err != nil {
		return err
	}
	if wantReject {
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			return fmt.Errorf("snapshot production enforcement left rejected output")
		}
	}
	return nil
}

func runPressureSizeProbe(ctx context.Context, limit uint64, wantReject bool) error {
	body := `{"pressure":1}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	source, err := pressure.NewSimpleHTTPWithLimits(server.URL, server.Client(), pressure.ResponseLimits{MaxBytes: probeBoundary(limit, uint64(len(body)))})
	if err != nil {
		return err
	}
	_, sampleErr := source.Sample(ctx)
	return expectProbeResult(sampleErr, wantReject)
}

func runPressureEntryProbe(ctx context.Context, limit uint64, wantReject bool) error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = fmt.Fprint(w, `{"access_token":"x","token_type":"Bearer","expires_in":0}`)
			return
		}
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":1}},{"data":{"Percent":2}}]`)
	}))
	defer server.Close()
	source, err := pressure.NewOAuthMultiWithLimits(pressure.OAuthConfig{
		AuthEndpoint: server.URL + "/token", DataEndpoints: []string{server.URL + "/data"}, ClientID: "id", ClientSecret: "secret",
	}, server.Client(), pressure.ResponseLimits{MaxEntries: limit})
	if err != nil {
		return err
	}
	_, sampleErr := source.Sample(ctx)
	return expectProbeResult(sampleErr, wantReject)
}

func parsedResourceLimits(flagName, raw string) (config.ScanResourceLimits, error) {
	args := []string{"-cidr-file", "performance-input.csv", "-resume", "performance-snapshot.json", "-disable-api"}
	if raw != "" {
		args = append(args, flagName, raw)
	}
	cfg, err := config.ParseScan(args)
	if err != nil {
		return config.ScanResourceLimits{}, err
	}
	return cfg.ResolveResourceLimits()
}

func expectResourceLimitParseFailure(flagName, value string) error {
	_, err := parsedResourceLimits(flagName, value)
	if err == nil {
		return fmt.Errorf("%s accepted invalid value %s", flagName, value)
	}
	return nil
}

func isResourceLimitFlag(flagName string) bool {
	switch flagName {
	case "-cidr-input-size-limit-gb", "-cidr-input-record-limit", "-port-input-size-limit-mb", "-port-input-record-limit", "-snapshot-size-limit-gb", "-snapshot-chunk-limit", "-snapshot-port-entry-limit", "-snapshot-unreachable-ip-limit", "-pressure-response-size-limit-mb", "-pressure-response-entry-limit":
		return true
	default:
		return false
	}
}

// RunTargetLimitCase runs one target limit case without target allocation.
func (suite Suite) RunTargetLimitCase(ctx context.Context, spec TargetLimitSpec) (CaseResult, error) {
	if spec.Flag != "-target-count-limit" && spec.Flag != "-target-memory-limit-gb" {
		return CaseResult{}, fmt.Errorf("unsupported target limit flag %q", spec.Flag)
	}
	observations := make([]Observation, 0, 6)
	for run := 0; run < 6; run++ {
		observation, err := suite.Measure(ctx, 0, 1, func(context.Context) (uint64, error) {
			return 0, executeTargetLimitCase(spec)
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run %s %s observation %d: %w", spec.Flag, spec.Case.Kind, run+1, err)
		}
		observations = append(observations, observation)
	}
	result, err := SummarizeCase("limit/"+strings.TrimPrefix(spec.Flag, "-")+"/"+string(spec.Case.Kind), observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Correctness = Correctness{ExpectedValues: true, Detail: "production limit result matched the case"}
	result.Verdict = Verdict{Passed: true}
	result.Semantic = &SemanticArtifact{Status: "passed"}
	return result, nil
}

func executeTargetLimitCase(spec TargetLimitSpec) error {
	switch spec.Case.Kind {
	case BypassNegative:
		return expectTargetLimitParseFailure(spec.Flag, "-1")
	case BypassOverflow:
		if spec.Flag == "-target-memory-limit-gb" {
			return expectTargetLimitParseFailure(spec.Flag, strconv.FormatInt(math.MaxInt64, 10))
		}
		limits, err := parsedTargetLimits(0, 0)
		if err != nil {
			return err
		}
		_, err = task.EstimateCandidateCounts([]task.CandidateInput{
			{Row: 1, CIDR: "first", Count: math.MaxUint64},
			{Row: 2, CIDR: "second", Count: 1},
		}, limits)
		if err == nil {
			return fmt.Errorf("%s accepted overflowing candidate count", spec.Flag)
		}
		return nil
	}
	limits, candidates, wantFailure, err := targetLimitInputs(spec)
	if err != nil {
		return err
	}
	_, estimateErr := task.EstimateCandidateCounts([]task.CandidateInput{{Row: 1, CIDR: "performance-case", Count: candidates}}, limits)
	if wantFailure {
		if estimateErr == nil {
			return fmt.Errorf("%s %s accepted %d candidates", spec.Flag, spec.Case.Kind, candidates)
		}
		return nil
	}
	if estimateErr != nil {
		return fmt.Errorf("%s %s rejected %d candidates: %w", spec.Flag, spec.Case.Kind, candidates, estimateErr)
	}
	return nil
}

func targetLimitInputs(spec TargetLimitSpec) (task.ExpansionLimits, uint64, bool, error) {
	countLimit := int64(0)
	memoryLimit := int64(0)
	candidates := task.DefaultTargetCandidateLimit
	wantFailure := false
	if spec.Flag == "-target-count-limit" {
		countLimit = int64(task.DefaultTargetCandidateLimit)
	} else {
		memoryLimit = int64(task.DefaultTargetMemoryLimitGB)
	}
	switch spec.Case.Kind {
	case BypassExactDefault:
	case BypassDefaultPlusOne:
		candidates++
		wantFailure = true
	case BypassPositiveOverride:
		candidates *= 2
		if spec.Flag == "-target-count-limit" {
			countLimit = int64(candidates)
		} else {
			memoryLimit = int64(task.DefaultTargetMemoryLimitGB * 2)
		}
	case BypassDisabledTwice:
		candidates *= 2
		countLimit = 0
		memoryLimit = 0
	default:
		return task.ExpansionLimits{}, 0, false, fmt.Errorf("unsupported bypass kind %q", spec.Case.Kind)
	}
	limits, err := parsedTargetLimits(countLimit, memoryLimit)
	return limits, candidates, wantFailure, err
}

func parsedTargetLimits(countLimit, memoryLimit int64) (task.ExpansionLimits, error) {
	configuration, err := config.ParseValidate([]string{
		"-cidr-file", "performance-input.csv",
		"-target-count-limit", strconv.FormatInt(countLimit, 10),
		"-target-memory-limit-gb", strconv.FormatInt(memoryLimit, 10),
	})
	if err != nil {
		return task.ExpansionLimits{}, err
	}
	values, err := configuration.ResolveTargetExpansion()
	if err != nil {
		return task.ExpansionLimits{}, err
	}
	return values.Limits, nil
}

func expectTargetLimitParseFailure(flagName, value string) error {
	_, err := config.ParseValidate([]string{"-cidr-file", "performance-input.csv", flagName, value})
	if err == nil {
		return fmt.Errorf("%s accepted invalid value %s", flagName, value)
	}
	return nil
}
