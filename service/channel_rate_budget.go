package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

var (
	ErrChannelRPM = errors.New("channel_rpm_exceeded")
	ErrChannelTPM = errors.New("channel_tpm_exceeded")
	// ErrChannelTPMUnbounded marks a request whose token ceiling cannot be
	// determined. Such a request must not enter a TPM-enabled candidate,
	// because reserving an unknown amount would either overcount every other
	// caller out of the window or let this one through unmetered.
	ErrChannelTPMUnbounded = errors.New("channel_tpm_upper_bound_unknown")
)

// channelRateWindow is the fixed-window width. Redis server time drives the
// bucket so every instance agrees on the boundary without clock sync.
const channelRateWindow = time.Minute

// ChannelRateReservation is the amount actually charged to a channel window.
// It is returned so the caller can correct the token estimate against real
// usage and release the reservation if the send never happened.
type ChannelRateReservation struct {
	ChannelID      int
	Bucket         int64
	ReservedTokens int
	RPMCounted     bool
	TPMEnabled     bool
}

// ChannelRateBudget is the channel's configured window limits. Zero means
// unlimited for either dimension.
type ChannelRateBudget struct {
	RPM int
	TPM int
}

func (b ChannelRateBudget) enabled() bool { return b.RPM > 0 || b.TPM > 0 }

func channelRateKeys(channelID int) (string, string) {
	base := fmt.Sprintf("route:budget:v1:channel:%d", channelID)
	return base + ":rpm", base + ":tpm"
}

