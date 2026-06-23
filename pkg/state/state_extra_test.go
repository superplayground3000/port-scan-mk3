package state

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestLoad_WhenJSONIsInvalid_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(file, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestLoadSnapshot_WhenLegacyChunkArrayProvided_PreservesCompatibility(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.json")
	wantChunks := []task.Chunk{
		{CIDR: "10.0.0.0/30", NextIndex: 1, TotalCount: 4, Status: "paused"},
	}
	if err := os.WriteFile(file, []byte(`[{"cidr":"10.0.0.0/30","next_index":1,"total_count":4,"status":"paused"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSnapshot(file)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if !reflect.DeepEqual(got.Chunks, wantChunks) {
		t.Fatalf("chunks mismatch: got %+v want %+v", got.Chunks, wantChunks)
	}
	if !reflect.DeepEqual(got.PreScanPing, PreScanPingState{}) {
		t.Fatalf("expected empty pre-scan ping state, got %+v", got.PreScanPing)
	}
}

func TestLoadSnapshot_WhenObjectEnvelopeMissingChunks_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing_chunks.json")
	if err := os.WriteFile(file, []byte(`{"pre_scan_ping":{"enabled":true,"timeout_ms":100}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("expected missing chunks error")
	}
}

func TestLoadSnapshot_WhenObjectEnvelopeHasUnknownField_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "unknown_field.json")
	if err := os.WriteFile(file, []byte(`{"chunks":[],"unexpected":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadSnapshot_WhenPreScanPingMissingEnabled_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing_enabled.json")
	if err := os.WriteFile(file, []byte(`{"chunks":[],"pre_scan_ping":{"timeout_ms":100}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("expected pre_scan_ping enabled error")
	}
}

func TestLoadSnapshot_WhenPreScanPingMissingTimeoutMS_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing_timeout_ms.json")
	if err := os.WriteFile(file, []byte(`{"chunks":[],"pre_scan_ping":{"enabled":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("expected pre_scan_ping timeout_ms error")
	}
}

func TestLoadSnapshot_WhenPreScanPingHasUnknownField_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "unknown_pre_scan_ping_field.json")
	if err := os.WriteFile(file, []byte(`{"chunks":[],"pre_scan_ping":{"enabled":true,"timeout_ms":100,"unexpected":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("expected pre_scan_ping unknown field error")
	}
}

func TestLoad_WhenObjectEnvelopeHasWrongSchema_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "wrong_schema.json")
	if err := os.WriteFile(file, []byte(`{"chunks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(file); err == nil {
		t.Fatal("expected schema error")
	}
}

func TestWithSIGINTCancel_WhenCancelInvoked_CancelsContext(t *testing.T) {
	ctx, cancel := WithSIGINTCancel(context.Background())
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected canceled context")
	}
}
