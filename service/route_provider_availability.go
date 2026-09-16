package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// Availability is advisory. Unknown observations stay eligible; the atomic
// reservation before sending remains authoritative, including credential limits.
func filterKnownProviderUnavailable(ctx context.Context, candidates []RouteSelectionCandidate) {
	policy, enabled, policyErr := GatewayRoutingPolicy(ctx)
	if policyErr != nil {
		common.SysError("gateway provider policy unavailable: " + policyErr.Error())
		return
	}
	if !enabled || !common.RedisEnabled || common.RDB == nil {
		return
	}
	pipe := common.RDB.Pipeline()
	defer pipe.Close()
	commands := make(map[int]*redis.Cmd)
	for index, candidate := range candidates {
		if candidate.FilterReason != "" {
			continue
		}
		if len(commands) == MaxRouteCandidateChannels {
			break
		}
		keys := gatewayProviderRateKeys(GatewayProviderRateReservation{Scopes: []GatewayProviderRateScope{{ID: GatewayChannelRateScopeID(candidate.ChannelID)}}})
		commands[index] = pipe.Eval(ctx, gatewayProviderRuntimeLua, keys[:5])
	}
	if len(commands) == 0 {
		return
	}
	if _, err := pipe.Exec(ctx); err != nil {
		common.SysError("gateway provider availability unavailable: " + err.Error())
		return
	}
	for index, command := range commands {
		values, err := command.Int64Slice()
		if err != nil || len(values) != 4 {
			continue
		}
		candidate := &candidates[index]
		limit := policy.ProviderRates[candidate.ChannelID]
		if values[3] > 0 || (limit.RPM > 0 && values[0] >= limit.RPM) || (limit.TPM > 0 && values[1]+values[2] >= limit.TPM) {
			candidate.FilterReason = "provider_rate_limited"
		}
	}
}
