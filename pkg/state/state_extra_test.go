package state

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestLoadSnapshot_WhenNestedObjectHasUnknownField_ReturnsError(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "chunk", json: `{"chunks":[{"cidr":"x","extra":1}]}`},
		{name: "output", json: `{"chunks":[],"output":{"scan_path":"x","extra":1}}`},
		{name: "target expansion", json: `{"chunks":[],"target_expansion":{"candidate_count":1,"extra":1}}`},
		{name: "escaped unknown", json: `{"chunks":[],"output":{"scan_path":"x","extr\u0061":1}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "snapshot.json")
			if err := os.WriteFile(file, []byte(test.json), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSnapshotWithLimits(file, SnapshotLimits{}); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("LoadSnapshotWithLimits() error = %v, want unknown field", err)
			}
		})
	}
}

func TestLoadSnapshot_WhenKnownFieldsUseEscapes_LoadsThem(t *testing.T) {
	file := filepath.Join(t.TempDir(), "escaped_fields.json")
	data := `{"\u0063hunks":[],"pre_scan_ping":{"en\u0061bled":true,"timeout_ms":7},"output":{"scan_p\u0061th":"brace } comma ,","open_path":""}}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
	if err != nil {
		t.Fatalf("LoadSnapshotWithLimits() error = %v", err)
	}
	if !got.PreScanPing.Enabled || got.PreScanPing.TimeoutMS != 7 || got.Output == nil || got.Output.ScanPath != "brace } comma ," {
		t.Fatalf("escaped-field snapshot = %#v", got)
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

func TestLoadSnapshot_WhenKnownFieldsRepeat_UsesLastValue(t *testing.T) {
	file := filepath.Join(t.TempDir(), "duplicate_fields.json")
	data := `{
		"pre_scan_ping":{"enabled":false,"timeout_ms":1,"unreachable_ipv4_u32":[9]},
		"chunks":[],
		"rich_deny_excluded":false,
		"output":{"scan_path":"old.csv","open_path":"old-open.csv"},
		"chunks":[{"cidr":"10.0.0.0/30","status":"paused"}],
		"pre_scan_ping":null,
		"pre_scan_ping":{"enabled":true,"timeout_ms":42,"unreachable_ipv4_u32":[1,2]},
		"rich_deny_excluded":true,
		"output":null
	}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
	if err != nil {
		t.Fatalf("LoadSnapshotWithLimits() error = %v", err)
	}
	want := Snapshot{
		Chunks:           []task.Chunk{{CIDR: "10.0.0.0/30", Status: "paused"}},
		PreScanPing:      PreScanPingState{Enabled: true, TimeoutMS: 42, UnreachableIPv4U32: []uint32{1, 2}},
		RichDenyExcluded: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestLoadSnapshot_WhenOptionalFieldsAreNull_UsesZeroValues(t *testing.T) {
	file := filepath.Join(t.TempDir(), "null_fields.json")
	data := `{
		"target_semantics_version":1,
		"basic_port_fallback":null,
		"target_expansion":null,
		"output":null,
		"pre_scan_ping":null,
		"chunks":[]
	}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
	if err != nil {
		t.Fatalf("LoadSnapshotWithLimits() error = %v", err)
	}
	if got.TargetSemanticsVersion != 1 || !reflect.DeepEqual(got.PreScanPing, PreScanPingState{}) || got.Output != nil || got.TargetExpansion != nil || got.BasicPortFallback != nil {
		t.Fatalf("unexpected null-field result: %#v", got)
	}
}

func TestLoadSnapshot_WhenChunksIsNull_TreatsItAsMissing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "null_chunks.json")
	if err := os.WriteFile(file, []byte(`{"chunks":null}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
	if err == nil || !strings.Contains(err.Error(), "missing required chunks field") {
		t.Fatalf("LoadSnapshotWithLimits() error = %v, want missing chunks", err)
	}
}

func TestLoadSnapshot_WhenJSONHasAnotherRoot_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "multiple_roots.json")
	if err := os.WriteFile(file, []byte(`{"chunks":[]} {"chunks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
	if err == nil {
		t.Fatal("LoadSnapshotWithLimits() succeeded with another JSON root")
	}
}

func TestLoadSnapshot_UnreachableNumberSyntaxMatchesJSONUint32(t *testing.T) {
	tests := []struct {
		name    string
		array   string
		want    []uint32
		wantErr bool
	}{
		{name: "null", array: "null"},
		{name: "empty", array: "[]", want: []uint32{}},
		{name: "whitespace", array: "[ 1,\n 2 ]", want: []uint32{1, 2}},
		{name: "exponent", array: "[1e2]", wantErr: true},
		{name: "negative", array: "[-1]", wantErr: true},
		{name: "fraction", array: "[1.5]", wantErr: true},
		{name: "overflow", array: "[4294967296]", wantErr: true},
		{name: "element null", array: "[null]", want: []uint32{0}},
		{name: "trailing comma", array: "[1,]", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "snapshot.json")
			data := `{"chunks":[],"pre_scan_ping":{"enabled":true,"timeout_ms":1,"unreachable_ipv4_u32":` + test.array + `}}`
			if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
			if test.wantErr {
				if err == nil {
					t.Fatalf("LoadSnapshotWithLimits() succeeded with %s", test.array)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadSnapshotWithLimits() error = %v", err)
			}
			if !reflect.DeepEqual(got.PreScanPing.UnreachableIPv4U32, test.want) {
				t.Fatalf("unreachable values = %#v, want %#v", got.PreScanPing.UnreachableIPv4U32, test.want)
			}
		})
	}
}

func TestLoadSnapshot_WhenFileChangesAfterStat_ReturnsError(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "truncated", data: `{"chunks":[]`},
		{name: "grown", data: `{"chunks":[]} `},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "snapshot.json")
			if err := os.WriteFile(file, []byte(`{"chunks":[]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			original := loadFileOps
			loadFileOps.open = func(path string) (*os.File, error) {
				if err := os.WriteFile(path, []byte(test.data), 0o644); err != nil {
					return nil, err
				}
				return os.Open(path)
			}
			t.Cleanup(func() { loadFileOps = original })

			_, err := LoadSnapshotWithLimits(file, SnapshotLimits{})
			if err == nil || !strings.Contains(err.Error(), "file size changed after stat") {
				t.Fatalf("LoadSnapshotWithLimits() error = %v, want mutation error", err)
			}
		})
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
