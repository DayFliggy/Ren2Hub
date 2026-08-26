package service

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	routeShadowAggregatePrefix    = "route:shadow:aggregate:v1:"
	routeShadowAggregateRetention = 8 * 24 * time.Hour
)

type routeShadowAggregateModel struct {
	Decisions uint64
	Resolved  uint64
}

type routeShadowAggregateSnapshot struct {
	Models    map[string]routeShadowAggregateModel
	Available bool
}

// recordRouteShadowAggregate stores only low-cardinality, model-scoped
// counters. Redis is used as the cross-instance diagnostic store; a failure is
// deliberately non-fatal because Shadow must never alter request behavior.
func recordRouteShadowAggregate(decision RouteShadowDecision) {
	if common.RDB == nil || !common.RedisEnabled || strings.TrimSpace(decision.NormalizedRequestModel) == "" {
		return
	}
	bucket := time.Now().UTC().Truncate(time.Hour).Unix()
	key := routeShadowAggregatePrefix + strconv.FormatInt(bucket, 10)
	modelName := decision.NormalizedRequestModel
	modelField := base64.RawURLEncoding.EncodeToString([]byte(modelName))
	pipe := common.RDB.Pipeline()
	pipe.HIncrBy(context.Background(), key, "decisions", 1)
	pipe.HIncrBy(context.Background(), key, "model:"+modelField+":decisions", 1)
	if decision.LabSlug != "" {
		pipe.HIncrBy(context.Background(), key, "model:"+modelField+":resolved", 1)
	}
	pipe.Expire(context.Background(), key, routeShadowAggregateRetention)
	if _, err := pipe.Exec(context.Background()); err != nil {
		routeShadowMetrics.AggregateWriteFailures.Add(1)
	}
}

func loadRouteShadowAggregate(ctx context.Context, since time.Time) routeShadowAggregateSnapshot {
	snapshot := routeShadowAggregateSnapshot{Models: make(map[string]routeShadowAggregateModel)}
	if common.RDB == nil || !common.RedisEnabled {
		return snapshot
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for bucket := since.UTC().Truncate(time.Hour); !bucket.After(time.Now().UTC()); bucket = bucket.Add(time.Hour) {
		key := routeShadowAggregatePrefix + strconv.FormatInt(bucket.Unix(), 10)
		values, err := common.RDB.HGetAll(ctx, key).Result()
		if err != nil {
			return routeShadowAggregateSnapshot{Models: make(map[string]routeShadowAggregateModel)}
		}
		if len(values) == 0 {
			continue
		}
		snapshot.Available = true
		for field, value := range values {
			if !strings.HasPrefix(field, "model:") {
				continue
			}
			parts := strings.Split(field, ":")
			if len(parts) != 3 || parts[1] == "" {
				continue
			}
			modelBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil || len(modelBytes) == 0 {
				continue
			}
			modelName := string(modelBytes)
			count, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				continue
			}
			stats := snapshot.Models[modelName]
			switch parts[2] {
			case "decisions":
				stats.Decisions += count
			case "resolved":
				stats.Resolved += count
			default:
				continue
			}
			snapshot.Models[modelName] = stats
		}
	}
	return snapshot
}
