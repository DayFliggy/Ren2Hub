package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const GatewayReservationTTL = 120 * time.Second
const GatewayReservationRenewInterval = 30 * time.Second

var ErrGatewayQueueFull = errors.New("gateway admission queue is full")
var ErrGatewayTotalQueueFull = fmt.Errorf("gateway process queue is full: %w", ErrGatewayQueueFull)
var ErrGatewayWaitExpired = errors.New("gateway admission wait budget is exhausted")

var admissionRevision atomic.Uint64

type GatewayAdmissionPolicy struct {
	ProviderRates    map[int]model.GatewayProviderRateLimit
	Revision         uint64
	ConfigVersion    int64
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

// GatewayRoutingPolicy refreshes mutable routing limits without resetting the
// logical request's original user policy or accumulated wait budget.
func GatewayRoutingPolicy(ctx context.Context) (GatewayAdmissionPolicy, bool, error) {
	policy, _, enabled := GatewayAdmissionFromContext(ctx)
	if !enabled {
		return policy, false, nil
	}
	revision := admissionRevision.Load()
	if policy.Revision == revision {
		return policy, true, nil
	}
	config, err := model.GetGatewayConfigContext(ctx)
	if err != nil {
		return policy, true, err
	}
	if revision != admissionRevision.Load() {
		return policy, true, ErrAdmissionRuntime
	}
	policy.ProviderRates, policy.ChannelQueueSize = config.ChannelRates, config.ChannelQueueSize
	policy.Revision, policy.ConfigVersion = revision, config.Version
	applyGatewayProcessConfig(config)
	return policy, true, nil
}

func ResolveGatewayAdmissionPolicy(config model.GatewayConfig, user model.User) (GatewayAdmissionPolicy, error) {
	if err := config.Validate(); err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	policy := GatewayAdmissionPolicy{ProviderRates: config.ChannelRates, Revision: admissionRevision.Load(), ConfigVersion: config.Version,
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
	revision := admissionRevision.Load()
	if model.DB == nil || userID <= 0 {
		return GatewayAdmissionPolicy{}, ErrAdmissionRuntime
	}
	config, err := model.GetGatewayConfigContext(ctx)
	if err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	policy, err := loadGatewayUserPolicy(ctx, config, userID)
	if err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	if revision != admissionRevision.Load() {
		return GatewayAdmissionPolicy{}, ErrAdmissionRuntime
	}
	applyGatewayProcessConfig(config)
	policy.Revision = revision
	return policy, nil
}

func loadGatewayUserPolicy(ctx context.Context, config model.GatewayConfig, userID int) (GatewayAdmissionPolicy, error) {
	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "status", "group", "gateway_concurrency", "gateway_wait_ms", "gateway_queue_size").First(&user, userID).Error; err != nil {
		return GatewayAdmissionPolicy{}, err
	}
	if user.Status != common.UserStatusEnabled {
		return GatewayAdmissionPolicy{}, ErrAdmissionRuntime
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

func AcquireGatewayUserPermit(ctx context.Context, requestID string, userID int, policy GatewayAdmissionPolicy, budget *GatewayWaitBudget) (*Permit, error) {
	if userID <= 0 || requestID == "" {
		return nil, ErrAdmissionRuntime
	}
	if policy.Revision != admissionRevision.Load() {
		return nil, ErrAdmissionRuntime
	}
	return acquireGatewayPermit(ctx, UserAdmissionKey(userID), AdmissionPolicy{Capacity: policy.UserConcurrency, QueueSize: policy.UserQueueSize, Revision: policy.Revision, ConfigVersion: policy.ConfigVersion}, budget)
}

// allowQueue only selects immediate admission versus waiting. The current
// policy owns the queue limit and is loaded within the remaining wait budget.
func AcquireGatewayChannelPermit(ctx context.Context, requestID string, channelID int, allowQueue bool, budget *GatewayWaitBudget) (result *Permit, admitted model.Channel, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = fmt.Errorf("channel admission: %w", resultErr)
		}
	}()
	if requestID == "" {
		return nil, model.Channel{}, ErrAdmissionRuntime
	}
	if allowQueue && budget.Remaining() > 0 {
		ownerBudget, parent := budget, ctx
		started := time.Now()
		waitCtx, cancel := context.WithTimeout(ctx, budget.Remaining())
		defer cancel()
		defer func() {
			ownerBudget.consume(time.Since(started))
			if errors.Is(resultErr, context.DeadlineExceeded) && parent.Err() == nil {
				resultErr = ErrGatewayWaitExpired
			}
		}()
		// The child deadline governs policy reads and the queue only. The
		// caller retains its logical request context for upstream execution.
		ctx = waitCtx
		budget = NewGatewayWaitBudget(budget.Remaining())
	}
	revision := admissionRevision.Load()
	queueLimit, configVersion := 0, int64(0)
	if policy, _, ok := GatewayAdmissionFromContext(ctx); ok && policy.Revision == revision {
		queueLimit, configVersion = policy.ChannelQueueSize, policy.ConfigVersion
	} else {
		config, err := model.GetGatewayConfigContext(ctx)
		if err != nil {
			return nil, model.Channel{}, err
		}
		queueLimit, configVersion = config.ChannelQueueSize, config.Version
	}
	channel, err := loadRouteChannel(ctx, channelID)
	if err != nil {
		return nil, channel, err
	}
	if revision != admissionRevision.Load() {
		return nil, channel, ErrAdmissionRuntime
	}
	if !allowQueue {
		budget = NewGatewayWaitBudget(0)
	}
	permit, err := acquireGatewayPermit(ctx, ChannelAdmissionKey(channelID), AdmissionPolicy{Capacity: channel.CapacityTotal, QueueSize: queueLimit, Revision: revision, ConfigVersion: configVersion}, budget)
	if permit != nil {
		permit.ChannelID = channelID
	}
	return permit, channel, err
}

func acquireGatewayPermit(ctx context.Context, key string, policy AdmissionPolicy, budget *GatewayWaitBudget) (*Permit, error) {
	permit, err := gatewayAdmission.TryAcquire(ctx, key, policy)
	if !errors.Is(err, ErrAdmissionCapacity) || budget.Remaining() <= 0 || policy.QueueSize == 0 {
		return permit, err
	}
	started := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, budget.Remaining())
	defer cancel()
	defer func() { budget.consume(time.Since(started)) }()
	permit, err = gatewayAdmission.Acquire(waitCtx, key, policy, true)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil, ErrGatewayWaitExpired
	}
	return permit, err
}

func UserAdmissionKey(id int) string    { return fmt.Sprintf("user:%d", id) }
func ChannelAdmissionKey(id int) string { return fmt.Sprintf("channel:%d", id) }
func GatewayAdmissionHTTPStatus(err error) int {
	if errors.Is(err, ErrAdmissionCapacity) || errors.Is(err, ErrGatewayQueueFull) || errors.Is(err, ErrGatewayWaitExpired) || errors.Is(err, ErrGatewayProviderRateLimit) {
		return http.StatusTooManyRequests
	}
	return http.StatusServiceUnavailable
}
func GatewayAdmissionErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrGatewayDraining):
		return "gateway_draining"
	case errors.Is(err, ErrGatewayTotalQueueFull):
		return "gateway_total_queue_full"
	case errors.Is(err, ErrGatewayQueueFull):
		return "gateway_queue_full"
	case errors.Is(err, ErrGatewayWaitExpired):
		return "gateway_wait_timeout"
	case errors.Is(err, ErrAdmissionCapacity):
		return "gateway_capacity_exhausted"
	default:
		return "gateway_admission_unavailable"
	}
}

func GatewayLogicalRateKey(userID int) string {
	return fmt.Sprintf("gateway:rate:v1:user:%d", userID)
}
