package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPaymentConfigUsesValidatedCallbackAndReturnOrigins(t *testing.T) {
	for _, rail := range []struct {
		name, addressKey, methodsKey string
		setup                        func(*testing.T, string) *gorm.DB
		load                         func(context.Context) (bool, string, string, error)
	}{
		{"epay", "PayAddress", "PayMethods", epayServiceTestDB, func(ctx context.Context) (bool, string, string, error) {
			config, err := LoadEpayConfig(ctx)
			if err != nil {
				return false, "", "", err
			}
			return config.Enabled(), config.NotifyURL, config.ReturnURL, nil
		}},
		{"gmpay", "GmpayPayAddress", "GmpayAssets", gmpayServiceTestDB, func(ctx context.Context) (bool, string, string, error) {
			config, err := LoadGmpayConfig(ctx)
			if err != nil {
				return false, "", "", err
			}
			return config.Enabled(), config.NotifyURL, config.ReturnURL, nil
		}},
	} {
		t.Run(rail.name, func(t *testing.T) {
			for _, test := range []struct{ name, server, custom, callback, returnOrigin string }{
				{"http IP fallback", "http://37.114.34.132:13000/", "", "http://37.114.34.132:13000", "http://37.114.34.132:13000"},
				{"custom IP", "https://Wallet.Example.test:443/", "http://37.114.34.132:18000/", "http://37.114.34.132:18000", "https://wallet.example.test"},
				{"blank callback fallback", "https://wallet.example.test/", "  ", "https://wallet.example.test", "https://wallet.example.test"},
				{"IPv6", "http://[::1]:3000/", "http://[::1]:4000/", "http://[::1]:4000", "http://[::1]:3000"},
			} {
				t.Run(test.name, func(t *testing.T) {
					db := rail.setup(t, "https://pay.example.test")
					setEpayTestOption(t, db, "ServerAddress", test.server)
					setEpayTestOption(t, db, "CustomCallbackAddress", test.custom)
					enabled, notifyURL, returnURL, err := rail.load(context.Background())
					require.NoError(t, err)
					assert.True(t, enabled)
					assert.Equal(t, test.callback+"/api/user/"+rail.name+"/notify", notifyURL)
					assert.Equal(t, test.returnOrigin+"/usage-logs", returnURL)
				})
			}
			for _, key := range []string{"ServerAddress", "CustomCallbackAddress"} {
				for _, value := range []string{"wallet.example.test", "https://wallet.example.test/path", "https://wallet.example.test?", "https://wallet.example.test#", "https://user:private-secret@wallet.example.test", "http://37.114.34.132:65536"} {
					t.Run("invalid stored "+key+" "+value, func(t *testing.T) {
						db := rail.setup(t, "https://pay.example.test")
						setEpayTestOption(t, db, key, value)
						enabled, notifyURL, returnURL, err := rail.load(context.Background())
						require.Error(t, err)
						assert.NotContains(t, err.Error(), "private-secret")
						assert.False(t, enabled)
						assert.Empty(t, notifyURL)
						assert.Empty(t, returnURL)
					})
				}
			}
			for _, option := range []struct{ key, value string }{
				{rail.addressKey, ""},
				{rail.methodsKey, "[]"},
				{"payment_setting.compliance_confirmed", "false"},
			} {
				t.Run("disabled "+option.key, func(t *testing.T) {
					db := rail.setup(t, "https://pay.example.test")
					setEpayTestOption(t, db, option.key, option.value)
					setEpayTestOption(t, db, "ServerAddress", "wallet.example.test")
					setEpayTestOption(t, db, "CustomCallbackAddress", "https://wallet.example.test/old/path")
					enabled, notifyURL, returnURL, err := rail.load(context.Background())
					require.NoError(t, err)
					assert.False(t, enabled)
					assert.Empty(t, notifyURL)
					assert.Empty(t, returnURL)
				})
			}
		})
	}
}

func TestEpayInvalidCurrentCallbackDoesNotBlockFrozenSettlement(t *testing.T) {
	db := epayServiceTestDB(t, "https://pay.example.test")
	ctx := context.Background()
	order, err := CreateEpayTopUp(ctx, 1, EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.NoError(t, err)
	setEpayTestOption(t, db, "CustomCallbackAddress", "https://wallet.example.test/old/path")
	_, err = LoadEpayConfig(ctx)
	require.Error(t, err)
	require.NoError(t, SettleEpayNotification(ctx, epayTestNotification(order, "frozen-origin-receipt", "test-only-key"), ""))
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 5_000_000, user.Quota)
}

func TestGmpayInvalidCurrentCallbackDoesNotBlockFrozenSettlement(t *testing.T) {
	db := gmpayServiceTestDB(t, "https://pay.example.test")
	order := &model.TopUp{UserId: 1, TradeNo: "frozen-origin-gmpay"}
	gmpayTestSnapshot(t, order, 5_000_000)
	setGmpayTestOption(t, db, "CustomCallbackAddress", "https://wallet.example.test/old/path")
	_, err := LoadGmpayConfig(context.Background())
	require.Error(t, err)
	require.NoError(t, SettleGmpayNotification(context.Background(), gmpayTestNotification(order, "frozen-origin-receipt", gmpayTestSecret, nil), ""))
	assert.EqualValues(t, 5_000_000, gmpayTestUserQuota(t, db))
}
