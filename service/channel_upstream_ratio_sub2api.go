package service

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 本文件移植 Sub2API backend/internal/service/upstream_billing_probe.go 的响应校验、
// 高峰系数和退避算法（参考 sub2api 2026-10-27 快照），语义保持一致。

const (
	upstreamRatioIntervalMinutes      = 30
	upstreamRatioMinIntervalMinutes   = 5
	upstreamRatioMaxDelay             = 24 * time.Hour
	upstreamRatioUnsupportedFactor    = 8
	upstreamRatioMaxBackoffShift      = 6
	upstreamRatioSub2APIBillingPath   = "/v1/sub2api/billing"
	upstreamRatioSub2APIBillingObject = "sub2api.key_billing"
)

type sub2apiBillingResponse struct {
	Object                  string   `json:"object"`
	SchemaVersion           int      `json:"schema_version"`
	BillingScope            string   `json:"billing_scope"`
	GroupRateMultiplier     *float64 `json:"group_rate_multiplier"`
	UserRateMultiplier      *float64 `json:"user_rate_multiplier"`
	ResolvedRateMultiplier  *float64 `json:"resolved_rate_multiplier"`
	PeakRateEnabled         *bool    `json:"peak_rate_enabled"`
	PeakStart               *string  `json:"peak_start"`
	PeakEnd                 *string  `json:"peak_end"`
	PeakRateMultiplier      *float64 `json:"peak_rate_multiplier"`
	AppliedPeakMultiplier   *float64 `json:"applied_peak_multiplier"`
	EffectiveRateMultiplier *float64 `json:"effective_rate_multiplier"`
	Timezone                *string  `json:"timezone"`
	ObservedAt              string   `json:"observed_at"`
}

// Sub2APIBilling 是校验后保存的声明。存 resolved 值和高峰规则，effective 在读取时按当前时间计算。
type Sub2APIBilling struct {
	GroupRate      float64  `json:"group_rate_multiplier"`
	UserRate       *float64 `json:"user_rate_multiplier,omitempty"`
	ResolvedRate   float64  `json:"resolved_rate_multiplier"`
	PeakEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart      string   `json:"peak_start,omitempty"`
	PeakEnd        string   `json:"peak_end,omitempty"`
	PeakRate       float64  `json:"peak_rate_multiplier"`
	Timezone       string   `json:"timezone,omitempty"`
	ObservedAtUnix int64    `json:"observed_at"`
}

func validBillingMultiplier(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func parseSub2APIBillingResponse(body []byte) (*Sub2APIBilling, error) {
	var response sub2apiBillingResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Object != upstreamRatioSub2APIBillingObject || response.SchemaVersion != 1 || response.BillingScope != "token" {
		return nil, fmt.Errorf("unexpected billing response schema")
	}
	if response.GroupRateMultiplier == nil || response.ResolvedRateMultiplier == nil ||
		response.PeakRateEnabled == nil || response.EffectiveRateMultiplier == nil {
		return nil, fmt.Errorf("incomplete billing response")
	}
	for _, value := range []float64{*response.GroupRateMultiplier, *response.ResolvedRateMultiplier, *response.EffectiveRateMultiplier} {
		if !validBillingMultiplier(value) {
			return nil, fmt.Errorf("invalid billing multiplier")
		}
	}
	if response.UserRateMultiplier != nil && !validBillingMultiplier(*response.UserRateMultiplier) {
		return nil, fmt.Errorf("invalid user billing multiplier")
	}
	expectedResolved := *response.GroupRateMultiplier
	if response.UserRateMultiplier != nil {
		expectedResolved = *response.UserRateMultiplier
	}
	if !equalBillingMultiplier(*response.ResolvedRateMultiplier, expectedResolved) {
		return nil, fmt.Errorf("inconsistent resolved billing multiplier")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, response.ObservedAt)
	if err != nil || observedAt.IsZero() {
		return nil, fmt.Errorf("invalid observed_at")
	}
	billing := &Sub2APIBilling{
		GroupRate:      *response.GroupRateMultiplier,
		UserRate:       response.UserRateMultiplier,
		ResolvedRate:   *response.ResolvedRateMultiplier,
		PeakEnabled:    *response.PeakRateEnabled,
		ObservedAtUnix: observedAt.Unix(),
	}
	if billing.PeakEnabled {
		if response.PeakStart == nil || response.PeakEnd == nil || response.Timezone == nil ||
			response.PeakRateMultiplier == nil || response.AppliedPeakMultiplier == nil ||
			*response.PeakStart == "" || *response.PeakEnd == "" || *response.Timezone == "" ||
			!validBillingMultiplier(*response.PeakRateMultiplier) || !validBillingMultiplier(*response.AppliedPeakMultiplier) {
			return nil, fmt.Errorf("incomplete peak billing response")
		}
		billing.PeakStart = *response.PeakStart
		billing.PeakEnd = *response.PeakEnd
		billing.PeakRate = *response.PeakRateMultiplier
		billing.Timezone = *response.Timezone
	}
	appliedPeak, ok := billing.peakMultiplierAt(observedAt)
	if !ok {
		return nil, fmt.Errorf("invalid peak billing response")
	}
	if billing.PeakEnabled {
		if !equalBillingMultiplier(*response.AppliedPeakMultiplier, appliedPeak) {
			return nil, fmt.Errorf("inconsistent applied peak multiplier")
		}
	} else if response.AppliedPeakMultiplier != nil && !equalBillingMultiplier(*response.AppliedPeakMultiplier, 1) {
		return nil, fmt.Errorf("inconsistent applied peak multiplier")
	}
	if !equalBillingMultiplier(*response.EffectiveRateMultiplier, *response.ResolvedRateMultiplier*appliedPeak) {
		return nil, fmt.Errorf("inconsistent effective billing multiplier")
	}
	return billing, nil
}

// effectiveRateAt 对应 Sub2API upstreamBillingRateAt。
func (b *Sub2APIBilling) effectiveRateAt(now time.Time) (float64, bool) {
	if !validBillingMultiplier(b.ResolvedRate) {
		return 0, false
	}
	appliedPeak, ok := b.peakMultiplierAt(now)
	if !ok {
		return 0, false
	}
	rate := b.ResolvedRate * appliedPeak
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	return rate, true
}

// peakMultiplierAt 对应 Sub2API upstreamBillingPeakMultiplierAt：同日左闭右开，start>=end 视为非法。
func (b *Sub2APIBilling) peakMultiplierAt(now time.Time) (float64, bool) {
	if !b.PeakEnabled {
		return 1, true
	}
	startMinute, validStart := parsePeakMinutes(b.PeakStart)
	endMinute, validEnd := parsePeakMinutes(b.PeakEnd)
	if !validStart || !validEnd || startMinute >= endMinute || !validBillingMultiplier(b.PeakRate) {
		return 0, false
	}
	location, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return 0, false
	}
	local := now.In(location)
	minute := local.Hour()*60 + local.Minute()
	if minute >= startMinute && minute < endMinute {
		return b.PeakRate, true
	}
	return 1, true
}

