package scanapp

import (
	"fmt"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// batchOutputs holds file handles and writers for scan result output.
// The writer fields use the RecordWriter interface to decouple from concrete types.
type batchOutputs struct {
	scanFile       *os.File
	openOnlyFile   *os.File
	scanWriter     RecordWriter
	openOnlyWriter RecordWriter
	scanFinalPath  string
	openFinalPath  string
}

type unreachableOutput struct {
	file      *os.File
	writer    *writer.UnreachableWriter
	finalPath string
}

func openBatchOutputs(scanPath, openPath string) (*batchOutputs, error) {
	scanTmpPath := scanPath + ".tmp"
	scanFile, err := os.Create(scanTmpPath)
	if err != nil {
		return nil, err
	}

	scanWriter := writer.NewCSVWriter(scanFile)
	if err := scanWriter.WriteHeader(); err != nil {
		if closeErr := scanFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close scan file: %v\n", closeErr)
		}
		return nil, err
	}

	openTmpPath := openPath + ".tmp"
	openOnlyFile, err := os.Create(openTmpPath)
	if err != nil {
		if closeErr := scanFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close scan file: %v\n", closeErr)
		}
		return nil, err
	}

	openOnlyWriter := writer.NewOpenOnlyWriter(writer.NewCSVWriter(openOnlyFile))
	if err := openOnlyWriter.WriteHeader(); err != nil {
		if closeErr := openOnlyFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close open-only file: %v\n", closeErr)
		}
		if closeErr := scanFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close scan file: %v\n", closeErr)
		}
		return nil, err
	}

	return &batchOutputs{
		scanFile:       scanFile,
		openOnlyFile:   openOnlyFile,
		scanWriter:     scanWriter,
		openOnlyWriter: openOnlyWriter,
		scanFinalPath:  scanPath,
		openFinalPath:  openPath,
	}, nil
}

func openUnreachableOutput(finalPath string) (*unreachableOutput, error) {
	tmpPath := finalPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}

	unreachableWriter := writer.NewUnreachableWriter(file)
	if err := unreachableWriter.WriteHeader(); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &unreachableOutput{
		file:      file,
		writer:    unreachableWriter,
		finalPath: finalPath,
	}, nil
}

func (b *batchOutputs) Finalize(success bool) error {
	if b == nil {
		return nil
	}
	var firstErr error
	if b.openOnlyFile != nil {
		if err := b.openOnlyFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if b.scanFile != nil {
		if err := b.scanFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return firstErr
	}

	if success {
		if err := os.Rename(b.scanFinalPath+".tmp", b.scanFinalPath); err != nil {
			return err
		}
		if err := os.Rename(b.openFinalPath+".tmp", b.openFinalPath); err != nil {
			return err
		}
	}
	return nil
}

func (u *unreachableOutput) Finalize(success bool) error {
	if u == nil {
		return nil
	}
	if u.file != nil {
		if err := u.file.Close(); err != nil {
			return err
		}
	}
	if success {
		if err := os.Rename(u.finalPath+".tmp", u.finalPath); err != nil {
			return err
		}
	}
	return nil
}

func openBatchOutputsAfterUnreachable(paths batchOutputPaths) (*batchOutputs, error) {
	output, err := openUnreachableOutput(paths.unreachablePath)
	if err != nil {
		return nil, err
	}
	if err := output.Finalize(true); err != nil {
		return nil, err
	}
	return openBatchOutputs(paths.scanPath, paths.openPath)
}
