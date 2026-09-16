package service

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var ErrGatewayTotalRateLimit = errors.New("gateway logical request limit exceeded")
var ErrGatewaySuccessRateLimit = errors.New("gateway successful request limit exceeded")
var ErrGatewayReservationLost = errors.New("gateway success reservation expired")

type GatewayRequestReservation struct {
	Key    string
	ID     string
	Window time.Duration
}

func StartGatewayRequestReservationRenewal(ctx context.Context, client *redis.Client, reservation GatewayRequestReservation) AttemptMaintenance {
	return StartAttemptMaintenance(ctx, GatewayReservationRenewInterval, func(ctx context.Context) error {
		return RenewGatewayRequestReservation(ctx, client, reservation)
	})
}

// ReserveGatewayRequest counts one logical request independently of the
// successful-request reservation. Failed requests consume only the total limit.
func ReserveGatewayRequest(ctx context.Context, client *redis.Client, key, requestID string, window time.Duration, totalLimit, successLimit int) (GatewayRequestReservation, error) {
	if client == nil || key == "" || requestID == "" || window < time.Millisecond || totalLimit < 0 || successLimit < 0 {
		return GatewayRequestReservation{}, ErrGatewayStorageUnavailable
	}
	if totalLimit > 0 {
		allowed, err := gatewayTotalRateScript.Run(ctx, client, []string{key + ":total"}, requestID, window.Milliseconds(), totalLimit).Int()
		if err != nil {
			return GatewayRequestReservation{}, errors.Join(ErrGatewayStorageUnavailable, err)
		}
		if allowed != 1 {
			return GatewayRequestReservation{}, ErrGatewayTotalRateLimit
		}
	}
	if successLimit == 0 {
		if err := client.Ping(ctx).Err(); err != nil {
			return GatewayRequestReservation{}, errors.Join(ErrGatewayStorageUnavailable, err)
		}
		return GatewayRequestReservation{}, nil
	}
	reservation := GatewayRequestReservation{Key: key, ID: uuid.NewString(), Window: window}
	allowed, err := gatewaySuccessReserveScript.Run(ctx, client, []string{key + ":success", key + ":pending"}, reservation.ID, window.Milliseconds(), successLimit, GatewayReservationTTL.Milliseconds()).Int()
	if err != nil {
		return GatewayRequestReservation{}, errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if allowed != 1 {
		return GatewayRequestReservation{}, ErrGatewaySuccessRateLimit
	}
	return reservation, nil
}

func RenewGatewayRequestReservation(ctx context.Context, client *redis.Client, reservation GatewayRequestReservation) error {
	if reservation.ID == "" {
		return nil
	}
	if client == nil {
		return ErrGatewayStorageUnavailable
	}
	result, err := gatewaySuccessRenewScript.Run(ctx, client, []string{reservation.Key + ":pending"}, reservation.ID, GatewayReservationTTL.Milliseconds()).Int()
	if err != nil {
		return errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if result != 1 {
		return ErrGatewayReservationLost
	}
	return nil
}

func FinishGatewayRequestReservation(ctx context.Context, client *redis.Client, reservation GatewayRequestReservation, success bool) error {
	if reservation.ID == "" {
		return nil
	}
	if client == nil {
		return ErrGatewayStorageUnavailable
	}
	result, err := gatewaySuccessFinishScript.Run(ctx, client, []string{reservation.Key + ":success", reservation.Key + ":pending"}, reservation.ID, reservation.Window.Milliseconds(), success).Int()
	if err != nil {
		return errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if result != 1 {
		return ErrGatewayReservationLost
	}
	return nil
}

var gatewayTotalRateScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local window, limit = tonumber(ARGV[2]), tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 1 end
if redis.call('ZCARD', KEYS[1]) >= limit then return 0 end
redis.call('ZADD', KEYS[1], now, ARGV[1])
redis.call('PEXPIRE', KEYS[1], window)
return 1
`)

var gatewaySuccessReserveScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local window, limit, ttl = tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
if redis.call('ZCARD', KEYS[1]) + redis.call('ZCARD', KEYS[2]) >= limit then return 0 end
redis.call('ZADD', KEYS[2], now + ttl, ARGV[1])
redis.call('PEXPIRE', KEYS[2], ttl)
return 1
`)

var gatewaySuccessRenewScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local expiry = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not expiry or tonumber(expiry) <= now then return 0 end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), ARGV[1])
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]))
return 1
`)

var gatewaySuccessFinishScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local window = tonumber(ARGV[2])
local expiry = redis.call('ZSCORE', KEYS[2], ARGV[1])
if not expiry or tonumber(expiry) <= now then
  if ARGV[3] ~= '1' then return 1 end
  if redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 1 end
  return 0
end
redis.call('ZREM', KEYS[2], ARGV[1])
if ARGV[3] == '1' then
  redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
  redis.call('ZADD', KEYS[1], now, ARGV[1])
  redis.call('PEXPIRE', KEYS[1], window)
end
return 1
`)
