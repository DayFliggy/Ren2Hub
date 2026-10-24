package service

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func observationDB(db *gorm.DB) (*sql.DB, error) {
	if db == nil {
		return nil, errors.New("database handle unavailable")
	}
	return db.DB()
}

func (s *observationSampler) probeStorage(ctx context.Context) ObservationStorage {
	mainDB, mainErr := observationDB(model.DB)
	logDB, logErr := observationDB(model.LOG_DB)
	result := ObservationStorage{Database: s.probeDatabase(ctx, "database", mainDB, mainErr)}
	// Distinct GORM sessions can still refer to the same underlying pool.
	result.LogShared = mainDB != nil && logDB == mainDB
	if !result.LogShared {
		probe := s.probeDatabase(ctx, "log_database", logDB, logErr)
		result.LogDatabase = &probe
	}
	if !common.RedisEnabled {
		result.Redis = ObservationProbe{Status: "disabled", Reason: "disabled"}
	} else {
		result.Redis = s.ping(ctx, "redis", func(ctx context.Context) error {
			if common.RDB == nil {
				return errors.New("redis client unavailable")
			}
			return common.RDB.Ping(ctx).Err()
		})
	}
	return result
}

func (s *observationSampler) probeDatabase(ctx context.Context, name string, db *sql.DB, err error) ObservationProbe {
	return s.ping(ctx, name, func(ctx context.Context) error {
		if err != nil {
			return err
		}
		return db.PingContext(ctx)
	})
}

func (s *observationSampler) ping(parent context.Context, name string, check func(context.Context) error) ObservationProbe {
	ctx, cancel := context.WithTimeout(parent, observationProbeTimeout)
	defer cancel()
	started := time.Now()
	err := check(ctx)
	if err == nil {
		if s.failing[name] {
			common.SysLog("performance observation: " + name + " probe recovered")
		}
		delete(s.failing, name)
		return ObservationProbe{Status: "ok", LatencyMs: observationPtr(float64(time.Since(started).Microseconds()) / 1000)}
	}
	reason := "unavailable"
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		reason = "timeout"
	}
	if errors.Is(err, context.Canceled) {
		reason = "canceled"
	}
	if !s.failing[name] && reason != "canceled" {
		common.SysError("performance observation: " + name + " probe failed (" + reason + ")")
	}
	s.failing[name] = true
	return ObservationProbe{Status: "critical", Reason: reason}
}
