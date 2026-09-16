package service

import (
	"context"
	"errors"

	"github.com/go-redis/redis/v8"
)

type GatewayProviderRateRuntime struct {
	RPMUsed        int64 `json:"rpm_used"`
	TokensUsed     int64 `json:"tokens_used"`
	TokensReserved int64 `json:"tokens_reserved"`
	CooldownMS     int64 `json:"cooldown_ms"`
}

func ReadGatewayAdmissionRuntime(resourceKey string) AdmissionSnapshot {
	return gatewayAdmission.Snapshot(resourceKey)
}
func ReadGatewayProviderRateRuntime(ctx context.Context, client *redis.Client, scopeID string) (GatewayProviderRateRuntime, error) {
	if client == nil || scopeID == "" || len(scopeID) > 512 {
		return GatewayProviderRateRuntime{}, ErrGatewayStorageUnavailable
	}
	keys := gatewayProviderRateKeys(GatewayProviderRateReservation{Scopes: []GatewayProviderRateScope{{ID: scopeID}}})
	values, err := gatewayProviderRuntimeScript.Run(ctx, client, keys[:5]).Int64Slice()
	if err != nil {
		return GatewayProviderRateRuntime{}, errors.Join(ErrGatewayStorageUnavailable, err)
	}
	if len(values) != 4 {
		return GatewayProviderRateRuntime{}, ErrGatewayStorageUnavailable
	}
	return GatewayProviderRateRuntime{RPMUsed: values[0], TokensUsed: values[1], TokensReserved: values[2], CooldownMS: values[3]}, nil
}

var gatewayProviderRuntimeScript = redis.NewScript(gatewayProviderRuntimeLua)

const gatewayProviderRuntimeLua = `
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local used, reserved = 0, 0
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[1] .. ':events', '(' .. (now - 60000), '+inf')) do
  used = used + tonumber(redis.call('HGET', KEYS[1], id) or '0')
end
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[3], '(' .. now, '+inf')) do
  reserved = reserved + tonumber(redis.call('HGET', KEYS[4], id) or '0')
end
local cooldown = math.max(tonumber(redis.call('GET', KEYS[5]) or '0') - now, 0)
return {redis.call('ZCOUNT', KEYS[2], '(' .. (now - 60000), '+inf'), used, reserved, cooldown}
`
