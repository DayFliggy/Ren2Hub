package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
)

// homeMetricsQueryTimeout bounds the shared query, which no single caller owns.
const homeMetricsQueryTimeout = 10 * time.Second

var homeMetricsCache struct {
	sync.Mutex
	value   model.HomeRequestMetrics
	expires time.Time
}
var homeMetricsRefresh singleflight.Group
var readHomeMetrics = model.GetHomeRequestMetricsWithContext

func HomeMetrics(ctx context.Context) (model.HomeRequestMetrics, error) {
	read := func() (model.HomeRequestMetrics, bool) {
		homeMetricsCache.Lock()
		defer homeMetricsCache.Unlock()
		return homeMetricsCache.value, time.Now().Before(homeMetricsCache.expires) && homeMetricsCache.value.Available == common.LogConsumeEnabled
	}
	if value, ok := read(); ok {
		return value, nil
	}
	// 合并查询避免每个匿名访客各扫一次日志。查询使用独立 context：第一个调用方
	// 取消只让它自己返回，同批等待者仍拿到结果；错误不写入缓存。
	result := homeMetricsRefresh.DoChan("home", func() (any, error) {
		if value, ok := read(); ok {
			return value, nil
		}
		queryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), homeMetricsQueryTimeout)
		defer cancel()
		value, err := readHomeMetrics(queryCtx, time.Now())
		if err != nil {
			return nil, err
		}
		homeMetricsCache.Lock()
		homeMetricsCache.value = value
		homeMetricsCache.expires = time.Now().Add(15 * time.Second)
		homeMetricsCache.Unlock()
		return value, nil
	})
	select {
	case <-ctx.Done():
		return model.HomeRequestMetrics{}, ctx.Err()
	case response := <-result:
		if response.Err != nil {
			return model.HomeRequestMetrics{}, response.Err
		}
		return response.Val.(model.HomeRequestMetrics), nil
	}
}
