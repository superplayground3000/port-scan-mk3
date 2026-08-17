package perfharness

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestWorkflowSpecificationsRejectInvalidCasesBeforeRunning(t *testing.T) {
	t.Parallel()

	suite := Suite{}
	checks := []struct {
		name string
		run  func() error
	}{
		{name: "resume zero items", run: func() error {
			_, err := suite.RunResumeSmoke(context.Background(), ResumeSpec{})
			return err
		}},
		{name: "resume negative percent", run: func() error {
			_, err := suite.RunResumeSmoke(context.Background(), ResumeSpec{Items: 1, CompletedPercent: -1})
			return err
		}},
		{name: "resume complete", run: func() error {
			_, err := suite.RunResumeSmoke(context.Background(), ResumeSpec{Items: 1, CompletedPercent: 100})
			return err
		}},
		{name: "rich deny zero items", run: func() error {
			_, err := suite.RunRichDenySmoke(context.Background(), RichDenySpec{})
			return err
		}},
		{name: "rich deny shape", run: func() error {
			_, err := suite.RunRichDenySmoke(context.Background(), RichDenySpec{Items: 1, Shape: "unknown"})
			return err
		}},
		{name: "rich zero items", run: func() error {
			_, err := suite.RunRichSmoke(context.Background(), RichSpec{})
			return err
		}},
		{name: "rich family", run: func() error {
			_, err := suite.RunRichSmoke(context.Background(), RichSpec{Items: 1, Family: FamilyRecordHeavy})
			return err
		}},
		{name: "failure zero items", run: func() error {
			_, err := suite.RunFailureSmoke(context.Background(), FailureSpec{})
			return err
		}},
		{name: "failure scenario", run: func() error {
			_, err := suite.RunFailureSmoke(context.Background(), FailureSpec{
				OutputDir: filepath.Join(t.TempDir(), "unknown failure"), Items: 1, Workers: 1, Scenario: "unknown",
			})
			return err
		}},
		{name: "failure workers", run: func() error {
			_, err := suite.RunFailureSmoke(context.Background(), FailureSpec{
				OutputDir: filepath.Join(t.TempDir(), "invalid workers"), Items: 1, Workers: 0, Scenario: "output-failure",
			})
			return err
		}},
		{name: "failure canceled preparation", run: func() error {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := suite.RunFailureSmoke(ctx, FailureSpec{
				OutputDir: filepath.Join(t.TempDir(), "canceled preparation"), Items: 1, Workers: 1, Scenario: "output-failure",
			})
			return err
		}},
		{name: "cancellation zero items", run: func() error {
			_, err := suite.RunCancellationSmoke(context.Background(), CancellationSpec{})
			return err
		}},
	}
	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()
			if err := check.run(); err == nil {
				t.Fatal("invalid workflow specification was accepted")
			}
		})
	}
}

func TestAcceptedRichWorkflowUsesBoundedPositiveSizeOverrides(t *testing.T) {
	for _, check := range []struct {
		actual       uint64
		wantCIDR     uint64
		wantSnapshot uint64
	}{
		{actual: 999_999_999, wantCIDR: 1_000_000_000, wantSnapshot: 2_000_000_000},
		{actual: 1_000_000_000, wantCIDR: 1_000_000_000, wantSnapshot: 2_000_000_000},
		{actual: 1_022_664_300, wantCIDR: 2_000_000_000, wantSnapshot: 4_000_000_000},
	} {
		limits, err := acceptedRichResourceLimits(check.actual)
		if err != nil {
			t.Fatalf("actual bytes %d: %v", check.actual, err)
		}
		if limits.CIDR.MaxBytes != check.wantCIDR {
			t.Fatalf("actual bytes %d: CIDR byte limit = %d, want %d", check.actual, limits.CIDR.MaxBytes, check.wantCIDR)
		}
		if limits.Snapshot.MaxBytes != check.wantSnapshot {
			t.Fatalf("actual bytes %d: snapshot byte limit = %d, want %d", check.actual, limits.Snapshot.MaxBytes, check.wantSnapshot)
		}
	}
	limits, err := acceptedRichResourceLimits(1_022_664_300)
	if err != nil {
		t.Fatal(err)
	}
	defaultCIDR := input.DefaultCIDRLimits("")
	if limits.CIDR.MaxRecords != defaultCIDR.MaxRecords {
		t.Fatalf("accepted rich CIDR record limit = %d, want %d", limits.CIDR.MaxRecords, defaultCIDR.MaxRecords)
	}
	defaultSnapshot := state.DefaultSnapshotLimits()
	if limits.Snapshot.MaxChunks != defaultSnapshot.MaxChunks ||
		limits.Snapshot.MaxPortEntries != defaultSnapshot.MaxPortEntries ||
		limits.Snapshot.MaxUnreachableIPs != defaultSnapshot.MaxUnreachableIPs ||
		limits.Port != input.DefaultPortLimits("") || limits.Pressure != pressure.DefaultResponseLimits() {
		t.Fatalf("accepted rich unrelated limits changed: %+v", limits)
	}

	fixture := FixtureSpec{
		Family: FamilyRichRecordMixed,
		Scale:  Scale{InputRecords: 100, TargetBytes: 20_000},
		Seed:   DefaultGeneratorSeed,
	}
	workflowLimits, err := acceptedRichResourceLimits(20_000)
	if err != nil {
		t.Fatal(err)
	}
	restricted := workflowLimits
	restricted.CIDR.MaxBytes = 10_000
	if _, err := runRichProductionWithLimits(context.Background(), filepath.Join(t.TempDir(), "restricted"), 100, 2, fixture, &restricted); err == nil {
		t.Fatal("restricted accepted rich workflow did not reject an oversized input")
	}
	if _, err := runRichProductionWithLimits(context.Background(), filepath.Join(t.TempDir(), "overridden"), 100, 2, fixture, &workflowLimits); err != nil {
		t.Fatalf("accepted rich workflow with positive size override: %v", err)
	}
}

