package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
)

const GatewayLeaseTTL = 120 * time.Second
const GatewayLeaseRenewInterval = 30 * time.Second

var ErrGatewayQueueFull = errors.New("gateway admission queue is full")
var ErrGatewayWaitExpired = errors.New("gateway admission wait budget is exhausted")

type GatewayAdmissionPolicy struct {
	UserConcurrency  int
	Wait             time.Duration
	UserQueueSize    int
	ChannelQueueSize int
	Sources          GatewayAdmissionSources
}

type GatewayAdmissionSources struct {
	Concurrency      string `json:"concurrency"`
	WaitMS           string `json:"wait_ms"`
	QueueSize        string `json:"queue_size"`
	ChannelQueueSize string `json:"channel_queue_size"`
}

type gatewayAdmissionContextKey struct{}

type gatewayAdmissionContext struct {
	policy GatewayAdmissionPolicy
	budget *GatewayWaitBudget
}

func WithGatewayAdmissionContext(ctx context.Context, policy GatewayAdmissionPolicy, budget *GatewayWaitBudget) context.Context {
	return context.WithValue(ctx, gatewayAdmissionContextKey{}, gatewayAdmissionContext{policy: policy, budget: budget})
}

func GatewayAdmissionFromContext(ctx context.Context) (GatewayAdmissionPolicy, *GatewayWaitBudget, bool) {
	value, ok := ctx.Value(gatewayAdmissionContextKey{}).(gatewayAdmissionContext)
	return value.policy, value.budget, ok
}

func ResolveGatewayAdmissionPolicy(config model.GatewayConfig, user model.User) (GatewayAdmissionPolicy, error) {
	if err := config.Validate(); err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	policy := GatewayAdmissionPolicy{
		UserConcurrency: config.UserConcurrency, Wait: time.Duration(config.WaitMS) * time.Millisecond,
		UserQueueSize: config.UserQueueSize, ChannelQueueSize: config.ChannelQueueSize,
		Sources: GatewayAdmissionSources{Concurrency: "system", WaitMS: "system", QueueSize: "system", ChannelQueueSize: "system"},
	}
	for _, entry := range []struct {
		override model.GatewayAdmissionOverride
		source   string
	}{
		{config.Groups[user.Group], "group"},
		{model.GatewayAdmissionOverride{Concurrency: user.GatewayConcurrency, WaitMS: user.GatewayWaitMS, QueueSize: user.GatewayQueueSize}, "user"},
	} {
		override := entry.override
		if err := override.Validate(); err != nil {
			return GatewayAdmissionPolicy{}, err
		}
		if override.Concurrency != nil {
			policy.UserConcurrency = *override.Concurrency
			policy.Sources.Concurrency = entry.source
		}
		if override.WaitMS != nil {
			policy.Wait = time.Duration(*override.WaitMS) * time.Millisecond
			policy.Sources.WaitMS = entry.source
		}
		if override.QueueSize != nil {
			policy.UserQueueSize = *override.QueueSize
			policy.Sources.QueueSize = entry.source
		}
	}
	return policy, nil
}

func LoadGatewayAdmissionPolicy(ctx context.Context, userID int) (GatewayAdmissionPolicy, error) {
	if model.DB == nil || userID <= 0 {
		return GatewayAdmissionPolicy{}, ErrRouteLeaseRuntime
	}
	config, err := model.GetGatewayConfig()
	if err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "status", "group", "gateway_concurrency", "gateway_wait_ms", "gateway_queue_size").First(&user, userID).Error; err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	if user.Status != common.UserStatusEnabled {
		return GatewayAdmissionPolicy{}, ErrRouteLeaseRuntime
	}
	return ResolveGatewayAdmissionPolicy(config, user)
}

// GatewayWaitBudget is shared by user admission and every channel attempt.
// Only time actually spent queued consumes it; upstream execution does not.
type GatewayWaitBudget struct {
	mu        sync.Mutex
	remaining time.Duration
}

func NewGatewayWaitBudget(wait time.Duration) *GatewayWaitBudget {
	return &GatewayWaitBudget{remaining: min(max(wait, 0), time.Duration(model.GatewayMaxWaitMS)*time.Millisecond)}
}

func (budget *GatewayWaitBudget) Remaining() time.Duration {
	if budget == nil {
		return 0
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.remaining
}

func (budget *GatewayWaitBudget) consume(waited time.Duration) {
	if budget == nil {
		return
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.remaining = max(budget.remaining-waited, 0)
}

func gatewayRedisAvailable() bool {
	return common.RedisEnabled && common.RDB != nil
}

func AcquireGatewayUserLease(ctx context.Context, requestID string, userID int, policy GatewayAdmissionPolicy, budget *GatewayWaitBudget) (RouteLease, error) {
	if !gatewayRedisAvailable() {
		return RouteLease{}, ErrRouteLeaseUnavailable
	}
	if userID <= 0 || requestID == "" || policy.UserConcurrency < 0 || policy.UserQueueSize < 0 {
		return RouteLease{}, ErrRouteLeaseRuntime
	}
	// Unlimited admission still records occupancy so reenabling a cap cannot
	// forget requests that began while the limit was disabled.
	return AcquireQueuedRouteLease(ctx, common.RDB, "user:"+requestID, uuid.NewString(), GatewayLeaseTTL,
		RouteLeaseResource{Key: UserRouteLeaseKey(userID), Capacity: policy.UserConcurrency}, policy.UserQueueSize, budget, func(ctx context.Context) (int, error) {
			current, err := LoadGatewayAdmissionPolicy(ctx, userID)
			return current.UserConcurrency, err
		})
}

func AcquireGatewayChannelLease(ctx context.Context, requestID string, channelID int, queueSize int, budget *GatewayWaitBudget) (RouteLease, model.Channel, error) {
	if !gatewayRedisAvailable() {
		return RouteLease{}, model.Channel{}, ErrRouteLeaseUnavailable
	}
	channel, err := loadRouteChannel(ctx, channelID)
	if err != nil {
		return RouteLease{}, channel, err
	}
	lease, err := AcquireQueuedRouteLease(ctx, common.RDB, "channel:"+requestID, uuid.NewString(), GatewayLeaseTTL,
		RouteLeaseResource{Key: ChannelRouteLeaseKey(channelID), Capacity: channel.CapacityTotal}, queueSize, budget, func(ctx context.Context) (int, error) {
			current, err := loadRouteChannel(ctx, channelID)
			return current.CapacityTotal, err
		})
	lease.ChannelID = channelID
	return lease, channel, err
}

func GatewayAdmissionErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrGatewayQueueFull):
		return "gateway_queue_full"
	case errors.Is(err, ErrGatewayWaitExpired):
		return "gateway_wait_timeout"
	case errors.Is(err, ErrRouteLeaseCapacity):
		return "gateway_capacity_exhausted"
	default:
		return "gateway_admission_unavailable"
	}
}

func GatewayLogicalRateKey(userID int) string {
	return fmt.Sprintf("gateway:rate:v1:user:%d", userID)
}
