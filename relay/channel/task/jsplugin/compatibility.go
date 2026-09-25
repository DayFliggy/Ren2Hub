package jsplugin

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// Legacy signatures remain only to satisfy the shared task interface. A
// persisted plugin task must supply its immutable identity and private state.
func (a *TaskAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, fmt.Errorf("plugin polling requires a persisted task")
}

func (a *TaskAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, fmt.Errorf("plugin polling requires a persisted task")
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, response *http.Response, info *relaycommon.RelayInfo) (string, []byte, any, *dto.TaskError) {
	parsed, taskErr := a.ParseResponse(c, response, info)
	if taskErr != nil {
		return "", nil, nil, taskErr
	}
	return parsed.UpstreamTaskID, parsed.TaskData, parsed.ClientResponse, nil
}
