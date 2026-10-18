package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
)

var homeMetricsCache struct {
	sync.Mutex
	value   model.HomeRequestMetrics
	expires time.Time
}
var homeMetricsRefresh singleflight.Group

func HomeMetrics(ctx context.Context) (model.HomeRequestMetrics, error) {
	read := func() (model.HomeRequestMetrics, bool) {
		homeMetricsCache.Lock()
		defer homeMetricsCache.Unlock()
		return homeMetricsCache.value, time.Now().Before(homeMetricsCache.expires) && homeMetricsCache.value.Available == common.LogConsumeEnabled
	}
	if value, ok := read(); ok {
		return value, nil
	}
	// Sharing the query prevents each anonymous visitor from scanning logs. The
	// first caller's cancellation propagates as an error; errors never populate cache.
	result := homeMetricsRefresh.DoChan("home", func() (any, error) {
		if value, ok := read(); ok {
			return value, nil
		}
		value, err := model.GetHomeRequestMetricsWithContext(ctx, time.Now())
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