// parsePeakMinutes 对应 Sub2API service/group.go parseMinutes：小时 1–2 位，分钟固定 2 位。
func parsePeakMinutes(hhmm string) (int, bool) {
	colon := strings.IndexByte(hhmm, ':')
	if (colon != 1 && colon != 2) || len(hhmm)-colon-1 != 2 {
		return 0, false
	}
	h := 0
	for i := 0; i < colon; i++ {
		d := hhmm[i] - '0'
		if d > 9 {
			return 0, false
		}
		h = h*10 + int(d)
	}
	m1, m2 := hhmm[colon+1]-'0', hhmm[colon+2]-'0'
	if m1 > 9 || m2 > 9 {
		return 0, false
	}
	m := int(m1)*10 + int(m2)
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func equalBillingMultiplier(left, right float64) bool {
	if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) {
		return false
	}
	scale := math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
	return math.Abs(left-right) <= 1e-9*scale
}

func nextUpstreamRatioProbeDelay(intervalMinutes int, retryAfter time.Duration) time.Duration {
	retryAfter = min(max(retryAfter, 0), upstreamRatioMaxDelay)
	interval := time.Duration(intervalMinutes) * time.Minute
	interval = min(max(interval, upstreamRatioMinIntervalMinutes*time.Minute), upstreamRatioMaxDelay)
	jitterRange := min(interval/5, 5*time.Minute)
	if jitterRange > 0 {
		interval += time.Duration(rand.Int64N(int64(jitterRange)*2+1)) - jitterRange
	}
	if retryAfter > interval {
		return retryAfter
	}
	return min(interval, upstreamRatioMaxDelay)
}

// failedUpstreamRatioProbeDelay 按连续失败次数翻倍退避：第 1 次失败用基础间隔，之后每次翻倍，
// 最长 24 小时。Retry-After 更长时以它为准。
func failedUpstreamRatioProbeDelay(intervalMinutes, failureCount int, retryAfter time.Duration) time.Duration {
	shift := min(max(failureCount-1, 0), upstreamRatioMaxBackoffShift)
	delay := min(nextUpstreamRatioProbeDelay(intervalMinutes, 0)<<shift, upstreamRatioMaxDelay)
	return max(delay, min(max(retryAfter, 0), upstreamRatioMaxDelay))
}

// unsupportedUpstreamRatioProbeDelay 把不支持的渠道放大 8 倍重探，最长 24 小时。
func unsupportedUpstreamRatioProbeDelay(intervalMinutes int, retryAfter time.Duration) time.Duration {
	base := nextUpstreamRatioProbeDelay(intervalMinutes, retryAfter)
	if base >= upstreamRatioMaxDelay {
		return base
	}
	return min(base*upstreamRatioUnsupportedFactor, upstreamRatioMaxDelay)
}

func upstreamRatioRetryAfter(header http.Header, now time.Time) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	// 先按秒限幅再换算：超大秒数乘以 time.Second 会溢出；超出整数范围的数字同样表示等待上限。
	if seconds, err := strconv.ParseUint(value, 10, 64); (err == nil && seconds > 0) || errors.Is(err, strconv.ErrRange) {
		return time.Duration(min(seconds, uint64(upstreamRatioMaxDelay/time.Second))) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := at.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}
