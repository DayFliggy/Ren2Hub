package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRouteHealthUnavailable = errors.New("route health probe is unavailable")
var ErrRouteHealthOwnership = errors.New("route health probe ownership changed")

type RouteHealthFence struct {
	KeyScope string
	Epoch    int64
	Probe    bool
}

// RouteAttemptHealth is captured only for an admitted attempt. Closed scopes
// also carry an epoch so a late success cannot erase a newer failure.
type RouteAttemptHealth struct {
	ChannelID int
	Model     string
	Owner     string
	Scopes    []RouteHealthFence
}

func AcquireRouteAttemptHealth(ctx context.Context, channelID int, requestModel, key, owner string, ttl time.Duration) (RouteAttemptHealth, error) {
	return acquireRouteAttemptHealthAt(ctx, channelID, requestModel, key, owner, ttl, time.Now())
}

func acquireRouteAttemptHealthAt(ctx context.Context, channelID int, requestModel, key, owner string, ttl time.Duration, now time.Time) (RouteAttemptHealth, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	attempt := RouteAttemptHealth{ChannelID: channelID, Model: modellab.NormalizeModel(requestModel), Owner: owner}
	if model.DB == nil || channelID <= 0 || attempt.Model == "" || strings.TrimSpace(owner) == "" || len(owner) > 128 || ttl < time.Millisecond {
		return attempt, ErrRouteHealthUnavailable
	}
	scopes := []string{""}
	if strings.TrimSpace(key) != "" {
		scopes = append(scopes, RouteKeyScope(key))
	}
	for _, scope := range scopes {
		fence, err := claimRouteHealthScope(ctx, channelID, attempt.Model, scope, owner, ttl, now)
		if err != nil {
			return RouteAttemptHealth{}, errors.Join(err, ReleaseRouteAttemptHealth(context.WithoutCancel(ctx), attempt))
		}
		attempt.Scopes = append(attempt.Scopes, fence)
	}
	return attempt, nil
}

func claimRouteHealthScope(ctx context.Context, channelID int, requestModel, scope, owner string, ttl time.Duration, now time.Time) (RouteHealthFence, error) {
	health, err := loadRouteHealthScope(ctx, channelID, requestModel, scope)
	fence := RouteHealthFence{KeyScope: scope, Epoch: health.HealthEpoch}
	if err != nil {
		return fence, err
	}
	if !RouteHealthReadOnlyUsable(health, now) {
		return fence, ErrRouteHealthUnavailable
	}
	if health.State == "" || health.State == model.RouteHealthStateClosed {
		return fence, nil
	}
	// Both a recovered open scope and an expired probe can be claimed. The
	// version predicate prevents competing requests from owning the same probe.
	result := model.DB.WithContext(ctx).Model(&model.ChannelHealth{}).
		Where("id = ? AND health_epoch = ? AND state = ?", health.ID, health.HealthEpoch, health.State).
		Where("COALESCE(probe_expires_at, 0) = ?", health.ProbeExpiresAt).
		Updates(map[string]any{
			"state": model.RouteHealthStateHalfOpen, "probe_owner": owner,
			"probe_expires_at": now.Add(ttl).UnixMilli(),
			"health_epoch":     health.HealthEpoch + 1, "updated_at": now.Unix(),
		})
	if result.Error != nil {
		return fence, result.Error
	}
	if result.RowsAffected != 1 {
		return fence, ErrRouteHealthUnavailable
	}
	fence.Probe, fence.Epoch = true, health.HealthEpoch+1
	return fence, nil
}

func RenewRouteAttemptHealth(ctx context.Context, attempt RouteAttemptHealth, ttl time.Duration) error {
	return renewRouteAttemptHealthAt(ctx, attempt, ttl, time.Now())
}

