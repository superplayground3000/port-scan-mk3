// Package perfharness defines deterministic large-data evidence for port-scan.
package perfharness

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const SchemaVersion = "1"

// Family identifies one fixture family.
type Family string

const (
	FamilyRecordHeavy Family = "record-heavy"
)

// Scale gives the expected logical size of one fixture.
type Scale struct {
	InputRecords       uint64 `json:"input_records"`
	CandidateAddresses uint64 `json:"candidate_addresses"`
	ProbeTasks         uint64 `json:"probe_tasks"`
	ExpectedOutputs    uint64 `json:"expected_outputs"`
	TargetBytes        uint64 `json:"target_bytes,omitempty"`
}

// FixtureSpec defines one deterministic fixture.
type FixtureSpec struct {
	Family            Family `json:"family"`
	Shape             string `json:"shape,omitempty"`
	CompletionPercent int    `json:"completion_percent,omitempty"`
	LineEnding        string `json:"line_ending,omitempty"`
	Scale             Scale  `json:"scale"`
	Seed              uint64 `json:"seed"`
}

// Manifest records the generated artifact and its expected counts.
type Manifest struct {
	SchemaVersion      string `json:"schema_version"`
	Family             Family `json:"family"`
	Shape              string `json:"shape,omitempty"`
	Seed               uint64 `json:"seed"`
	InputRecords       uint64 `json:"input_records"`
	CandidateAddresses uint64 `json:"candidate_addresses"`
	ProbeTasks         uint64 `json:"probe_tasks"`
	ExpectedOutputs    uint64 `json:"expected_outputs"`
	ActualBytes        uint64 `json:"actual_bytes"`
	SHA256             string `json:"sha256"`
	ArtifactName       string `json:"artifact"`
	ArtifactPath       string `json:"-"`
	ManifestPath       string `json:"-"`
}

// Suite provides the performance-contract operations.
type Suite struct{}

// New returns a performance harness.
func New() Suite { return Suite{} }

// Generate writes one fixture in a new output directory.
func (Suite) Generate(ctx context.Context, spec FixtureSpec, outputDir string) (Manifest, error) {
	artifactName, err := artifactNameFor(spec.Family)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("create fixture directory: %w", err)
	}
	artifactPath := filepath.Join(outputDir, artifactName)
	file, err := os.Create(artifactPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("create fixture: %w", err)
	}
	hash := sha256.New()
	counting := &countWriter{writer: io.MultiWriter(file, hash)}
	if err := writeFixture(ctx, counting, spec); err != nil {
		_ = file.Close()
		return Manifest{}, err
	}
	if err := file.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close fixture: %w", err)
	}
	manifest := Manifest{
		SchemaVersion:      SchemaVersion,
		Family:             spec.Family,
		Shape:              spec.Shape,
		Seed:               spec.Seed,
		InputRecords:       spec.Scale.InputRecords,
		CandidateAddresses: spec.Scale.CandidateAddresses,
		ProbeTasks:         spec.Scale.ProbeTasks,
		ExpectedOutputs:    spec.Scale.ExpectedOutputs,
		ActualBytes:        counting.count,
		SHA256:             fmt.Sprintf("%x", hash.Sum(nil)),
		ArtifactName:       artifactName,
		ArtifactPath:       artifactPath,
		ManifestPath:       filepath.Join(outputDir, "manifest.json"),
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("encode fixture manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(manifest.ManifestPath, encoded, 0o644); err != nil {
		return Manifest{}, fmt.Errorf("write fixture manifest: %w", err)
	}
	return manifest, nil
}

