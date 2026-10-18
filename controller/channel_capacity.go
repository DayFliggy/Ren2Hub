package controller

import (
	"errors"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func acquireChannelManagement(c *gin.Context, ch *model.Channel) (*service.ConcurrencyPermit, bool) {
	permit, err := service.AcquireChannelConcurrency(c.Request.Context(), ch)
	if err == nil {
		return permit, true
	}
	status := http.StatusServiceUnavailable
	if errors.Is(err, service.ErrConcurrencyFull) {
		status = http.StatusTooManyRequests
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
	return nil, false
}

func rejectBusyChannelManagement(c *gin.Context, err error) bool {
	if !errors.Is(err, service.ErrConcurrencyFull) {
		return false
	}
	c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": err.Error()})
	return true
}
