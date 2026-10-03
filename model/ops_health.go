package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type OpsChannelCounts struct {
	Total            int64 `json:"total"`
	Enabled          int64 `json:"enabled"`
	ManuallyDisabled int64 `json:"manually_disabled"`
	AutoDisabled     int64 `json:"auto_disabled"`
}

type OpsBreakerCounts struct {
	Closed   int64 `json:"closed"`
	Open     int64 `json:"open"`
	HalfOpen int64 `json:"half_open"`
}

type OpsUnhealthyRoute struct {
	ChannelID           int    `json:"channel_id"`
	ChannelName         string `json:"channel_name"`
	Model               string `json:"model"`
	State               string `json:"state"`
	FailureCount        int    `json:"failure_count"`
	CooldownUntil       int64  `json:"cooldown_until"`
	LastLatencyMS       *int64 `json:"last_latency_ms"`
	FirstTokenLatencyMS *int64 `json:"first_token_latency_ms"`
	UpdatedAt           int64  `json:"updated_at"`
}

// Breaker rows store 0 for an unmeasured latency, as route health scoring
// treats it; the projection reports that as null.
func opsMeasuredLatency(ms int64) *int64 {
	if ms <= 0 {
		return nil
	}
	return &ms
}

type OpsChannelHealth struct {
	Channels  OpsChannelCounts    `json:"channels"`
	Breakers  OpsBreakerCounts    `json:"breakers"`
	Unhealthy []OpsUnhealthyRoute `json:"unhealthy"`
}

// ReadOpsChannelHealth counts channels and breaker rows, and lists non-closed
// breakers grouped by channel and model. Breaker rows are per credential key
// scope, which is never exposed; the worst state of a group wins.
func ReadOpsChannelHealth(limit int) (OpsChannelHealth, error) {
	var result OpsChannelHealth
	var channelRows []struct {
		Status int
		Type   int
		Total  int64
	}
	if err := DB.Model(&Channel{}).Select("status, type, COUNT(*) AS total").Group("status, type").Scan(&channelRows).Error; err != nil {
		return result, err
	}
	// Counts use the same projection as the channel list: a retired row that
	// still stores status 1 is counted as manually disabled.
	for _, row := range channelRows {
		result.Channels.Total += row.Total
		switch (&Channel{Status: row.Status, Type: row.Type}).EffectiveStatus() {
		case common.ChannelStatusEnabled:
			result.Channels.Enabled += row.Total
		case common.ChannelStatusManuallyDisabled:
			result.Channels.ManuallyDisabled += row.Total
		case common.ChannelStatusAutoDisabled:
			result.Channels.AutoDisabled += row.Total
		}
	}
	var breakerRows []struct {
		State string
		Total int64
	}
	if err := opsCurrentChannelHealth().Select("state, COUNT(*) AS total").Group("state").Scan(&breakerRows).Error; err != nil {
		return result, err
	}
	for _, row := range breakerRows {
		switch row.State {
		case RouteHealthStateClosed:
			result.Breakers.Closed = row.Total
		case RouteHealthStateOpen:
			result.Breakers.Open = row.Total
		case RouteHealthStateHalfOpen:
			result.Breakers.HalfOpen = row.Total
		}
	}
	unhealthy, err := readOpsUnhealthyRoutes(limit)
	result.Unhealthy = unhealthy
	return result, err
}

// Deleting a channel retains its historical health rows; only channels that
// still exist contribute to the current operational projection.
func opsCurrentChannelHealth() *gorm.DB {
	return DB.Model(&ChannelHealth{}).Where("channel_id IN (?)", DB.Model(&Channel{}).Select("id"))
}

func readOpsUnhealthyRoutes(limit int) ([]OpsUnhealthyRoute, error) {
	routes := []OpsUnhealthyRoute{}
	if limit <= 0 {
		return routes, nil
	}
	// Limit route groups, not credential rows: every scope contributes to the
	// worst state even when one channel has many keys. Only selected groups and
	// their newest latency observation are returned to the process.
	groups := opsCurrentChannelHealth().
		Select(`channel_id, model,
			CASE WHEN MAX(CASE WHEN state = ? THEN 1 ELSE 0 END) = 1 THEN ? ELSE ? END AS state,
			MAX(failure_count) AS failure_count, MAX(cooldown_until) AS cooldown_until, MAX(updated_at) AS updated_at`,
			RouteHealthStateOpen, RouteHealthStateOpen, RouteHealthStateHalfOpen).
		Where("state <> ?", RouteHealthStateClosed).
		Group("channel_id, model").Order("updated_at DESC, channel_id ASC, model ASC").Limit(limit)
	if err := DB.Table("(?) AS route_health", groups).
		Select("route_health.*, channels.name AS channel_name, latest.last_latency_ms, latest.first_token_latency_ms").
		Joins(`LEFT JOIN channel_healths AS latest ON latest.id = (
			SELECT id FROM channel_healths WHERE channel_id = route_health.channel_id
			AND model = route_health.model AND state <> ? ORDER BY updated_at DESC, id DESC LIMIT 1)`, RouteHealthStateClosed).
		Joins("LEFT JOIN channels ON channels.id = route_health.channel_id").
		Order("route_health.updated_at DESC, route_health.channel_id ASC, route_health.model ASC").Scan(&routes).Error; err != nil {
		return nil, err
	}
	for i := range routes {
		if routes[i].LastLatencyMS != nil {
			routes[i].LastLatencyMS = opsMeasuredLatency(*routes[i].LastLatencyMS)
		}
		if routes[i].FirstTokenLatencyMS != nil {
			routes[i].FirstTokenLatencyMS = opsMeasuredLatency(*routes[i].FirstTokenLatencyMS)
		}
	}
	return routes, nil
}