// Validate makes sure that the artifact bytes match its manifest.
func (Suite) Validate(manifest Manifest) error {
	info, err := os.Stat(manifest.ArtifactPath)
	if err != nil {
		return fmt.Errorf("stat fixture artifact: %w", err)
	}
	if uint64(info.Size()) != manifest.ActualBytes {
		return fmt.Errorf("fixture byte count is %d, want %d", info.Size(), manifest.ActualBytes)
	}
	file, err := os.Open(manifest.ArtifactPath)
	if err != nil {
		return fmt.Errorf("open fixture artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash fixture artifact: %w", err)
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if actual != manifest.SHA256 {
		return fmt.Errorf("fixture digest is %s, want %s", actual, manifest.SHA256)
	}
	return nil
}

func artifactNameFor(family Family) (string, error) {
	switch family {
	case FamilyRecordHeavy, FamilyCandidateHeavy, FamilyRichRecordMixed,
		FamilyRichUniqueKey, FamilyRichHotKey, FamilyRichPrecheck, FamilyRichDeny:
		return "input.csv", nil
	case FamilyPortHeavy:
		return "ports.csv", nil
	case FamilyTaskHeavy, FamilySnapshotHeavy, FamilyResumeHeavy:
		return "snapshot.json", nil
	case FamilyOutputHeavy:
		return "results.csv", nil
	default:
		return "", fmt.Errorf("unsupported fixture family %q", family)
	}
}

func writeFixture(ctx context.Context, output io.Writer, spec FixtureSpec) error {
	switch spec.Family {
	case FamilyRecordHeavy:
		return writeBasicCSV(ctx, output, spec, spec.Scale.InputRecords)
	case FamilyCandidateHeavy:
		return writeBasicCSV(ctx, output, spec, spec.Scale.CandidateAddresses)
	case FamilyPortHeavy:
		return writePorts(ctx, output, spec.Scale.ProbeTasks, spec.LineEnding)
	case FamilyOutputHeavy:
		return writeResults(ctx, output, spec.Scale.ExpectedOutputs)
	case FamilyTaskHeavy, FamilyResumeHeavy:
		return writeResumeSnapshot(output, spec)
	case FamilySnapshotHeavy:
		return writeSizedSnapshot(ctx, output, spec)
	case FamilyRichRecordMixed, FamilyRichUniqueKey, FamilyRichHotKey,
		FamilyRichPrecheck, FamilyRichDeny:
		return writeRichCSV(ctx, output, spec)
	default:
		return fmt.Errorf("unsupported fixture family %q", spec.Family)
	}
}

func writeBasicCSV(ctx context.Context, output io.Writer, spec FixtureSpec, records uint64) error {
	writer := csv.NewWriter(output)
	writer.UseCRLF = spec.LineEnding == "CRLF"
	cidrName := "loopback"
	if records > 0 && spec.Scale.TargetBytes > 0 {
		perRecord := spec.Scale.TargetBytes / records
		if perRecord > 42 {
			padding := perRecord - 42
			maxInt := uint64(^uint(0) >> 1)
			if padding > maxInt {
				return fmt.Errorf("record fixture padding exceeds the addressable integer range")
			}
			cidrName += strings.Repeat("x", int(padding))
		}
	}
	if err := writer.Write([]string{"ip", "ip_cidr", "fab_name", "cidr_name"}); err != nil {
		return fmt.Errorf("write fixture header: %w", err)
	}
	for index := uint64(0); index < records; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		address := fixtureIPv4(index)
		boundary := "127.0.0.0/8"
		if spec.Shape == "unique-groups" {
			boundary = address + "/32"
		}
		if err := writer.Write([]string{address, boundary, fmt.Sprintf("fab-%d", spec.Seed), cidrName}); err != nil {
			return fmt.Errorf("write fixture record: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush fixture: %w", err)
	}
	return nil
}

func writeRichCSV(ctx context.Context, output io.Writer, spec FixtureSpec) error {
	writer := csv.NewWriter(output)
	writer.UseCRLF = spec.LineEnding == "CRLF"
	header := []string{"src_ip", "src_network_segment", "dst_ip", "dst_network_segment", "service_label", "protocol", "port", "decision", "matched_policy_id", "reason"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write rich fixture header: %w", err)
	}
	serviceLabel := "service"
	if spec.Scale.InputRecords > 0 && spec.Scale.TargetBytes > 0 {
		bytesPerRecord := spec.Scale.TargetBytes / spec.Scale.InputRecords
		// Use a conservative estimate so TargetBytes is a lower bound.
		const estimatedFixedRichRecordBytes = uint64(95)
		if bytesPerRecord > estimatedFixedRichRecordBytes {
			padding := bytesPerRecord - estimatedFixedRichRecordBytes
			maxInt := uint64(^uint(0) >> 1)
			if padding > maxInt {
				return fmt.Errorf("rich fixture padding exceeds the addressable integer range")
			}
			serviceLabel += strings.Repeat("x", int(padding))
		}
	}
	for index := uint64(0); index < spec.Scale.InputRecords; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		keyIndex := index
		if spec.Family == FamilyRichHotKey || spec.Family == FamilyRichDeny {
			keyIndex = index % 4
		}
		if spec.Family == FamilyRichDeny && spec.Shape == "accept-deny-conflict" {
			keyIndex = index / 2 % 4
		}
		destination := fixtureIPv4(keyIndex)
		destinationSegment := "127.0.0.0/8"
		decision := "accept"
		reason := "MATCH_POLICY_ACCEPT"
		if spec.Family == FamilyRichPrecheck {
			reason = "PRECHECK_ALLOW_ALL"
			destinationSegment = destination + "/32"
		}
		if spec.Family == FamilyRichDeny && (spec.Shape == "deny-only" || spec.Shape == "accept-deny-conflict" && index%2 == 0) {
			decision = "deny"
		}
		if spec.Family == FamilyRichRecordMixed && index%3 == 0 {
			reason = "PRECHECK_ALLOW_ALL"
			destinationSegment = destination + "/32"
		}
		row := []string{"127.0.0.1", "127.0.0.0/8", destination, destinationSegment, serviceLabel, "tcp", "443", decision, fmt.Sprintf("policy-%d", index%16), reason}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write rich fixture record: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush rich fixture: %w", err)
	}
	return nil
}