func TestAcceptedRichResourceLimitsRejectInvalidSizes(t *testing.T) {
	for _, actualBytes := range []uint64{0, ^uint64(0)/2 + 1, ^uint64(0)} {
		if _, err := acceptedRichResourceLimits(actualBytes); err == nil {
			t.Fatalf("accepted rich input size %d did not fail", actualBytes)
		}
	}
}

func TestExpectedFailureRejectsMissingOrDifferentErrors(t *testing.T) {
	t.Parallel()

	for _, err := range []error{nil, errors.New("different failure")} {
		if _, validationErr := expectedFailure("case", "operation", "class", err, "expected failure"); validationErr == nil {
			t.Fatalf("expectedFailure accepted %v", err)
		}
	}
}

func TestFailureReferenceEvidenceRejectsInvalidSnapshots(t *testing.T) {
	t.Parallel()

	for _, snapshot := range []state.Snapshot{
		{},
		{Chunks: []task.Chunk{{TotalCount: 2, NextIndex: 0, Ports: []string{"443/tcp"}}}},
	} {
		if _, err := failureCandidateTaskEvidence(snapshot, 1); err == nil {
			t.Fatalf("failureCandidateTaskEvidence accepted %+v", snapshot)
		}
	}
}

func TestOutputHandleCheckRejectsMissingFiles(t *testing.T) {
	t.Parallel()

	if outputHandlesReleased(filepath.Join(t.TempDir(), "missing.csv")) {
		t.Fatal("outputHandlesReleased accepted a missing output file")
	}
}

func TestFailureEvidenceCompletionRejectsMissingSnapshots(t *testing.T) {
	t.Parallel()

	inputs := failureInputs{snapshotPath: filepath.Join(t.TempDir(), "missing.json")}
	if _, err := completeOutputFailureEvidence(context.Background(), FailureSpec{Items: 1}, inputs, failureStageState{}); err == nil {
		t.Fatal("completeOutputFailureEvidence accepted a missing snapshot")
	}
	if _, err := completeSnapshotFailureEvidence(inputs, failureStageState{}); err == nil {
		t.Fatal("completeSnapshotFailureEvidence accepted a missing snapshot")
	}
	if _, err := completePressureFailureEvidence(context.Background(), FailureSpec{Items: 1}, inputs, failureStageState{}); err == nil {
		t.Fatal("completePressureFailureEvidence accepted a missing snapshot")
	}
}

func TestWorkflowArtifactEvidenceReportsMissingAndMalformedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "missing.csv")
	if _, err := fileSize(missing); err == nil {
		t.Fatal("fileSize accepted a missing artifact")
	}
	if _, _, err := workflowOutputPaths(root); err == nil {
		t.Fatal("workflowOutputPaths accepted missing output files")
	}
	if _, err := countCSVRows(missing); err == nil {
		t.Fatal("countCSVRows accepted a missing CSV")
	}
	if _, err := fileDigest(missing); err == nil {
		t.Fatal("fileDigest accepted a missing artifact")
	}
	if _, err := workflowResultFromFiles(0, 0, false, Observation{}, Observation{}, missing, missing, nil); err == nil {
		t.Fatal("workflowResultFromFiles accepted missing output files")
	}

	empty := filepath.Join(root, "empty.csv")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := countCSVRows(empty); err == nil {
		t.Fatal("countCSVRows accepted a CSV without a header")
	}
	malformed := filepath.Join(root, "malformed.csv")
	if err := os.WriteFile(malformed, []byte("a,b\n\"unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := countCSVRows(malformed); err == nil {
		t.Fatal("countCSVRows accepted a malformed row")
	}
	if _, err := fileDigest(root); err == nil {
		t.Fatal("fileDigest accepted a directory")
	}
}

func TestFakeConnectionImplementsRequiredOperations(t *testing.T) {
	t.Parallel()

	connection := fakeOpenConn{}
	data := []byte("probe")
	if count, err := connection.Write(data); err != nil || count != len(data) {
		t.Fatalf("Write = %d, %v", count, err)
	}
	if count, err := connection.Read(data); count != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("Read = %d, %v", count, err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if connection.LocalAddr().Network() != "fake" || connection.LocalAddr().String() != "local" {
		t.Fatalf("local address = %v", connection.LocalAddr())
	}
	if connection.RemoteAddr().Network() != "fake" || connection.RemoteAddr().String() != "remote" {
		t.Fatalf("remote address = %v", connection.RemoteAddr())
	}
	for _, setDeadline := range []func(time.Time) error{
		connection.SetDeadline,
		connection.SetReadDeadline,
		connection.SetWriteDeadline,
	} {
		if err := setDeadline(time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}