// ReserveChannelRateBudget charges one request and its token ceiling to the
// current window. It is called after the capacity lease is held and
// immediately before the upstream send, so a capacity rejection never consumes
// send budget.
//
// tokenCeiling must be the estimated input plus the effective output ceiling.
// A non-positive ceiling with TPM enabled is rejected rather than assumed.
func ReserveChannelRateBudget(ctx context.Context, channelID int, budget ChannelRateBudget, tokenCeiling int) (ChannelRateReservation, error) {
	reservation := ChannelRateReservation{ChannelID: channelID, TPMEnabled: budget.TPM > 0}
	if !budget.enabled() {
		return reservation, nil
	}
	// An enabled hard limit must never degrade to per-node counting: without
	// Redis there is no shared window, so the request is refused instead of
	// being admitted against a local counter.
	if !common.RedisEnabled || common.RDB == nil {
		return reservation, ErrRouteLeaseUnavailable
	}
	if budget.TPM > 0 && tokenCeiling <= 0 {
		return reservation, ErrChannelTPMUnbounded
	}
	if budget.TPM > 0 && tokenCeiling > budget.TPM {
		// The request alone cannot fit the window, so no amount of waiting
		// helps. Report it as a bound failure rather than a transient
		// exhaustion so the caller does not retry the same candidate.
		return reservation, ErrChannelTPMUnbounded
	}

	rpmKey, tpmKey := channelRateKeys(channelID)
	status, err := channelRateReserveScript.Run(ctx, common.RDB, []string{rpmKey, tpmKey},
		budget.RPM, budget.TPM, tokenCeiling, int(channelRateWindow.Seconds())).Int64Slice()
	if err != nil {
		return reservation, fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	if len(status) != 2 {
		return reservation, ErrRouteLeaseUnavailable
	}
	reservation.Bucket = status[1]
	switch status[0] {
	case 1:
		reservation.RPMCounted = true
		if budget.TPM > 0 {
			reservation.ReservedTokens = tokenCeiling
		}
		return reservation, nil
	case -1:
		return reservation, ErrChannelRPM
	default:
		return reservation, ErrChannelTPM
	}
}

// SettleChannelRateBudget corrects a token reservation against real usage.
// Only the difference is applied, so the window reflects what the upstream
// actually consumed. A reservation that never sent is released in full by
// passing zero usage.
func SettleChannelRateBudget(ctx context.Context, reservation ChannelRateReservation, actualTokens int) error {
	if !reservation.TPMEnabled || reservation.ReservedTokens == 0 {
		return nil
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	delta := actualTokens - reservation.ReservedTokens
	if delta == 0 {
		return nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return ErrRouteLeaseUnavailable
	}
	_, tpmKey := channelRateKeys(reservation.ChannelID)
	if err := channelRateSettleScript.Run(ctx, common.RDB, []string{tpmKey},
		reservation.Bucket, delta, int(channelRateWindow.Seconds())).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	return nil
}

// ReleaseChannelRateBudget returns an unused reservation to the window. The
// request counter is not refunded when the send already happened; callers pass
// sent=false only for an attempt that never reached the upstream.
func ReleaseChannelRateBudget(ctx context.Context, reservation ChannelRateReservation, sent bool) error {
	if sent {
		return SettleChannelRateBudget(ctx, reservation, 0)
	}
	if !reservation.RPMCounted {
		return nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return ErrRouteLeaseUnavailable
	}
	rpmKey, tpmKey := channelRateKeys(reservation.ChannelID)
	if err := channelRateReleaseScript.Run(ctx, common.RDB, []string{rpmKey, tpmKey},
		reservation.Bucket, reservation.ReservedTokens, int(channelRateWindow.Seconds())).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	return nil
}

// Both dimensions are checked before either is incremented, so a rejected
// request leaves no partial charge behind. The bucket is derived from Redis
// TIME so all instances share one window.
var channelRateReserveScript = redis.NewScript(`
local parts = redis.call('TIME')
local bucket = math.floor(tonumber(parts[1]) / tonumber(ARGV[4]))
local rpm_limit, tpm_limit = tonumber(ARGV[1]), tonumber(ARGV[2])
local tokens, window = tonumber(ARGV[3]), tonumber(ARGV[4])
local rpm_key = KEYS[1] .. ':' .. bucket
local tpm_key = KEYS[2] .. ':' .. bucket
if rpm_limit > 0 then
  local used = tonumber(redis.call('GET', rpm_key) or '0')
  if used + 1 > rpm_limit then return {-1, bucket} end
end
if tpm_limit > 0 then
  local used = tonumber(redis.call('GET', tpm_key) or '0')
  if used + tokens > tpm_limit then return {-2, bucket} end
end
if rpm_limit > 0 then
  redis.call('INCRBY', rpm_key, 1)
  redis.call('EXPIRE', rpm_key, window * 2)
end
if tpm_limit > 0 then
  redis.call('INCRBY', tpm_key, tokens)
  redis.call('EXPIRE', tpm_key, window * 2)
end
return {1, bucket}
`)

// The correction targets the original bucket, so a call that spans a window
// boundary adjusts the window it was charged to instead of the current one.
var channelRateSettleScript = redis.NewScript(`
local tpm_key = KEYS[1] .. ':' .. ARGV[1]
local delta, window = tonumber(ARGV[2]), tonumber(ARGV[3])
if redis.call('EXISTS', tpm_key) == 0 then return 0 end
local updated = redis.call('INCRBY', tpm_key, delta)
if updated < 0 then redis.call('SET', tpm_key, 0) end
redis.call('EXPIRE', tpm_key, window * 2)
return 1
`)

var channelRateReleaseScript = redis.NewScript(`
local rpm_key = KEYS[1] .. ':' .. ARGV[1]
local tpm_key = KEYS[2] .. ':' .. ARGV[1]
local tokens, window = tonumber(ARGV[2]), tonumber(ARGV[3])
if redis.call('EXISTS', rpm_key) == 1 then
  local updated = redis.call('INCRBY', rpm_key, -1)
  if updated < 0 then redis.call('SET', rpm_key, 0) end
  redis.call('EXPIRE', rpm_key, window * 2)
end
if tokens > 0 and redis.call('EXISTS', tpm_key) == 1 then
  local updated = redis.call('INCRBY', tpm_key, -tokens)
  if updated < 0 then redis.call('SET', tpm_key, 0) end
  redis.call('EXPIRE', tpm_key, window * 2)
end
return 1
`)
