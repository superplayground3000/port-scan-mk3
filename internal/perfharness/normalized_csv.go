package perfharness

import (
	"bufio"
	"container/heap"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func normalizedCSVDigest(path string) (string, error) {
	return normalizedCSVDigestWithChunkSize(path, 100_000)
}

func normalizedCSVDigestWithChunkSize(path string, chunkSize int) (digest string, resultErr error) {
	return normalizedCSVDigestWithOps(path, chunkSize, osNormalizedCSVFileOps{})
}

type namedWriteCloser interface {
	io.WriteCloser
	Name() string
}

type normalizedCSVFileOps interface {
	Open(string) (io.ReadCloser, error)
	CreateTemp(string, string) (namedWriteCloser, error)
	Remove(string) error
}

type osNormalizedCSVFileOps struct{}

func (osNormalizedCSVFileOps) Open(path string) (io.ReadCloser, error) { return os.Open(path) }
func (osNormalizedCSVFileOps) CreateTemp(dir, pattern string) (namedWriteCloser, error) {
	return os.CreateTemp(dir, pattern)
}
func (osNormalizedCSVFileOps) Remove(path string) error { return os.Remove(path) }

func normalizedCSVDigestWithOps(path string, chunkSize int, fileOps normalizedCSVFileOps) (digest string, resultErr error) {
	if chunkSize < 1 {
		return "", fmt.Errorf("normalization chunk size must be positive")
	}
	file, err := fileOps.Open(path)
	if err != nil {
		return "", fmt.Errorf("open workflow CSV for normalization: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && resultErr == nil {
			resultErr = fmt.Errorf("close workflow CSV after normalization: %w", closeErr)
		}
	}()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return "", fmt.Errorf("workflow CSV has no header")
		}
		return "", fmt.Errorf("read workflow CSV header for normalization: %w", err)
	}
	if len(header) == 0 {
		return "", fmt.Errorf("workflow CSV has no header")
	}
	durationColumn := -1
	for index, name := range header {
		if name == "response_time_ms" {
			durationColumn = index
			break
		}
	}
	runPaths := make([]string, 0)
	defer func() {
		for _, runPath := range runPaths {
			if removeErr := fileOps.Remove(runPath); removeErr != nil && !os.IsNotExist(removeErr) && resultErr == nil {
				resultErr = fmt.Errorf("remove normalized CSV sort run: %w", removeErr)
			}
		}
	}()
	chunk := make([]string, 0, chunkSize)
	flushChunk := func() error {
		if len(chunk) == 0 {
			return nil
		}
		sort.Strings(chunk)
		run, createErr := fileOps.CreateTemp(filepath.Dir(path), ".normalized-sort-*")
		if createErr != nil {
			return fmt.Errorf("create normalized CSV sort run: %w", createErr)
		}
		runPaths = append(runPaths, run.Name())
		buffered := bufio.NewWriter(run)
		for _, value := range chunk {
			if writeErr := writeLengthDelimited(buffered, value); writeErr != nil {
				_ = run.Close()
				return fmt.Errorf("write normalized CSV sort run: %w", writeErr)
			}
		}
		if flushErr := buffered.Flush(); flushErr != nil {
			_ = run.Close()
			return fmt.Errorf("flush normalized CSV sort run: %w", flushErr)
		}
		if closeErr := run.Close(); closeErr != nil {
			return fmt.Errorf("close normalized CSV sort run: %w", closeErr)
		}
		chunk = chunk[:0]
		return nil
	}
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("read workflow CSV for normalization: %w", readErr)
		}
		if durationColumn >= 0 && durationColumn < len(row) {
			row[durationColumn] = "<duration>"
		}
		chunk = append(chunk, strings.Join(row, "\x00"))
		if len(chunk) == chunkSize {
			if err := flushChunk(); err != nil {
				return "", err
			}
		}
	}
	if err := flushChunk(); err != nil {
		return "", err
	}
	return mergeNormalizedRuns(runPaths, fileOps)
}

func writeLengthDelimited(writer io.Writer, value string) error {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	if _, err := writer.Write(length[:]); err != nil {
		return err
	}
	_, err := io.WriteString(writer, value)
	return err
}

type normalizedRun struct {
	file   io.ReadCloser
	reader *bufio.Reader
	value  string
}

type normalizedRunHeap []*normalizedRun

func (items normalizedRunHeap) Len() int           { return len(items) }
func (items normalizedRunHeap) Less(i, j int) bool { return items[i].value < items[j].value }
func (items normalizedRunHeap) Swap(i, j int)      { items[i], items[j] = items[j], items[i] }
func (items *normalizedRunHeap) Push(value any)    { *items = append(*items, value.(*normalizedRun)) }
func (items *normalizedRunHeap) Pop() any {
	old := *items
	value := old[len(old)-1]
	*items = old[:len(old)-1]
	return value
}

func mergeNormalizedRuns(paths []string, fileOps normalizedCSVFileOps) (digest string, resultErr error) {
	runs := make(normalizedRunHeap, 0, len(paths))
	defer func() {
		for _, run := range runs {
			if closeErr := run.file.Close(); closeErr != nil && resultErr == nil {
				resultErr = fmt.Errorf("close normalized CSV sort run: %w", closeErr)
			}
		}
	}()
	for _, path := range paths {
		file, err := fileOps.Open(path)
		if err != nil {
			return "", fmt.Errorf("open normalized CSV sort run: %w", err)
		}
		run := &normalizedRun{file: file, reader: bufio.NewReader(file)}
		value, err := readLengthDelimited(run.reader)
		if err != nil {
			_ = file.Close()
			return "", fmt.Errorf("read normalized CSV sort run: %w", err)
		}
		run.value = value
		runs = append(runs, run)
	}
	heap.Init(&runs)
	digester := sha256.New()
	first := true
	for runs.Len() > 0 {
		run := heap.Pop(&runs).(*normalizedRun)
		if !first {
			_, _ = digester.Write([]byte{'\n'})
		}
		first = false
		_, _ = io.WriteString(digester, run.value)
		next, err := readLengthDelimited(run.reader)
		if err == nil {
			run.value = next
			heap.Push(&runs, run)
			continue
		}
		if err != io.EOF {
			_ = run.file.Close()
			return "", fmt.Errorf("read normalized CSV sort run: %w", err)
		}
		if err := run.file.Close(); err != nil {
			return "", fmt.Errorf("close normalized CSV sort run: %w", err)
		}
	}
	return fmt.Sprintf("%x", digester.Sum(nil)), nil
}

func readLengthDelimited(reader io.Reader) (string, error) {
	var length [8]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return "", err
	}
	size := binary.BigEndian.Uint64(length[:])
	if size > uint64(^uint(0)>>1) {
		return "", fmt.Errorf("normalized CSV sort value length %d overflows int", size)
	}
	value := make([]byte, int(size))
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	return string(value), nil
}
