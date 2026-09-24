package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomPaymentCallbackRejectsInvalidOriginAtomically(t *testing.T) {
	db := useWalletIngressDB(t)
	require.NoError(t, db.Create(&Option{Key: "CustomCallbackAddress", Value: "https://callback.example.test"}).Error)
	require.NoError(t, db.Create(&Option{Key: "MinTopUp", Value: "1"}).Error)
	for _, value := range []string{
		"callback.example.test", "https://callback.example.test/path", "https://callback.example.test?x=1",
		"https://callback.example.test?", "https://callback.example.test#fragment", "https://callback.example.test#",
		"https://user:private-secret@callback.example.test", "http://37.114.34.132:65536",
	} {
		t.Run(value, func(t *testing.T) {
			err := UpdateOptionsBulk(map[string]string{"CustomCallbackAddress": value, "MinTopUp": "5"})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "private-secret")
			var options []Option
			require.NoError(t, db.Order("key").Find(&options).Error)
			assert.Equal(t, []Option{{Key: "CustomCallbackAddress", Value: "https://callback.example.test"}, {Key: "MinTopUp", Value: "1"}}, options)
		})
	}
}

func TestCustomPaymentCallbackAcceptsClearAndIPOrigins(t *testing.T) {
	db := useWalletIngressDB(t)
	previousMap := common.OptionMap
	previousCallback := operation_setting.CustomCallbackAddress
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		common.OptionMap = previousMap
		operation_setting.CustomCallbackAddress = previousCallback
	})
	for _, value := range []string{"http://37.114.34.132:13000/", "https://wallet.example.test:443/", "http://[::1]:13000/", "", "  "} {
		t.Run(value, func(t *testing.T) {
			require.NoError(t, UpdateOption("CustomCallbackAddress", value))
			var option Option
			require.NoError(t, db.Where(&Option{Key: "CustomCallbackAddress"}).First(&option).Error)
			assert.Equal(t, value, option.Value)
			assert.Equal(t, value, operation_setting.CustomCallbackAddress)
		})
	}
}
