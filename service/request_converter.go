package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// ValidateRequestProjection applies the same field contract used by route
// planning to the concrete DTO immediately before a host projection runs.
func ValidateRequestProjection(info *relaycommon.RelayInfo, apiType int, request any) error {
	requirements, err := relayconvert.AnalyzeRequest(request)
	if err != nil {
		return err
	}
	definition, found := relaycapability.Describe(apiType, requirements.Model)
	surface, supported := definition.Resolve(requirements.Format)
	if !found || !supported || surface.TextProjection == nil {
		return fmt.Errorf("request has no registered text projection")
	}
	policy := types.ConversionLossPolicySafe
	if info != nil && info.ConversionPolicy != "" {
		policy = info.ConversionPolicy
	}
	if info != nil && info.ConversionPlan != nil {
		if info.ConversionPlan.RequestProjection != surface.TextProjection.ID {
			return fmt.Errorf("request projection no longer matches the selected plan")
		}
		policy = info.ConversionPlan.Policy
	}
	diagnostics, err := surface.TextProjection.Check(requirements, policy)
	RecordRequestConversionDiagnostics(info, &relayconvert.RequestResult{Diagnostics: diagnostics})
	return err
}

func init() {
	relayconvert.SetMediaResolver(relayconvert.MediaResolver{
		// relayconvert is gin-free; recover the gin context when the caller
		// passed one so file caching/cleanup keeps working.
		GetBase64Data: func(ctx context.Context, source types.FileSource, reason ...string) (string, string, error) {
			ginCtx, _ := ctx.(*gin.Context)
			return GetBase64Data(ginCtx, source, reason...)
		},
		DecodeBase64FileData: DecodeBase64FileData,
	})
}

func ConvertRequest(c *gin.Context, info *relaycommon.RelayInfo, target types.RelayFormat, request any) (*relayconvert.RequestResult, error) {
	var result *relayconvert.RequestResult
	var err error
	if info != nil && info.ConversionPlan != nil && info.ConversionPlan.To == target {
		result, err = relayconvert.ExecuteConversionPlan(c, info, *info.ConversionPlan, request)
	} else {
		result, err = relayconvert.ConvertRequest(c, info, target, request)
	}
	RecordRequestConversionDiagnostics(info, result)
	return result, err
}

func ConvertRequestByID(c *gin.Context, info *relaycommon.RelayInfo, converter string, request any) (*relayconvert.RequestResult, error) {
	if info != nil && info.ConversionPlan != nil && info.ConversionPlan.RequestConverter == converter {
		result, err := relayconvert.ExecuteConversionPlan(c, info, *info.ConversionPlan, request)
		RecordRequestConversionDiagnostics(info, result)
		return result, err
	}
	result, err := relayconvert.ConvertRequestByID(c, info, converter, request)
	RecordRequestConversionDiagnostics(info, result)
	return result, err
}

func ConvertRequestVia(c *gin.Context, info *relaycommon.RelayInfo, request any, path ...types.RelayFormat) (*relayconvert.RequestResult, error) {
	result, err := relayconvert.ConvertRequestVia(c, info, request, path...)
	RecordRequestConversionDiagnostics(info, result)
	return result, err
}

func RecordRequestConversionDiagnostics(info *relaycommon.RelayInfo, result *relayconvert.RequestResult) {
	if info == nil || result == nil {
		return
	}
	for _, diagnostic := range result.Diagnostics {
		found := false
		for _, previous := range info.ConversionDiagnostics {
			if previous == diagnostic {
				found = true
				break
			}
		}
		if !found && len(info.ConversionDiagnostics) < 128 {
			info.ConversionDiagnostics = append(info.ConversionDiagnostics, diagnostic)
		}
	}
}

func ClaudeToOpenAIRequest(claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, &claudeRequest)
	if err != nil {
		return nil, err
	}
	openAIRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	return openAIRequest, nil
}

func GeminiToOpenAIRequest(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, geminiRequest)
	if err != nil {
		return nil, err
	}
	openAIRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	return openAIRequest, nil
}
