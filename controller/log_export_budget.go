package controller

import (
	"errors"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const logExportMaxRows = 10000
const logExportMaxBytes = 32 * 1024 * 1024

var errLogExportLimit = errors.New("log export limit exceeded")

type logExportBudget struct {
	count int
	bytes int
}

func (b *logExportBudget) write(w io.Writer, log *model.Log) error {
	if b.count >= logExportMaxRows {
		return errLogExportLimit
	}
	line, err := common.Marshal(log)
	if err != nil {
		return err
	}
	if len(line)+1 > logExportMaxBytes-b.bytes {
		return errLogExportLimit
	}
	line = append(line, '\n')
	n, err := w.Write(line)
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	b.count++
	b.bytes += n
	return nil
}
