package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var ErrGatewayProviderRateLimit = errors.New("gateway upstream rate limit exceeded")

// ID is a stable channel or credential identity, never a credential
// secret or its mutable list index. Scopes are atomically admitted together.
type GatewayProviderRateScope struct {
	ID  string
	RPM int64
	TPM int64
}

type GatewayProviderRateReservation struct {
	ID              string
	Scopes          []GatewayProviderRateScope
	EstimatedTokens int64
}

func gatewayProviderRateKeys(reservation GatewayProviderRateReservation) []string {
	keys := make([]string, 0, len(reservation.Scopes)*5+1)
	for _, scope := range reservation.Scopes {
		digest := sha256.Sum256([]byte(scope.ID))
		prefix := "gateway:provider:v1:" + hex.EncodeToString(digest[:])
		keys = append(keys, prefix+":usage", prefix+":rpm", prefix+":pending", prefix+":amounts", prefix+":cooldown")
	}
	return append(keys, "gateway:provider:v1:reservation:"+reservation.ID)
}

func ReserveGatewayProviderRate(ctx context.Context, client *redis.Client, scopes []GatewayProviderRateScope, estimatedTokens int64) (GatewayProviderRateReservation, error) {
	if client == nil || len(scopes) == 0 || len(scopes) > 3 || estimatedTokens < 0 || estimatedTokens > 1_000_000_000 {
		return GatewayProviderRateReservation{}, ErrGatewayStorageUnavailable
	}
	seen := make(map[string]bool)
	reservation := GatewayProviderRateReservation{ID: uuid.NewString(), Scopes: append([]GatewayProviderRateScope(nil), scopes...), EstimatedTokens: estimatedTokens}
	args := []any{reservation.ID, estimatedTokens, GatewayReservationTTL.Milliseconds()}
	for _, scope := range scopes {
		if scope.ID == "" || len(scope.ID) > 512 || seen[scope.ID] || scope.RPM < 0 || scope.TPM < 0 || scope.RPM > 1_000_000_000 || scope.TPM > 1_000_000_000 {
			return GatewayProviderRateReservation{}, ErrAdmissionRuntime
		}
		seen[scope.ID] = true
		args = append(args, scope.RPM, scope.TPM)
	}
	result, err := gatewayProviderReserveScript.Run(ctx, client, gatewayProviderRateKeys(reservation), args...).Int()
	if err != nil {
		return GatewayProviderRateReservation{}, errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if result != 1 {
		return GatewayProviderRateReservation{}, ErrGatewayProviderRateLimit
	}
	return reservation, nil
}

func RenewGatewayProviderRate(ctx context.Context, client *redis.Client, reservation GatewayProviderRateReservation) error {
	if client == nil || reservation.ID == "" || len(reservation.Scopes) == 0 {
		return ErrGatewayStorageUnavailable
	}
	result, err := gatewayProviderRenewScript.Run(ctx, client, gatewayProviderRateKeys(reservation), reservation.ID, GatewayReservationTTL.Milliseconds()).Int()
	if err != nil {
		return errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if result != 1 {
		return ErrGatewayReservationLost
	}
	return nil
}

// Actual tokens should be the provider's final usage. Unknown usage after a
// sent attempt retains the conservative estimate; pre-send failures refund RPM.
func FinishGatewayProviderRate(ctx context.Context, client *redis.Client, reservation GatewayProviderRateReservation, actualTokens int64, upstreamStarted, usageKnown bool, retryAfter time.Duration) error {
	if client == nil || reservation.ID == "" || len(reservation.Scopes) == 0 || actualTokens < 0 || actualTokens > 1_000_000_000 {
		return ErrGatewayStorageUnavailable
	}
	if upstreamStarted && !usageKnown {
		actualTokens = reservation.EstimatedTokens
	}
	if !upstreamStarted {
		retryAfter = 0
	}
	result, err := gatewayProviderFinishScript.Run(ctx, client, gatewayProviderRateKeys(reservation), reservation.ID, actualTokens, upstreamStarted, max(retryAfter.Milliseconds(), 0)).Int()
	if err != nil {
		return errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if result != 1 {
		return ErrGatewayReservationLost
	}
	return nil
}

func GatewayChannelRateScopeID(channelID int) string {
	return "channel:" + strconv.Itoa(channelID)
}

func GatewayCredentialRateScopeID(credentialID string) string {
	return "credential:" + credentialID
}

func ResolveGatewayProviderRateScopes(config model.GatewayConfig, channelID int, credentialID string) ([]GatewayProviderRateScope, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if channelID <= 0 || credentialID == "" {
		return nil, ErrAdmissionRuntime
	}
	channelLimit, credentialLimit := config.ChannelRates[channelID], config.CredentialRates[credentialID]
	return []GatewayProviderRateScope{
		{ID: GatewayChannelRateScopeID(channelID), RPM: channelLimit.RPM, TPM: channelLimit.TPM},
		{ID: GatewayCredentialRateScopeID(credentialID), RPM: credentialLimit.RPM, TPM: credentialLimit.TPM},
	}, nil
}

func LoadGatewayProviderRateScopes(ctx context.Context, channelID int, credentialID string) ([]GatewayProviderRateScope, error) {
	config, err := model.GetGatewayConfigContext(ctx)
	if err != nil {
		return nil, err
	}
	return ResolveGatewayProviderRateScopes(config, channelID, credentialID)
}

var gatewayProviderReserveScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local id, estimate, ttl = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
local count = (#KEYS - 1) / 5
for scope = 1, count do
  local base = (scope - 1) * 5
  local usage, rpm, pending, amounts, cooldown = KEYS[base+1], KEYS[base+2], KEYS[base+3], KEYS[base+4], KEYS[base+5]
  redis.call('ZREMRANGEBYSCORE', rpm, '-inf', now - 60000)
  local expired = redis.call('ZRANGEBYSCORE', pending, '-inf', now)
  for _, old in ipairs(expired) do redis.call('ZREM', pending, old); redis.call('HDEL', amounts, old) end
  local completed = redis.call('ZRANGEBYSCORE', usage .. ':events', '-inf', now - 60000)
  for _, old in ipairs(completed) do redis.call('ZREM', usage .. ':events', old); redis.call('HDEL', usage, old) end
  local spent = 0
  for _, amount in ipairs(redis.call('HVALS', usage)) do spent = spent + tonumber(amount) end
  for _, amount in ipairs(redis.call('HVALS', amounts)) do spent = spent + tonumber(amount) end
  local rpm_limit, tpm_limit = tonumber(ARGV[2*scope+2]), tonumber(ARGV[2*scope+3])
  if tonumber(redis.call('GET', cooldown) or '0') > now then return 0 end
  if rpm_limit > 0 and redis.call('ZCARD', rpm) >= rpm_limit then return 0 end
  if tpm_limit > 0 and spent + estimate > tpm_limit then return 0 end
end
for scope = 1, count do
  local base = (scope - 1) * 5
  redis.call('ZADD', KEYS[base+2], now, id)
  redis.call('PEXPIRE', KEYS[base+2], 60000)
  redis.call('ZADD', KEYS[base+3], now + ttl, id)
  redis.call('HSET', KEYS[base+4], id, estimate)
  redis.call('PEXPIRE', KEYS[base+3], ttl)
  redis.call('PEXPIRE', KEYS[base+4], ttl)
end
redis.call('SET', KEYS[#KEYS], 'reserved', 'PX', ttl)
return 1
`)

var gatewayProviderRenewScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
if redis.call('GET', KEYS[#KEYS]) ~= 'reserved' then return 0 end
for base = 0, #KEYS - 2, 5 do
  local expiry = redis.call('ZSCORE', KEYS[base+3], ARGV[1])
  if not expiry or tonumber(expiry) <= now then return 0 end
end
for base = 0, #KEYS - 2, 5 do
  redis.call('ZADD', KEYS[base+3], now + tonumber(ARGV[2]), ARGV[1])
  redis.call('PEXPIRE', KEYS[base+3], tonumber(ARGV[2]))
  redis.call('PEXPIRE', KEYS[base+4], tonumber(ARGV[2]))
end
redis.call('PEXPIRE', KEYS[#KEYS], tonumber(ARGV[2]))
return 1
`)

var gatewayProviderFinishScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local state = redis.call('GET', KEYS[#KEYS])
if state == 'finished' then return 1 end
if state ~= 'reserved' then return 0 end
for base = 0, #KEYS - 2, 5 do
  local expiry = redis.call('ZSCORE', KEYS[base+3], ARGV[1])
  if not expiry or tonumber(expiry) <= now then return 0 end
end
for base = 0, #KEYS - 2, 5 do
  redis.call('ZREM', KEYS[base+3], ARGV[1])
  redis.call('HDEL', KEYS[base+4], ARGV[1])
  if ARGV[3] ~= '1' then
    redis.call('ZREM', KEYS[base+2], ARGV[1])
  else
    redis.call('HSET', KEYS[base+1], ARGV[1], ARGV[2])
    redis.call('ZADD', KEYS[base+1] .. ':events', now, ARGV[1])
    redis.call('PEXPIRE', KEYS[base+1], 60000)
    redis.call('PEXPIRE', KEYS[base+1] .. ':events', 60000)
  end
  local cooldown = tonumber(ARGV[4])
  if cooldown > 0 then
    local until_ms = math.max(tonumber(redis.call('GET', KEYS[base+5]) or '0'), now + cooldown)
    redis.call('SET', KEYS[base+5], until_ms, 'PX', until_ms - now)
  end
end
redis.call('SET', KEYS[#KEYS], 'finished', 'PX', 120000)
return 1
`)
