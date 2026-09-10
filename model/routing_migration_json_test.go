package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskSnapshotMigrationPreservesZeroAndUnknownExtensions(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`{"vendor":{"opaque":123},"billing_context":{"group_ratio":0,"model_ratio":2,"vendor":"kept"}}`, `{"vendor":{"opaque":123},"billing_context":{"billing_multiplier":0,"multiplier_source":"historical_snapshot","model_ratio":2,"vendor":"kept"}}`},
		{`{"billing_context":{"model_ratio":2}}`, `{"billing_context":{"billing_multiplier":0,"multiplier_source":"historical_snapshot","model_ratio":2}}`},
		{`{"billing_context":{"group_ratio":1.25,"billing_multiplier":1.25,"multiplier_source":"channel"}}`, `{"billing_context":{"billing_multiplier":1.25,"multiplier_source":"channel"}}`},
	} {
		migrated, err := migrateTaskBillingJSON([]byte(test.input), true)
		require.NoError(t, err)
		assert.JSONEq(t, test.want, string(migrated))
		repeated, err := migrateTaskBillingJSON(migrated, true)
		require.NoError(t, err)
		assert.JSONEq(t, string(migrated), string(repeated))
	}
}

func TestTaskSnapshotMigrationBlocksAmbiguousOrMissingPrices(t *testing.T) {
	for _, raw := range []string{`{}`, `{"billing_context":null}`, `invalid`, `{"billing_context":{"group_ratio":1,"billing_multiplier":2}}`, `{"billing_context":{"group_ratio":null}}`, `{"billing_context":{"group_ratio":-1}}`} {
		_, err := migrateTaskBillingJSON([]byte(raw), true)
		assert.Error(t, err, raw)
	}
	_, err := migrateTaskBillingJSON([]byte(`{}`), false)
	require.NoError(t, err)
}

func TestLogSnapshotMigrationDoesNotMultiplyUserOverrideAgain(t *testing.T) {
	converted, err := migrateLogBillingJSON([]byte(`{"group_ratio":0.5,"user_group_ratio":0.5,"model_ratio":3}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"billing_multiplier":0.5,"historical_user_multiplier":0.5,"multiplier_source":"historical_snapshot","model_ratio":3}`, string(converted))
}

func TestSnapshotMigrationPreservesExactNumericEvidence(t *testing.T) {
	converted, err := migrateTaskBillingJSON([]byte(`{"billing_context":{"group_ratio":9007199254740993}}`), true)
	require.NoError(t, err)
	assert.Contains(t, string(converted), `"billing_multiplier":9007199254740993`)
	_, err = migrateTaskBillingJSON([]byte(`{"billing_context":{"group_ratio":9007199254740993,"billing_multiplier":9007199254740992}}`), true)
	require.ErrorContains(t, err, "conflicting")
	converted, err = migrateTaskBillingJSON([]byte(`{"billing_context":{"group_ratio":1e2,"billing_multiplier":100.0}}`), true)
	require.NoError(t, err)
	assert.NotContains(t, string(converted), "group_ratio")
}

func TestLogSnapshotMigrationValidatesAlreadyNamedMultipliers(t *testing.T) {
	for _, raw := range []string{`{"billing_multiplier":null}`, `{"billing_multiplier":-1}`, `{"historical_user_multiplier":"invalid"}`, `{"billing_multiplier":1,"multiplier_source":17}`} {
		_, err := migrateLogBillingJSON([]byte(raw))
		assert.Error(t, err, raw)
	}
}