func renewRouteAttemptHealthAt(ctx context.Context, attempt RouteAttemptHealth, ttl time.Duration, now time.Time) error {
	if ttl < time.Millisecond {
		return ErrRouteHealthOwnership
	}
	for _, scope := range attempt.Scopes {
		if !scope.Probe {
			continue
		}
		result := routeHealthOwnedProbe(ctx, attempt, scope).
			Where("probe_expires_at > ?", now.UnixMilli()).
			Update("probe_expires_at", gorm.Expr("CASE WHEN probe_expires_at > ? THEN probe_expires_at ELSE ? END", now.Add(ttl).UnixMilli(), now.Add(ttl).UnixMilli()))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			// MySQL may report zero changed rows when an existing renewal is
			// already later than this request's target expiry.
			var owned int64
			err := routeHealthOwnedProbe(ctx, attempt, scope).
				Where("probe_expires_at > ? AND probe_expires_at >= ?", now.UnixMilli(), now.Add(ttl).UnixMilli()).Count(&owned).Error
			if err != nil {
				return err
			}
			if owned != 1 {
				return ErrRouteHealthOwnership
			}
		}
	}
	return nil
}

// Release is idempotent and cannot release another attempt's recovered probe.
// It retains the provider cooldown and makes an abandoned probe claimable again.
func ReleaseRouteAttemptHealth(ctx context.Context, attempt RouteAttemptHealth) error {
	var releaseErr error
	for _, scope := range attempt.Scopes {
		if !scope.Probe {
			continue
		}
		result := routeHealthOwnedProbe(ctx, attempt, scope).Updates(map[string]any{
			"state": model.RouteHealthStateOpen, "probe_owner": "", "probe_expires_at": 0,
			"health_epoch": gorm.Expr("health_epoch + 1"),
		})
		releaseErr = errors.Join(releaseErr, result.Error)
	}
	return releaseErr
}

func routeHealthOwnedProbe(ctx context.Context, attempt RouteAttemptHealth, scope RouteHealthFence) *gorm.DB {
	return model.DB.WithContext(ctx).Model(&model.ChannelHealth{}).
		Where("channel_id = ? AND model = ? AND key_scope = ? AND state = ? AND health_epoch = ? AND probe_owner = ?",
			attempt.ChannelID, attempt.Model, scope.KeyScope, model.RouteHealthStateHalfOpen, scope.Epoch, attempt.Owner)
}

func ObserveRouteAttemptOutcome(ctx context.Context, attempt RouteAttemptHealth, outcome AttemptOutcome) error {
	return observeRouteAttemptOutcomeAt(ctx, attempt, outcome, time.Now())
}

func observeRouteAttemptOutcomeAt(ctx context.Context, attempt RouteAttemptHealth, outcome AttemptOutcome, now time.Time) error {
	if model.DB == nil || attempt.ChannelID <= 0 || attempt.Model == "" || len(attempt.Scopes) == 0 {
		return ErrRouteHealthUnavailable
	}
	classification := ClassifyAttemptOutcome(outcome)
	var observationErr error
	for _, scope := range attempt.Scopes {
		failed := classification.MarkKey && scope.KeyScope != "" ||
			(classification.MarkChannelModel || classification.MarkCapability) && scope.KeyScope == ""
		if !outcome.Success && !failed {
			continue
		}
		policy := DefaultRouteHealthPolicy()
		if scope.KeyScope != "" {
			policy = routeKeyHealthPolicy()
		} else if classification.MarkCapability {
			policy = routeCapabilityHealthPolicy()
		}
		err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			initial := model.ChannelHealth{ChannelID: attempt.ChannelID, Model: attempt.Model, KeyScope: scope.KeyScope}
			initial.Normalize(now)
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
				return err
			}
			var current model.ChannelHealth
			if err := model.LockForUpdate(tx).Where("channel_id = ? AND model = ? AND key_scope = ?", attempt.ChannelID, attempt.Model, scope.KeyScope).First(&current).Error; err != nil {
				return err
			}
			if scope.Probe && (current.ProbeOwner != attempt.Owner || current.HealthEpoch != scope.Epoch || current.ProbeExpiresAt <= now.UnixMilli()) {
				return nil
			}
			if outcome.Success {
				if current.HealthEpoch != scope.Epoch {
					return nil
				}
				current = ObserveRouteHealthSuccess(current, now)
				if scope.KeyScope == "" {
					current.LastLatencyMS = max(outcome.LatencyMS, 0)
					current.FirstTokenLatencyMS = max(outcome.TTFTMS, 0)
				}
			} else {
				current = ObserveRouteHealthFailureWithRetryAfter(current, policy, now, outcome.RetryAfter)
			}
			return tx.Save(&current).Error
		})
		observationErr = errors.Join(observationErr, err)
	}
	return errors.Join(observationErr, ReleaseRouteAttemptHealth(ctx, attempt))
}
