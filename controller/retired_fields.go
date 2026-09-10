package controller

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// Only management DTOs use this boundary. Inference payloads keep their
// protocol-specific extension fields instead of rejecting unknown SDK fields.
func rejectRetiredManagementFields(c *gin.Context, fields ...string) bool {
	for _, field := range fields {
		if _, exists := c.Request.URL.Query()[field]; exists {
			return rejectRetiredManagementField(c, field)
		}
	}
	if c.Request.Body == nil || c.Request.Method == http.MethodGet {
		return false
	}
	body, err := c.GetRawData()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return true
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var object map[string]any
	if common.Unmarshal(body, &object) != nil {
		return false // The owning controller returns its normal parse error.
	}
	for _, field := range fields {
		parts := strings.Split(field, ".")
		current := object
		for i, part := range parts {
			value, exists := current[part]
			if !exists {
				break
			}
			if i == len(parts)-1 {
				return rejectRetiredManagementField(c, field)
			}
			current, _ = value.(map[string]any)
		}
	}
	return false
}

func rejectRetiredManagementField(c *gin.Context, field string) bool {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
		"success": false, "code": "retired_field", "message": "retired management field: " + field,
	})
	return true
}
