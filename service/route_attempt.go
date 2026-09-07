package service

import "github.com/gin-gonic/gin"

// RetryParam tracks an attempt within one request, including Compact stages.
type RetryParam struct {
	Ctx        *gin.Context
	Retry      *int
	MaxRetries *int
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	p.SetRetry(p.GetRetry() + 1)
}