func writePorts(ctx context.Context, output io.Writer, records uint64, lineEnding string) error {
	buffer := bufio.NewWriter(output)
	newline := "\n"
	if lineEnding == "CRLF" {
		newline = "\r\n"
	}
	for index := uint64(0); index < records; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		port := index%65_535 + 1
		if _, err := fmt.Fprintf(buffer, "%d/tcp%s", port, newline); err != nil {
			return fmt.Errorf("write port fixture: %w", err)
		}
	}
	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("flush port fixture: %w", err)
	}
	return nil
}

func writeResults(ctx context.Context, output io.Writer, records uint64) error {
	writer := csv.NewWriter(output)
	header := []string{"ip", "ip_cidr", "port", "status", "response_time_ms", "fab_name", "cidr_name", "service_label", "decision", "matched_policy_id", "reason", "execution_key", "src_ip", "src_network_segment"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write result fixture header: %w", err)
	}
	for index := uint64(0); index < records; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		ip := fixtureIPv4(index)
		row := []string{ip, "127.0.0.0/8", "443", "open", "0", "fab", "loopback", "service", "accept", "policy", "MATCH_POLICY_ACCEPT", ip + ":443/tcp", "127.0.0.1", "127.0.0.0/8"}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write result fixture record: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush result fixture: %w", err)
	}
	return nil
}

func writeResumeSnapshot(output io.Writer, spec FixtureSpec) error {
	next := spec.Scale.ProbeTasks * uint64(spec.CompletionPercent) / 100
	status := "pending"
	if next > 0 {
		status = "scanning"
	}
	_, err := fmt.Fprintf(output, "{\"chunks\":[{\"cidr\":\"127.0.0.1/32\",\"cidr_name\":\"loopback\",\"ports\":[\"443/tcp\"],\"next_index\":%d,\"scanned_count\":%d,\"total_count\":%d,\"status\":%q}],\"pre_scan_ping\":{\"enabled\":true,\"timeout_ms\":0}}\n", next, next, spec.Scale.ProbeTasks, status)
	if err != nil {
		return fmt.Errorf("write resume fixture: %w", err)
	}
	return nil
}

func writeSizedSnapshot(ctx context.Context, output io.Writer, spec FixtureSpec) error {
	written := &countWriter{writer: output}
	buffer := bufio.NewWriterSize(written, 256*1_024)
	var err error
	switch spec.Shape {
	case "chunk-heavy":
		err = writeChunkHeavySnapshot(ctx, buffer, written, spec.Scale.TargetBytes)
	case "port-heavy":
		err = writePortHeavySnapshot(ctx, buffer, written, spec.Scale.TargetBytes)
	case "unreachable-heavy":
		err = writeUnreachableSnapshot(ctx, buffer, written, spec.Scale.TargetBytes, "{\"chunks\":[]")
	case "mixed", "":
		err = writeUnreachableSnapshot(ctx, buffer, written, spec.Scale.TargetBytes, "{\"chunks\":[{\"cidr\":\"127.0.0.1/32\",\"cidr_name\":\"mixed\",\"ports\":[\"80/tcp\",\"443/tcp\"],\"next_index\":0,\"scanned_count\":0,\"total_count\":2,\"status\":\"pending\"}]")
	default:
		return fmt.Errorf("unsupported snapshot shape %q", spec.Shape)
	}
	if err != nil {
		return err
	}
	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("flush snapshot fixture: %w", err)
	}
	return nil
}

