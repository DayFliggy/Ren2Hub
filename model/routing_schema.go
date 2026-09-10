package model

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
)

const RoutingSchemaVersion = 1

// RoutingSchemaState is written by the offline migration only after data
// verification and physical retirement have completed for this database.
type RoutingSchemaState struct {
	Role               string `gorm:"primaryKey;size:16" json:"role"`
	Version            int    `json:"version"`
	Stage              string `gorm:"size:64" json:"stage"`
	SourceFingerprint  string `gorm:"size:64" json:"source_fingerprint"`
	Cursor             int64  `json:"cursor"`
	UpdatedAt          int64  `json:"updated_at"`
	ToolVersion        string `gorm:"size:32" json:"tool_version"`
	DatabaseIdentity   string `gorm:"size:64" json:"database_identity"`
	MigrationID        string `gorm:"size:32" json:"migration_id"`
	BaselineAccounting string `gorm:"type:text" json:"-"`
	CandidateDigest    string `gorm:"size:64" json:"candidate_digest"`
	CandidateCount     int64  `json:"candidate_count"`
	OrphanAbilities    int64  `json:"orphan_abilities"`
	AbilityStep        string `gorm:"size:32" json:"ability_step"`
	RetiredIndexes     string `gorm:"type:text" json:"-"`
	ClickHouseManifest string `gorm:"type:text" json:"-"`
	TargetFingerprint  string `gorm:"size:64" json:"target_fingerprint"`
}

func requireRoutingSchema(db *gorm.DB, role string) error {
	coreTable := "users"
	if role == "log" {
		coreTable = "logs"
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return fmt.Errorf("cannot inspect %s database schema: %w", role, err)
	}
	coreExists, stateExists := false, false
	for _, table := range tables {
		coreExists = coreExists || table == coreTable
		stateExists = stateExists || table == "routing_schema_states"
	}
	if !coreExists && !stateExists {
		return nil
	}
	if !stateExists {
		return fmt.Errorf("%s database requires offline routing migration; run ren2hub-migrate inspect", role)
	}
	var state RoutingSchemaState
	if err := db.Where("role = ?", role).Order("updated_at DESC").First(&state).Error; err != nil {
		return fmt.Errorf("%s routing schema state unavailable: %w", role, err)
	}
	if state.Version != RoutingSchemaVersion || state.Stage != "complete" {
		return fmt.Errorf("%s routing schema is not ready (version=%d stage=%s)", role, state.Version, state.Stage)
	}
	return nil
}

func preflightRoutingDatabases(db *gorm.DB) error {
	if err := requireRoutingSchema(db, "main"); err != nil {
		return err
	}
	if os.Getenv("LOG_SQL_DSN") == "" {
		return nil
	}
	logDB, _, err := chooseDB("LOG_SQL_DSN", true)
	if err != nil {
		return errors.New("cannot open log database for routing schema preflight")
	}
	sqlDB, err := logDB.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return requireRoutingSchema(logDB, "log")
}

func completeFreshRoutingSchema(db *gorm.DB, role string) error {
	state := RoutingSchemaState{Role: role, Version: RoutingSchemaVersion, Stage: "complete", UpdatedAt: time.Now().UnixNano()}
	if db.Dialector.Name() == "clickhouse" {
		if err := createClickHouseRoutingJournal(db); err != nil {
			return err
		}
		return db.Create(&state).Error
	}
	if err := db.AutoMigrate(&RoutingSchemaState{}); err != nil {
		return err
	}
	previous, err := loadRoutingMigrationState(db, role)
	if err != nil {
		return err
	}
	if previous != nil {
		if previous.Version != RoutingSchemaVersion || previous.Stage != "complete" {
			return errors.New("cannot replace unfinished routing migration evidence")
		}
		return nil
	}
	return db.Save(&state).Error
}
