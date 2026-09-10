package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"gorm.io/gorm"
)

// The legacy projection is derived from reachable ability rows, never from the
// channel's model list. The complete projection is retained as migration evidence.
func projectedRoutingAbilities(ctx context.Context, db *gorm.DB, table string) ([]Ability, int64, error) {
	if !db.Migrator().HasTable(table) {
		return []Ability{}, 0, nil
	}
	if !db.Migrator().HasColumn(table, "group") {
		rows := make([]Ability, 0)
		err := db.WithContext(ctx).Table(table).Order("channel_id ASC, model ASC").Find(&rows).Error
		sortRoutingAbilities(rows)
		return rows, 0, err
	}
	var old []struct {
		Group     string
		Model     string
		ChannelID int
		Enabled   bool
	}
	if err := db.WithContext(ctx).Table(table).Find(&old).Error; err != nil {
		return nil, 0, err
	}
	var channels []Channel
	if err := db.WithContext(ctx).Select("id", "priority", "weight", "tag").Find(&channels).Error; err != nil {
		return nil, 0, err
	}
	byID := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		byID[channel.Id] = channel
	}
	merged := map[string]Ability{}
	var orphans int64
	for _, row := range old {
		channel, exists := byID[row.ChannelID]
		canonical := modellab.NormalizeModel(row.Model)
		if !exists || canonical == "" {
			orphans++
			continue
		}
		key := fmt.Sprintf("%d\x00%s", row.ChannelID, canonical)
		ability := merged[key]
		ability.Model, ability.ChannelId = canonical, row.ChannelID
		ability.Enabled = ability.Enabled || (row.Enabled && strings.TrimSpace(row.Group) != "")
		ability.Priority, ability.Weight, ability.Tag = channel.Priority, uint(channel.GetWeight()), channel.Tag
		merged[key] = ability
	}
	rows := make([]Ability, 0, len(merged))
	for _, row := range merged {
		rows = append(rows, row)
	}
	sortRoutingAbilities(rows)
	return rows, orphans, nil
}

func sortRoutingAbilities(rows []Ability) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ChannelId != rows[j].ChannelId {
			return rows[i].ChannelId < rows[j].ChannelId
		}
		return rows[i].Model < rows[j].Model
	})
}

func routingCandidateSnapshot(ctx context.Context, db *gorm.DB, table string) (string, int64, int64, error) {
	rows, orphans, err := projectedRoutingAbilities(ctx, db, table)
	if err != nil {
		return "", 0, 0, err
	}
	encoded, err := common.Marshal(rows)
	if err != nil {
		return "", 0, 0, err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), int64(len(rows)), orphans, nil
}

func routingAbilityCopyTables(state *RoutingSchemaState) (string, string) {
	return "routing_migration_a_" + state.MigrationID, "routing_migration_o_" + state.MigrationID
}

func verifyAbilityCopy(ctx context.Context, db *gorm.DB, table string, state *RoutingSchemaState) error {
	if !db.Migrator().HasTable(table) {
		return fmt.Errorf("missing migration ability table %s", table)
	}
	digest, count, _, err := routingCandidateSnapshot(ctx, db, table)
	if err != nil {
		return err
	}
	if digest != state.CandidateDigest || count != state.CandidateCount {
		return fmt.Errorf("ability copy content mismatch in %s", table)
	}
	return nil
}

// MySQL DDL is not transactional. Each intent is persisted before DDL, and each
// resume validates which of the two names exists before advancing the journal.
func rebuildRetiredAbilities(ctx context.Context, db *gorm.DB, state *RoutingSchemaState) error {
	shadow, original := routingAbilityCopyTables(state)
	if state.AbilityStep == "" {
		if db.Migrator().HasTable(shadow) || db.Migrator().HasTable(original) {
			return errors.New("unowned ability copy table collides with this migration")
		}
		if !db.Migrator().HasTable("abilities") || !db.Migrator().HasColumn("abilities", "group") {
			return nil
		}
		state.AbilityStep = "copying"
		if err := checkpointRoutingMigration(db, state, state.Stage, state.Cursor); err != nil {
			return err
		}
	}
	if state.AbilityStep == "copying" {
		if db.Migrator().HasTable(original) || !db.Migrator().HasTable("abilities") || !db.Migrator().HasColumn("abilities", "group") {
			return errors.New("unexpected ability tables before copy")
		}
		rows, _, err := projectedRoutingAbilities(ctx, db, "abilities")
		if err != nil {
			return err
		}
		if err := verifyAbilityCopy(ctx, db, "abilities", state); err != nil {
			return err
		}
		if db.Migrator().HasTable(shadow) {
			if err := db.Migrator().DropTable(shadow); err != nil {
				return err
			}
		}
		if err := db.Table(shadow).Migrator().CreateTable(&Ability{}); err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := db.Table(shadow).CreateInBatches(rows, 200).Error; err != nil {
				return err
			}
		}
		if err := verifyAbilityCopy(ctx, db, shadow, state); err != nil {
			return err
		}
		state.AbilityStep = "copied"
		if err := checkpointRoutingMigration(db, state, state.Stage, state.Cursor); err != nil {
			return err
		}
	}
	if state.AbilityStep == "copied" {
		if err := verifyAbilityCopy(ctx, db, shadow, state); err != nil {
			return err
		}
		if !db.Migrator().HasTable(original) {
			if err := verifyAbilityCopy(ctx, db, "abilities", state); err != nil {
				return err
			}
			if err := db.Migrator().RenameTable("abilities", original); err != nil {
				return err
			}
		} else if db.Migrator().HasTable("abilities") {
			return errors.New("both original and active ability tables exist before activation")
		}
		state.AbilityStep = "source_renamed"
		if err := checkpointRoutingMigration(db, state, state.Stage, state.Cursor); err != nil {
			return err
		}
	}
	if state.AbilityStep == "source_renamed" {
		if err := verifyAbilityCopy(ctx, db, original, state); err != nil {
			return err
		}
		if !db.Migrator().HasTable("abilities") {
			if err := verifyAbilityCopy(ctx, db, shadow, state); err != nil {
				return err
			}
			if err := db.Migrator().RenameTable(shadow, "abilities"); err != nil {
				return err
			}
		} else if db.Migrator().HasTable(shadow) {
			return errors.New("both shadow and active ability tables exist after activation")
		}
		state.AbilityStep = "activated"
		if err := checkpointRoutingMigration(db, state, state.Stage, state.Cursor); err != nil {
			return err
		}
	}
	if state.AbilityStep == "activated" {
		if err := verifyAbilityCopy(ctx, db, "abilities", state); err != nil {
			return err
		}
		if db.Migrator().HasTable(shadow) || db.Migrator().HasColumn("abilities", "group") {
			return errors.New("unexpected ability schema after activation")
		}
		if db.Migrator().HasTable(original) {
			if err := verifyAbilityCopy(ctx, db, original, state); err != nil {
				return err
			}
			if err := db.Migrator().DropTable(original); err != nil {
				return err
			}
		}
		state.AbilityStep = "complete"
		return checkpointRoutingMigration(db, state, state.Stage, state.Cursor)
	}
	if state.AbilityStep != "complete" {
		return errors.New("unknown ability copy checkpoint")
	}
	return verifyAbilityCopy(ctx, db, "abilities", state)
}
