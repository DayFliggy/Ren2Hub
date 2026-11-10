package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// A Midjourney continuation (change, simple-change, modal, video, action)
// looks up its origin task before admission. A found task pins its original
// channel and reuses Distribute's channel setup. An unresolved origin leaves
// the existing model checks, routing and handler error order unchanged.

const (
	contextKeyMidjourneyOrigin      = "midjourney_origin_task"
	contextKeyOriginChannelPrepared = "origin_task_channel_prepared"
)

// lookUpMidjourneyOrigin runs on a fresh parse of the body: GetMjRequestModel
// has already converted a plus action in its own copy. A request whose origin
// cannot be named is left to the handler, which reports the missing field.
func lookUpMidjourneyOrigin(c *gin.Context, relayMode int) error {
	var request dto.MidjourneyRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return err
	}
	_, mjID, mjErr := service.MidjourneyOriginTaskID(relayMode, &request)
	if mjErr != nil || mjID == "" {
		return nil
	}
	task := model.GetByMJId(c.GetInt("id"), mjID)
	if task == nil {
		return nil
	}
	c.Set(contextKeyMidjourneyOrigin, task)
	service.GetChannelConstraints(c).AddPin(dto.ChannelPin{
		ChannelId: task.ChannelId,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})
	return nil
}

// MidjourneyOriginLookup returns only a task found for this request's user.
// An absent result leaves the handler's original lookup behavior in place.
func MidjourneyOriginLookup(c *gin.Context) (task *model.Midjourney, found bool) {
	value, exists := c.Get(contextKeyMidjourneyOrigin)
	if !exists {
		return nil, false
	}
	task, _ = value.(*model.Midjourney)
	return task, task != nil
}

// markOriginChannelPrepared records that this attempt already admitted and
// set up the origin channel, so the handler does not rotate its key again.
func markOriginChannelPrepared(c *gin.Context, channelID int) {
	c.Set(contextKeyOriginChannelPrepared, channelID)
}

// OriginChannelPrepared reports whether Distribute set up channelID for this attempt.
func OriginChannelPrepared(c *gin.Context, channelID int) bool {
	prepared, ok := c.Get(contextKeyOriginChannelPrepared)
	return ok && channelID > 0 && prepared == channelID
}
