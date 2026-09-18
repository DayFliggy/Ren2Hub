package helper

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func StreamError(info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil || info.StreamStatus == nil {
		return nil
	}
	if err := info.StreamStatus.Err(); err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	return nil
}

func StreamOutcome(info *relaycommon.RelayInfo) relayconvert.StreamOutcome {
	if info != nil && info.StreamStatus != nil {
		if info.StreamStatus.IsNormalEnd() {
			return relayconvert.StreamOutcomeCompleted
		}
		if info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
			return relayconvert.StreamOutcomeCancelled
		}
	}
	return relayconvert.StreamOutcomeFailed
}

func WriteConvertedStreamResult(c *gin.Context, result relayconvert.ResponseResult) (err error) {
	ObserveResponseConversion(c, result)
	eventType := ""
	defer func() {
		if err == nil && (eventType == "error" || eventType == "response.failed" || eventType == "response.incomplete" && !result.Termination.IsSuccessful()) {
			c.Set("gateway_stream_error_sent", true)
		}
	}()
	switch value := result.Value.(type) {
	case relayconvert.ChatToResponsesStreamEvent:
		eventType = value.Type
		body, err := common.Marshal(value.Payload)
		if err != nil {
			return err
		}
		return ResponseChunkData(c, dto.ResponsesStreamResponse{Type: value.Type}, string(body))
	case *dto.ResponsesStreamResponse:
		eventType = value.Type
		body, err := common.Marshal(value)
		if err != nil {
			return err
		}
		return ResponseChunkData(c, *value, string(body))
	case *dto.ClaudeResponse:
		eventType = value.Type
		return ClaudeData(c, *value)
	case dto.ClaudeResponse:
		eventType = value.Type
		return ClaudeData(c, value)
	case nil:
		return fmt.Errorf("converted stream response is nil")
	default:
		return ObjectData(c, value)
	}
}

func FinalizeConvertedStream(c *gin.Context, info *relaycommon.RelayInfo, state *relayconvert.ResponseStreamState) *types.NewAPIError {
	outcome := StreamOutcome(info)
	if outcome != relayconvert.StreamOutcomeCompleted && !c.Writer.Written() {
		return StreamError(info)
	}
	results, err := relayconvert.FinalizeStreamResponseWithOutcome(c, info, state, outcome)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	for _, result := range results {
		if err := WriteConvertedStreamResult(c, result); err != nil {
			return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
	}
	return StreamError(info)
}

// StreamResult is passed to each dataHandler invocation, providing methods
// to record soft errors, signal fatal stops, or mark normal completion.
// StreamScannerHandler checks IsStopped() after each callback invocation.
type StreamResult struct {
	status      *relaycommon.StreamStatus
	stopped     bool
	hadError    bool
	validOutput bool
	eventName   string
}

func newStreamResult(status *relaycommon.StreamStatus) *StreamResult {
	return &StreamResult{status: status}
}

// Error records a soft error. The stream continues processing.
// Can be called multiple times per chunk.
func (r *StreamResult) Error(err error) {
	if err == nil {
		return
	}
	r.status.RecordError(err.Error())
	r.hadError = true
}

// Stop records a fatal error and marks the stream to stop after this chunk.
func (r *StreamResult) Stop(err error) {
	if err != nil {
		r.status.RecordError(err.Error())
		r.hadError = true
	}
	r.status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
	r.stopped = true
}

// Done signals that the handler has finished processing normally
// (e.g., Dify "message_end"). The stream stops after this chunk.
func (r *StreamResult) Done() {
	r.status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	r.stopped = true
}

// IsStopped returns whether Stop() or Done() was called during this chunk.
func (r *StreamResult) IsStopped() bool {
	return r.stopped
}

// MarkValidOutput marks semantic output, not a parsed prelude or usage frame.
func (r *StreamResult) MarkValidOutput() {
	if r != nil {
		r.validOutput = true
	}
}

func (r *StreamResult) HasValidOutput() bool {
	return r != nil && r.validOutput
}

func (r *StreamResult) HasError() bool {
	return r != nil && r.hadError
}

// EventName is the SSE event field associated with this data frame.
func (r *StreamResult) EventName() string { return r.eventName }

// reset clears the per-chunk stopped flag so the object can be reused.
func (r *StreamResult) reset() {
	r.stopped = false
	r.hadError = false
	r.validOutput = false
}
