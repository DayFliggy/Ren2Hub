package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueChecksBackupPaymentLinks(t *testing.T) {
	key := operation_setting.BackupPaymentLinksOptionKey
	require.Error(t, validateOptionValue(key, `[{"name":"Shop","url":"javascript:alert(1)"}]`))
	require.NoError(t, validateOptionValue(key, `[{"name":"Shop","url":"https://shop.example.com"}]`))
}
