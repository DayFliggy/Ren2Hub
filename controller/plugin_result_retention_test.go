package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestPluginDiscardedArtifactListsAreNotFoundForEveryAudience(t *testing.T) {
	for _, dashboard := range []bool{false, true} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodGet, "/artifacts", nil)
		writeTaskArtifacts(c, &model.Task{TaskID: "task_discarded", Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{ResultDiscarded: true}}, dashboard)
		assert.Equal(t, http.StatusNotFound, response.Code)
		assert.Contains(t, response.Body.String(), "artifact_not_found")
		assert.NotContains(t, response.Body.String(), "task_discarded")
	}
}
