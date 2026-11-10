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

// errPollChannelDeleted 表示准入时数据库已没有这个渠道，请求没有发出。调用方用自己持有的
// 任务走渠道缺失流程（failTasksWithoutChannel），不计入上游轮询失败。
var errPollChannelDeleted = errors.New("task poll channel deleted before send")

// doPollRequest 在渠道并发许可下发送一次轮询请求。skip 为 true 时调用方应立即返回 err：
// 满载或上限读不到时跳过，err 为 nil；轮询被取消时 err 为 ctx.Err()。准入时渠道已删除返回
// errPollChannelDeleted。其余情况 resp 和 err 是上游请求的结果。准入阶段的错误只来自
// AcquireChannelConcurrency，发送阶段不会产生这些哨兵错误。
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
		return nil, false, errPollChannelDeleted
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
