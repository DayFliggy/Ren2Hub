package service

import (
	"context"
	"sync"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// 供应商任务 ID 只在渠道内唯一。每个调用方拿到独立索引，批量和单任务
// 解析都不能把另一个渠道的响应应用到本渠道任务上。
func dispatchChannelScopedTasks(ctx context.Context, platform constant.TaskPlatform, channels map[int][]string, tasks map[int]map[string]*model.Task) {
	var workers sync.WaitGroup
	for channelID, ids := range channels {
		if ctx.Err() != nil {
			break
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			DispatchPlatformUpdate(ctx, platform, map[int][]string{channelID: ids}, tasks[channelID])
		}()
	}
	workers.Wait()
}
