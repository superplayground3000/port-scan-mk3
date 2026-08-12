package perfharness

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestNormalizedCSVDigestMatchesTheFormerInMemoryResult(t *testing.T) {
	t.Parallel()

	header := []string{"ip", "status", "response_time_ms"}
	rows := [][]string{
		{"192.0.2.3", "closed", "3.75"},
		{"192.0.2.1", "open", "1.25"},
		{"192.0.2.2", "open", "2.50"},
	}
	left := writeDigestCSV(t, "left.csv", header, rows, false)
	right := writeDigestCSV(t, "right.csv", header, [][]string{rows[1], rows[2], rows[0]}, true)

	want := formerInMemoryDigest(rows, 2)
	for _, path := range []string{left, right} {
		got, err := normalizedCSVDigestWithChunkSize(path, 2)
		if err != nil {
			t.Fatalf("normalizedCSVDigestWithChunkSize(%s): %v", path, err)
		}
		if got != want {
			t.Fatalf("digest = %s, want former result %s", got, want)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(left), ".normalized-sort-*")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary sort runs remain: matches=%v err=%v", matches, err)
	}
}

func TestNormalizedCSVDigestKeepsValuesStatusAndCountExact(t *testing.T) {
	t.Parallel()

	header := []string{"ip", "status", "response_time_ms"}
	baseRows := [][]string{{"192.0.2.1", "open", "1"}, {"192.0.2.2", "closed", "2"}}
	base := writeDigestCSV(t, "base.csv", header, baseRows, false)
	baseDigest, err := normalizedCSVDigestWithChunkSize(base, 1)
	if err != nil {
		t.Fatal(err)
	}
	changes := [][][]string{
		{{"192.0.2.9", "open", "9"}, {"192.0.2.2", "closed", "2"}},
		{{"192.0.2.1", "closed", "9"}, {"192.0.2.2", "closed", "2"}},
		{{"192.0.2.1", "open", "9"}},
	}
	for index, rows := range changes {
		path := writeDigestCSV(t, fmt.Sprintf("changed-%d.csv", index), header, rows, false)
		got, digestErr := normalizedCSVDigestWithChunkSize(path, 1)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		if got == baseDigest {
			t.Fatalf("change %d did not change the digest", index)
		}
	}
}

func TestNormalizedCSVDigestReportsRunFailuresAndCleansTemporaryFiles(t *testing.T) {
	t.Parallel()

	path := writeDigestCSV(t, "failures.csv", []string{"ip", "status"}, [][]string{{"192.0.2.1", "open"}, {"192.0.2.2", "closed"}}, false)
	tests := []struct {
		name string
		ops  *faultNormalizedCSVFileOps
		want string
	}{
		{name: "create", ops: &faultNormalizedCSVFileOps{failCreate: true}, want: "create normalized CSV sort run"},
		{name: "write", ops: &faultNormalizedCSVFileOps{failWrite: true}, want: "flush normalized CSV sort run"},
		{name: "merge read", ops: &faultNormalizedCSVFileOps{failOpenAt: 2}, want: "open normalized CSV sort run"},
		{name: "cleanup", ops: &faultNormalizedCSVFileOps{failRemove: true}, want: "remove normalized CSV sort run"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizedCSVDigestWithOps(path, 1, test.ops)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			for _, temporaryPath := range test.ops.created {
				if test.ops.failRemove {
					if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) {
						t.Fatalf("remove injected cleanup artifact: %v", removeErr)
					}
					continue
				}
				if _, statErr := os.Stat(temporaryPath); !os.IsNotExist(statErr) {
					t.Fatalf("temporary sort run remains after failure: %s (%v)", temporaryPath, statErr)
				}
			}
		})
	}
}

type faultNormalizedCSVFileOps struct {
	openCalls  int
	failOpenAt int
	failCreate bool
	failWrite  bool
	failRemove bool
	created    []string
}

func (ops *faultNormalizedCSVFileOps) Open(path string) (io.ReadCloser, error) {
	ops.openCalls++
	if ops.failOpenAt > 0 && ops.openCalls == ops.failOpenAt {
		return nil, os.ErrPermission
	}
	return os.Open(path)
}

func (ops *faultNormalizedCSVFileOps) CreateTemp(dir, pattern string) (namedWriteCloser, error) {
	if ops.failCreate {
		return nil, os.ErrPermission
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	ops.created = append(ops.created, file.Name())
	if ops.failWrite {
		return failingNamedWriteCloser{File: file}, nil
	}
	return file, nil
}

func (ops *faultNormalizedCSVFileOps) Remove(path string) error {
	if ops.failRemove {
		return os.ErrPermission
	}
	return os.Remove(path)
}

type failingNamedWriteCloser struct{ *os.File }

func (writer failingNamedWriteCloser) Write([]byte) (int, error) { return 0, os.ErrClosed }

func writeDigestCSV(t *testing.T, name string, header []string, rows [][]string, crlf bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := csv.NewWriter(file)
	writer.UseCRLF = crlf
	if err := writer.Write(header); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func formerInMemoryDigest(rows [][]string, durationColumn int) string {
	normalized := make([]string, 0, len(rows))
	for _, row := range rows {
		copyRow := append([]string(nil), row...)
		copyRow[durationColumn] = "<duration>"
		normalized = append(normalized, strings.Join(copyRow, "\x00"))
	}
	sort.Strings(normalized)
	digest := sha256.Sum256([]byte(strings.Join(normalized, "\n")))
	return fmt.Sprintf("%x", digest[:])
}
