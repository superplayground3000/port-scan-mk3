package perfharness

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

func TestCancellationSnapshotReferenceMatchesUninterruptedProductionOrder(t *testing.T) {
	const items = uint64(200)
	spec := CancellationSpec{
		OutputDir: filepath.Join(t.TempDir(), "uninterrupted"),
		Items:     items,
		Workers:   4,
		Stage:     CancellationResultOutput,
		Percent:   50,
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareScanCancellation(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.LoadSnapshot(prepared.inputs.snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := cancellationSnapshotTaskEvidence(snapshot, false)
	if err != nil {
		t.Fatal(err)
	}
	actual := newOrderedTaskEvidence()
	err = scanapp.Run(context.Background(), prepared.config, io.Discard, io.Discard, scanapp.RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return fakeOpenConn{}, nil
		},
		TaskObserver: func(ip string, port int) {
			actual.Observe(net.JoinHostPort(ip, strconv.Itoa(port)) + "/tcp")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := actual.Snapshot()
	if observed.Count != items || observed.Digest != reference.Digest {
		t.Fatalf("production order = count:%d digest:%s, reference = count:%d digest:%s", observed.Count, observed.Digest, reference.Count, reference.Digest)
	}
}

func TestCancellationSnapshotLimitBypassChangesOnlySerializedBytes(t *testing.T) {
	limits := cancellationSnapshotLimits()
	defaults := state.DefaultSnapshotLimits()
	if limits.MaxBytes != 0 {
		t.Fatalf("snapshot byte limit = %d, want disabled", limits.MaxBytes)
	}
	if limits.MaxChunks != defaults.MaxChunks || limits.MaxPortEntries != defaults.MaxPortEntries ||
		limits.MaxUnreachableIPs != defaults.MaxUnreachableIPs {
		t.Fatalf("snapshot object limits changed: got %+v, defaults %+v", limits, defaults)
	}
}

func TestCancellationCIDRLimitBypassChangesOnlyInputBytes(t *testing.T) {
	limits := cancellationCIDRLimits()
	defaults := input.DefaultCIDRLimits("")
	if limits.MaxBytes != 0 {
		t.Fatalf("CIDR byte limit = %d, want disabled", limits.MaxBytes)
	}
	if limits.MaxRecords != defaults.MaxRecords {
		t.Fatalf("CIDR record limit = %d, want %d", limits.MaxRecords, defaults.MaxRecords)
	}
}

func TestCancellationProgressReaderStartsAfterTheCountPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	injector, err := NewCancellationInjector(CancellationInputParsing, 50, 4, cancel)
	if err != nil {
		t.Fatal(err)
	}
	reader := &progressReader{
		reader:     strings.NewReader("data"),
		totalBytes: 4,
		totalItems: 4,
		injector:   injector,
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("the count pass triggered cancellation")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2)
	if _, err := reader.Read(buffer); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("parse pass context = %v, want context.Canceled", ctx.Err())
	}
}
