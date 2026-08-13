package perfharness

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

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
