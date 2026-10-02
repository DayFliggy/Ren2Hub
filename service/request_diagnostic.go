package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const RequestDiagnosticRetentionDays = 7

func ValidRequestCorrelationID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}

type RequestDiagnosticView struct {
	SchemaVersion int                    `json:"schema_version"`
	RequestID     string                 `json:"request_id"`
	TokenID       int                    `json:"token_id"`
	TraceID       string                 `json:"trace_id,omitempty"`
	ThreadID      string                 `json:"thread_id,omitempty"`
	ModelName     string                 `json:"model_name"`
	CreatedAt     int64                  `json:"created_at"`
	WaitMS        int64                  `json:"wait_ms"`
	UserQueueMS   int64                  `json:"user_queue_ms"`
	FinalError    string                 `json:"final_error,omitempty"`
	Attempts      []RouteDecisionAttempt `json:"attempts"`
}

func PersistRouteDecision(ctx context.Context, decision RouteDecision) error {
	if decision.UserID <= 0 {
		return nil
	}
	if decision.TraceID != "" && !ValidRequestCorrelationID(decision.TraceID) || decision.ThreadID != "" && !ValidRequestCorrelationID(decision.ThreadID) {
		return errors.New("invalid diagnostic correlation")
	}
	if len(decision.Attempts) > DefaultTotalAttempts {
		return errors.New("invalid diagnostic attempts")
	}
	attempts := append([]RouteDecisionAttempt{}, decision.Attempts...)
	for i := range attempts {
		// Only the stable outcome and measured times are stored, never provider messages.
		attempts[i].Diagnostics = nil
	}
	payload, err := common.Marshal(attempts)
	if err != nil {
		return err
	}
	return model.SaveGatewayRequestDiagnostic(ctx, &model.GatewayRequestDiagnostic{
		RequestID: decision.RequestID, UserID: decision.UserID, TokenID: decision.TokenID,
		TraceID: decision.TraceID, ThreadID: decision.ThreadID, ModelName: decision.RequestModel,
		CreatedAt: decision.GeneratedAt / 1000, WaitMS: decision.WaitMS, UserQueueMS: decision.UserQueueMS,
		FinalError: decision.FinalError, AttemptsJSON: string(payload),
	})
}

func ProjectRequestDiagnostic(row model.GatewayRequestDiagnostic, internal bool) (RequestDiagnosticView, error) {
	result := RequestDiagnosticView{SchemaVersion: 1, RequestID: row.RequestID, TokenID: row.TokenID, TraceID: row.TraceID, ThreadID: row.ThreadID, ModelName: row.ModelName, CreatedAt: row.CreatedAt, WaitMS: row.WaitMS, UserQueueMS: row.UserQueueMS, FinalError: row.FinalError, Attempts: []RouteDecisionAttempt{}}
	if err := common.Unmarshal([]byte(row.AttemptsJSON), &result.Attempts); err != nil {
		return result, err
	}
	if result.Attempts == nil {
		result.Attempts = []RouteDecisionAttempt{}
	}
	for i := range result.Attempts {
		result.Attempts[i].Diagnostics = nil
		if !internal {
			result.Attempts[i].ChannelID = 0
			result.Attempts[i].CredentialID = ""
		}
	}
	return result, nil
}

func StartRequestDiagnosticsCleanup() {
	common.BackgroundWorkers.Go(func(ctx context.Context) {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
			for {
				count, err := model.CleanupGatewayRequestDiagnostics(deadline, time.Now().Add(-RequestDiagnosticRetentionDays*24*time.Hour))
				if err != nil {
					common.SysError("request diagnostic cleanup failed")
					break
				}
				if count < 1000 || deadline.Err() != nil {
					break
				}
			}
			cancel()
		}
	})
}
