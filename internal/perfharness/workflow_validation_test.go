package perfharness

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
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
