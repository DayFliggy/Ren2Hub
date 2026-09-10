package middleware

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func UserAdmissionCleanup() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer service.ReleaseRequestUserAdmission(c)
		c.Next()
	}
}
