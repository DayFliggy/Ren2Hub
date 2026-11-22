package controller

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const logExportMaxConcurrent = 2
const logExportWriteIdleTimeout = 60 * time.Second

var logExportSlots = make(chan struct{}, logExportMaxConcurrent)

func acquireLogExportSlot(c *gin.Context) (func(), bool) {
	select {
	case logExportSlots <- struct{}{}:
		return func() { <-logExportSlots }, true
	default:
		common.ApiErrorStatus(c, http.StatusOK, common.NewMessage("Log export is busy. Try again later."), gin.H{"code": "LOG_EXPORT_BUSY"})
		return nil, false
	}
}

type logExportWriter struct {
	writer      io.Writer
	response    *http.ResponseController
	count       int
	idleTimeout time.Duration
	ctx         context.Context
	failed      bool
}

func newLogExportWriter(c *gin.Context) *logExportWriter {
	return &logExportWriter{writer: c.Writer, response: http.NewResponseController(c.Writer), idleTimeout: logExportWriteIdleTimeout, ctx: c.Request.Context()}
}

func (w *logExportWriter) write(log *model.Log) (err error) {
	defer func() {
		if err != nil {
			w.failed = true
		}
	}()
	if err := w.refreshDeadline(); err != nil {
		return err
	}
	line, err := common.Marshal(log)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	n, err := w.writer.Write(line)
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	w.count++
	if w.count%model.LogExportBatchSize == 0 && w.response != nil {
		return w.flush()
	}
	return nil
}

func (w *logExportWriter) close() {
	// net/http does not reset this for the next keep-alive request when the
	// server's WriteTimeout is zero. Flush the final batch/result before reset.
	if w.ctx.Err() == nil && !w.failed {
		if err := w.flush(); err != nil && w.ctx.Err() == nil {
			common.SysError(common.LogText("failed to flush log export: %s", err.Error()))
		}
	}
	if err := w.response.SetWriteDeadline(time.Time{}); err != nil && w.ctx.Err() == nil {
		common.SysError(common.LogText("failed to reset log export deadline: %s", err.Error()))
	}
}

// Refresh on each write so progress across a batch has no duration cap.
func (w *logExportWriter) refreshDeadline() error {
	if w.response == nil {
		return http.ErrNotSupported
	}
	return w.response.SetWriteDeadline(time.Now().Add(w.idleTimeout))
}

func (w *logExportWriter) flush() error {
	if err := w.refreshDeadline(); err != nil {
		return err
	}
	return w.response.Flush()
}

// Verify the complete middleware writer chain before choosing streaming headers.
func prepareLogExportWriter(c *gin.Context) (*logExportWriter, bool) {
	writer := newLogExportWriter(c)
	if err := writer.refreshDeadline(); err != nil {
		common.SysError(common.LogText("failed to initialize log export deadline: %s", err.Error()))
		common.ApiErrorT(c, "Failed to export logs")
		return nil, false
	}
	return writer, true
}
