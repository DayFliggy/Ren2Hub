package model

import (
	"github.com/QuantumNous/new-api/common"
)

// Bounds the breaker rows read per request; a mass outage still renders.
const opsUnhealthyScanLimit = 500

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
		Total  int64
	}
	if err := DB.Model(&Channel{}).Select("status, COUNT(*) AS total").Group("status").Scan(&channelRows).Error; err != nil {
		return result, err
	}
	for _, row := range channelRows {
		result.Channels.Total += row.Total
		switch row.Status {
		case common.ChannelStatusEnabled:
			result.Channels.Enabled = row.Total
		case common.ChannelStatusManuallyDisabled:
			result.Channels.ManuallyDisabled = row.Total
		case common.ChannelStatusAutoDisabled:
			result.Channels.AutoDisabled = row.Total
		}
	}
	var breakerRows []struct {
		State string
		Total int64
	}
	if err := DB.Model(&ChannelHealth{}).Select("state, COUNT(*) AS total").Group("state").Scan(&breakerRows).Error; err != nil {
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

func readOpsUnhealthyRoutes(limit int) ([]OpsUnhealthyRoute, error) {
	var rows []ChannelHealth
	if err := DB.Where("state <> ?", RouteHealthStateClosed).Order("updated_at DESC").Limit(opsUnhealthyScanLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	type routeKey struct {
		channelID int
		model     string
	}
	grouped := map[routeKey]*OpsUnhealthyRoute{}
	order := make([]routeKey, 0, len(rows))
	for _, row := range rows {
		key := routeKey{row.ChannelID, row.Model}
		route, seen := grouped[key]
		if !seen {
			// Rows arrive newest first, so the first row carries the latest latency.
			route = &OpsUnhealthyRoute{ChannelID: row.ChannelID, Model: row.Model, State: row.State, UpdatedAt: row.UpdatedAt,
				LastLatencyMS: opsMeasuredLatency(row.LastLatencyMS), FirstTokenLatencyMS: opsMeasuredLatency(row.FirstTokenLatencyMS)}
			grouped[key] = route
			order = append(order, key)
		}
		if row.State == RouteHealthStateOpen {
			route.State = RouteHealthStateOpen
		}
		route.FailureCount = max(route.FailureCount, row.FailureCount)
		route.CooldownUntil = max(route.CooldownUntil, row.CooldownUntil)
	}
	if len(order) > limit {
		order = order[:limit]
	}
	routes := make([]OpsUnhealthyRoute, 0, len(order))
	channelIDs := make([]int, 0, len(order))
	for _, key := range order {
		routes = append(routes, *grouped[key])
		channelIDs = append(channelIDs, key.channelID)
	}
	if len(channelIDs) == 0 {
		return routes, nil
	}
	var channels []Channel
	if err := DB.Select("id", "name").Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return nil, err
	}
	names := make(map[int]string, len(channels))
	for _, channel := range channels {
		names[channel.Id] = channel.Name
	}
	for i := range routes {
		routes[i].ChannelName = names[routes[i].ChannelID]
	}
	return routes, nil
}
