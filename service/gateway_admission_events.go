package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var admissionRefresh = struct {
	sync.Mutex
	pending    map[string]uint64
	refreshing bool
}{pending: make(map[string]uint64)}

func init() { model.SetGatewayAdmissionChangeHook(invalidateGatewayAdmission) }

func InitGatewayAdmission(ctx context.Context) error {
	config, err := model.GetGatewayConfigContext(ctx)
	if err != nil {
		return err
	}
	applyGatewayProcessConfig(config)
	return nil
}

func invalidateGatewayAdmission(kind string, ids ...int) {
	keys := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		keys[kind+":"+strconv.Itoa(id)] = struct{}{}
	}
	admissionRefresh.Lock()
	gatewayAdmission.mu.Lock()
	revision := admissionRevision.Add(1)
	for key, pool := range gatewayAdmission.pools {
		if _, affected := keys[key]; kind != "config" && !affected {
			continue
		}
		pool.invalid, pool.policy.Revision = true, revision
		admissionRefresh.pending[key] = revision
	}
	gatewayAdmission.mu.Unlock()
	if kind == "config" {
		admissionRefresh.pending["config"] = revision
	}
	if admissionRefresh.refreshing {
		admissionRefresh.Unlock()
		return
	}
	admissionRefresh.refreshing = true
	for {
		pending := admissionRefresh.pending
		admissionRefresh.pending = make(map[string]uint64)
		if len(pending) == 0 {
			admissionRefresh.refreshing = false
			admissionRefresh.Unlock()
			return
		}
		admissionRefresh.Unlock()
		refreshAdmissionPolicies(pending)
		admissionRefresh.Lock()
	}
}

func refreshAdmissionPolicies(pending map[string]uint64) {
	// One immutable configuration snapshot per committed notification. Resource
	// reads stay outside the manager lock, and revision fences reject late results.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config, configErr := model.GetGatewayConfigContext(ctx)
	if configErr == nil {
		applyGatewayProcessConfig(config)
	}
	for key, revision := range pending {
		policy := AdmissionPolicy{Revision: revision, ConfigVersion: config.Version}
		err := configErr
		if err == nil && key != "config" {
			parts := strings.SplitN(key, ":", 2)
			id, parseErr := strconv.Atoi(parts[1])
			err = parseErr
			if err == nil && parts[0] == "user" {
				var userPolicy GatewayAdmissionPolicy
				userPolicy, err = loadGatewayUserPolicy(ctx, config, id)
				policy.Capacity, policy.QueueSize = userPolicy.UserConcurrency, userPolicy.UserQueueSize
			} else if err == nil {
				var channel model.Channel
				channel, err = loadRouteChannel(ctx, id)
				policy.Capacity, policy.QueueSize = channel.CapacityTotal, config.ChannelQueueSize
			}
		}
		if err != nil {
			common.SysError("gateway admission policy refresh failed: " + err.Error())
			gatewayAdmission.RejectRevision(key, revision, ErrAdmissionRuntime)
		} else {
			gatewayAdmission.ApplyPolicy(key, policy)
		}
	}
}

var gatewayProcessConfigMu sync.Mutex
var gatewayProcessConfigVersion int64

func applyGatewayProcessConfig(config model.GatewayConfig) {
	gatewayProcessConfigMu.Lock()
	defer gatewayProcessConfigMu.Unlock()
	if config.Version < gatewayProcessConfigVersion {
		return
	}
	gatewayProcessConfigVersion = config.Version
	gatewayAdmission.SetTotalLimit(config.TotalQueueSize)
	gatewayRetries.Configure(config)
}