func writeChunkHeavySnapshot(ctx context.Context, buffer *bufio.Writer, written *countWriter, target uint64) error {
	if _, err := buffer.WriteString("{\"chunks\":["); err != nil {
		return fmt.Errorf("write chunk snapshot prefix: %w", err)
	}
	for index := uint64(0); ; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if index > 0 {
			if err := buffer.WriteByte(','); err != nil {
				return fmt.Errorf("write chunk separator: %w", err)
			}
		}
		if _, err := fmt.Fprintf(buffer, "{\"cidr\":\"127.0.0.1/32\",\"cidr_name\":\"chunk-%d\",\"ports\":[\"443/tcp\"],\"next_index\":0,\"scanned_count\":0,\"total_count\":1,\"status\":\"pending\"}", index); err != nil {
			return fmt.Errorf("write chunk snapshot entry: %w", err)
		}
		if reachedTarget(buffer, written, target, 70) {
			break
		}
	}
	if _, err := buffer.WriteString("],\"pre_scan_ping\":{\"enabled\":true,\"timeout_ms\":0}}\n"); err != nil {
		return fmt.Errorf("write chunk snapshot suffix: %w", err)
	}
	return nil
}

func writePortHeavySnapshot(ctx context.Context, buffer *bufio.Writer, written *countWriter, target uint64) error {
	if _, err := buffer.WriteString("{\"chunks\":[{\"cidr\":\"127.0.0.1/32\",\"cidr_name\":\"ports\",\"ports\":["); err != nil {
		return fmt.Errorf("write port snapshot prefix: %w", err)
	}
	var portCount uint64
	for index := uint64(0); ; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if index > 0 {
			if err := buffer.WriteByte(','); err != nil {
				return fmt.Errorf("write port separator: %w", err)
			}
		}
		if _, err := fmt.Fprintf(buffer, "\"%d/tcp\"", index%65_535+1); err != nil {
			return fmt.Errorf("write port snapshot entry: %w", err)
		}
		portCount++
		if reachedTarget(buffer, written, target, 140) {
			break
		}
	}
	if _, err := fmt.Fprintf(buffer, "],\"next_index\":0,\"scanned_count\":0,\"total_count\":%d,\"status\":\"pending\"}],\"pre_scan_ping\":{\"enabled\":true,\"timeout_ms\":0}}\n", portCount); err != nil {
		return fmt.Errorf("write port snapshot suffix: %w", err)
	}
	return nil
}

func writeUnreachableSnapshot(ctx context.Context, buffer *bufio.Writer, written *countWriter, target uint64, chunks string) error {
	if _, err := buffer.WriteString(chunks + ",\"pre_scan_ping\":{\"enabled\":true,\"timeout_ms\":0,\"unreachable_ipv4_u32\":["); err != nil {
		return fmt.Errorf("write unreachable snapshot prefix: %w", err)
	}
	var scratch [20]byte
	for index := uint64(0); ; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if index > 0 {
			if err := buffer.WriteByte(','); err != nil {
				return fmt.Errorf("write unreachable separator: %w", err)
			}
		}
		entry := strconv.AppendUint(scratch[:0], uint64(uint32(index)), 10)
		if _, err := buffer.Write(entry); err != nil {
			return fmt.Errorf("write unreachable snapshot entry: %w", err)
		}
		if reachedTarget(buffer, written, target, 5) {
			break
		}
	}
	if _, err := buffer.WriteString("]}}\n"); err != nil {
		return fmt.Errorf("write unreachable snapshot suffix: %w", err)
	}
	return nil
}

func reachedTarget(buffer *bufio.Writer, written *countWriter, target, suffixBytes uint64) bool {
	return target == 0 || written.count+uint64(buffer.Buffered())+suffixBytes >= target
}

func fixtureIPv4(index uint64) string {
	index++
	return fmt.Sprintf("127.%d.%d.%d", byte(index>>16), byte(index>>8), byte(index))
}

type countWriter struct {
	writer io.Writer
	count  uint64
}

func (w *countWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.count += uint64(written)
	return written, err
}
