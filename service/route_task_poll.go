package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// 任务轮询和中继请求共用渠道的即时并发上限。满载时本轮轮询直接跳过，
// 下一轮再查，不排队，也不计入轮询失败。
const pollSkipLogInterval = time.Minute

// pollSkipLog 是进程内唯一的限频状态：只记上次写日志的时间和其后被抑制的次数，大小固定。
var pollSkipLog struct {
	mu         sync.Mutex
	last       time.Time
	suppressed int
}

// doPollRequest 在实际请求期间持有渠道许可；准入失败仅跳过本轮，取消仍返回 ctx.Err()。
func doPollRequest(ctx context.Context, ch *model.Channel, fetch func() (*http.Response, error)) (resp *http.Response, skip bool, err error) {
	resp, err = DoChannelRequest(ctx, ch, fetch)
	if errors.Is(err, ErrConcurrencyFull) {
		logPollSkip(ctx, ch.Id, "is at its concurrency limit")
		return nil, true, nil
	}
	if errors.Is(err, errConcurrencyLimitUnavailable) && ctx.Err() == nil {
		logPollSkip(ctx, ch.Id, "has an unreadable concurrency limit")
		return nil, true, nil
	}
	if errors.Is(err, errConcurrencyResourceDeleted) {
		logPollSkip(ctx, ch.Id, "was deleted before admission")
		return nil, true, nil
	}
	if ctx.Err() != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, true, ctx.Err()
	}
	return resp, false, err
}

func logPollSkip(ctx context.Context, channelID int, reason string) {
	pollSkipLog.mu.Lock()
	now := time.Now()
	if now.Sub(pollSkipLog.last) < pollSkipLogInterval {
		pollSkipLog.suppressed++
		pollSkipLog.mu.Unlock()
		return
	}
	suppressed := pollSkipLog.suppressed
	pollSkipLog.last = now
	pollSkipLog.suppressed = 0
	pollSkipLog.mu.Unlock()
	logger.LogWarn(ctx, common.LogText("task poll skipped: channel #%d %s (%d more skips suppressed in the last %s)",
		channelID, reason, suppressed, pollSkipLogInterval))
}
