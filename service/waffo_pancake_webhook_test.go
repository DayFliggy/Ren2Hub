package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaffoPancakeCheckoutRequiresConfiguredStore(t *testing.T) {
	previous := setting.WaffoPancakeStoreID
	setting.WaffoPancakeStoreID = " "
	t.Cleanup(func() { setting.WaffoPancakeStoreID = previous })
	session, err := CreateWaffoPancakeCheckoutSession(context.Background(), &WaffoPancakeCreateSessionParams{
		BuyerIdentity: WaffoPancakeBuyerIdentityFromUserID(1), OrderMerchantExternalID: "test-order",
	})
	require.EqualError(t, err, "missing Waffo Pancake store id")
	assert.Nil(t, session)
}

func TestWaffoPancakeWebhookBindsVerifiedStore(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	t.Setenv("WAFFO_WEBHOOK_TEST_PUBLIC_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})))
	previous := setting.WaffoPancakeStoreID
	t.Cleanup(func() { setting.WaffoPancakeStoreID = previous })
	for _, tc := range []struct {
		name, configured, received string
		validSignature, accepted   bool
	}{
		{"matching store", "STO_local", "STO_local", true, true},
		{"trim configured store", " STO_local ", "STO_local", true, true},
		{"other signed store", "STO_local", "STO_other", true, false},
		{"missing configured store", "", "STO_local", true, false},
		{"missing event store", "STO_local", "", true, false},
		{"invalid signature", "STO_local", "STO_local", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.WaffoPancakeStoreID = tc.configured
			body, err := common.Marshal(map[string]any{
				"id": "evt_local", "eventId": "payment_local", "eventType": "order.completed",
				"storeId": tc.received, "mode": "test",
				"data": map[string]any{"orderMerchantExternalId": "order_local", "merchantProvidedBuyerIdentity": "new-api-user-1"},
			})
			require.NoError(t, err)
			timestamp := fmt.Sprint(time.Now().UnixMilli())
			hash := sha256.Sum256([]byte(timestamp + "." + string(body)))
			signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
			require.NoError(t, err)
			if !tc.validSignature {
				signature[0] ^= 1
			}
			header := "t=" + timestamp + ",v1=" + base64.StdEncoding.EncodeToString(signature)
			event, err := VerifyConfiguredWaffoPancakeWebhook(string(body), header)
			if !tc.accepted {
				require.Error(t, err)
				assert.Nil(t, event)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, event)
			assert.Equal(t, "STO_local", event.StoreID)
			assert.Equal(t, "evt_local", event.ID)
			// Signature/store validation is repeatable; order settlement owns idempotence.
			repeated, err := VerifyConfiguredWaffoPancakeWebhook(string(body), header)
			require.NoError(t, err)
			assert.Equal(t, event, repeated)
		})
	}
}
