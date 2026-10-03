package relay

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modelmapping"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func init() {
	for apiType := range constant.APITypeDummy {
		_, described := relaycapability.Describe(apiType, "")
		registration, bound := adaptorRegistry[apiType]
		if described != bound || (bound && registration.new == nil) {
			panic(fmt.Sprintf("adaptor capability and constructor disagree for API type %d", apiType))
		}
	}
}

// PlanRequestForChannel checks the concrete incoming protocol against implemented
// host entry points. It performs no transport, credential initialization or debit.
func PlanRequestForChannel(c *gin.Context, info *relaycommon.RelayInfo, selected *model.Channel) error {
	if info == nil || selected == nil {
		return fmt.Errorf("channel and relay info are required")
	}
	info.ConversionPlan, info.ConversionDiagnostics = nil, nil
	if err := constant.ValidateChannelTypeLifecycle(selected.Type); err != nil {
		return err
	}
	apiType, registered := common.ChannelType2APIType(selected.Type)
	if !registered || GetAdaptor(apiType) == nil {
		return fmt.Errorf("channel type %d has no adaptor", selected.Type)
	}
	var requirements relayconvert.RequestRequirements
	var err error
	if info.RequestRequirements == nil && c != nil {
		if cached, ok := c.Get(service.RouteRequestRequirementsKey); ok {
			if parsed, valid := cached.(relayconvert.RequestRequirements); valid {
				info.RequestRequirements = &parsed
			}
		}
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAIRealtime, types.RelayFormatOpenAIAlphaSearch:
		requirements = relayconvert.RequestRequirements{Format: info.RelayFormat, Stream: info.IsStream, Model: info.OriginModelName}
	default:
		if info.RequestRequirements != nil {
			requirements = *info.RequestRequirements
		} else if c != nil && c.Request != nil && c.Request.Body != nil && c.Request.Body != http.NoBody &&
			(info.RelayFormat == types.RelayFormatOpenAI || info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini || info.RelayFormat == types.RelayFormatOpenAIResponses) {
			storage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				return bodyErr
			}
			original, bodyErr := storage.Bytes()
			if bodyErr != nil {
				return bodyErr
			}
			requirements, err = relayconvert.AnalyzeRequestJSON(info.Request, original)
			if err == nil {
				info.RequestRequirements = &requirements
			}
		} else {
			requirements, err = relayconvert.AnalyzeRequest(info.Request)
		}
		if err != nil {
			return err
		}
	}
	if requirements.Format == types.RelayFormatGemini {
		requirements.Model, requirements.Stream = info.OriginModelName, info.IsStream
	}
	if requirements.Model == "" {
		requirements.Model = info.OriginModelName
	}
	mapping, err := modelmapping.Parse(selected.GetModelMapping())
	if err != nil {
		return err
	}
	actualModel, _, err := modelmapping.Resolve(requirements.Model, mapping)
	if err != nil {
		return err
	}
	path := info.RequestURLPath
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	plan, err := service.PlanChannelConversion(requirements, selected, path, actualModel, info.ConversionPolicy)
	info.ConversionDiagnostics = append([]types.ConversionDiagnostic(nil), plan.Diagnostics...)
	if err != nil {
		return err
	}
	info.ConversionPlan = &plan
	return nil
}
