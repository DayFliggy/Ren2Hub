package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const requestAdmissionKey = "request_user_admission"

type requestAdmission struct {
	lease         RouteLease
	stop          context.CancelFunc
	cancelRequest context.CancelCauseFunc
	done          chan struct{}
	once          sync.Once
}

// StartRequestUserAdmission is called after entry-specific basic validation.
// Router cleanup releases it after all handler defers, including settlement.
func StartRequestUserAdmission(c *gin.Context) error {
	if _, exists := c.Get(requestAdmissionKey); exists {
		return nil
	}
	lease, err := AcquireUserAdmission(c.Request.Context(), c.GetInt("id"), c.GetString(common.RequestIdKey))
	if err != nil {
		return err
	}
	requestCtx, cancelRequest := context.WithCancelCause(c.Request.Context())
	c.Request = c.Request.WithContext(requestCtx)
	workerCtx, stop := context.WithCancel(context.Background())
	admission := &requestAdmission{lease: lease, stop: stop, cancelRequest: cancelRequest, done: make(chan struct{})}
	c.Set(requestAdmissionKey, admission)
	go admission.renew(workerCtx)
	return nil
}

func (a *requestAdmission) renew(ctx context.Context) {
	defer close(a.done)
	ticker := time.NewTicker(UserAdmissionRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := RenewRouteLease(renewCtx, common.RDB, a.lease, UserAdmissionLeaseTTL)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					a.cancelRequest(err)
					common.SysError("user lease renewal failed: " + err.Error())
				}
				return
			}
		}
	}
}

func ReleaseRequestUserAdmission(c *gin.Context) {
	value, exists := c.Get(requestAdmissionKey)
	if !exists {
		return
	}
	a, ok := value.(*requestAdmission)
	if !ok {
		common.SysError("invalid user admission lifecycle state")
		return
	}
	a.once.Do(func() {
		a.stop()
		a.cancelRequest(context.Canceled)
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		select {
		case <-a.done:
		case <-cleanup.Done():
			common.SysError("user lease renewal shutdown timeout")
		}
		if err := ReleaseRouteLease(cleanup, common.RDB, a.lease); err != nil && !errors.Is(err, ErrRouteLeaseOwnership) {
			common.SysError("user lease release failed: " + err.Error())
		}
	})
}
