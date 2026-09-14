package gemini

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const veoRequestContextKey = "validated_veo_request"

// ValidateVeoRequest prepares the shared Gemini/Vertex wire and billing input
// before price calculation or admission. It never downloads remote images.
func ValidateVeoRequest(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	params, err := ResolveVeoParameters(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_veo_parameters", http.StatusBadRequest)
	}
	if len(req.Images) > 0 && (req.InputReference != "" || req.Image != "" && (len(req.Images) != 1 || req.Images[0] != req.Image)) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("Veo input image must use a single source"), "invalid_veo_image", http.StatusBadRequest)
	}
	if len(req.Images) == 0 && req.InputReference != "" {
		req.Images = []string{req.InputReference}
	}
	if len(req.Images) > 1 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("Veo task submission supports one input image"), "invalid_veo_image", http.StatusBadRequest)
	}
	image, err := ExtractMultipartImage(c)
	if err == nil && len(req.Images) > 0 {
		if image != nil {
			err = fmt.Errorf("Veo input image must use a single source")
		} else {
			image, err = ParseImageInput(req.Images[0])
		}
	}
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_veo_image", http.StatusBadRequest)
	}
	if image != nil {
		info.Action = constant.TaskActionGenerate
	}
	c.Set(veoRequestContextKey, VeoRequestPayload{Instances: []VeoInstance{{Prompt: req.Prompt, Image: image}}, Parameters: params})
	return nil
}

func BuildVeoRequestBody(c *gin.Context) (io.Reader, error) {
	value, exists := c.Get(veoRequestContextKey)
	if !exists {
		return nil, fmt.Errorf("validated Veo request is missing")
	}
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func EstimateVeoBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	value, exists := c.Get(veoRequestContextKey)
	if !exists {
		return nil // BuildVeoRequestBody rejects the missing validation before sending.
	}
	params := value.(VeoRequestPayload).Parameters
	return map[string]float64{
		"seconds":    float64(params.DurationSeconds),
		"resolution": VeoResolutionRatio(info.UpstreamModelName, params.Resolution),
	}
}
