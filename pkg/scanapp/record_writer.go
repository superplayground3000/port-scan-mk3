package scanapp

import "github.com/xuxiping/port-scan-mk3/pkg/writer"

type recordWriter interface {
	Write(record writer.Record) error
	WriteHeader() error
}
