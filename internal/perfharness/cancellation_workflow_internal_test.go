package perfharness

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

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
