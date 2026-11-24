package service

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpstreamCredentialCryptoRoundTripAndAAD(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "fixture")
	account := &model.UpstreamAccount{CredentialUID: "first", Platform: "sub2api"}
	cred := UpstreamCredential{RefreshToken: "private-fixture"}
	encoded, err := sealUpstreamCredential(account, cred)
	require.NoError(t, err)
	require.NotContains(t, encoded, cred.RefreshToken)
	account.CredentialCiphertext = encoded
	got, err := openUpstreamCredential(account)
	require.NoError(t, err)
	require.Equal(t, cred, got)
	account.CredentialUID = "second"
	_, err = openUpstreamCredential(account)
	require.Error(t, err)
	account.CredentialUID = "first"
	account.Platform = "newapi"
	_, err = openUpstreamCredential(account)
	require.Error(t, err)
}
func TestUpstreamCredentialRequiresPersistentSecret(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	_, err := sealUpstreamCredential(&model.UpstreamAccount{}, UpstreamCredential{})
	require.Error(t, err)
}
func TestUpstreamCredentialUndecryptableMarksAccount(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccount{}))
	a := &model.UpstreamAccount{CredentialUID: "fixture", Status: "active", Platform: "newapi", CredentialCiphertext: "invalid"}
	require.NoError(t, model.CreateUpstreamAccount(a))
	_, err := credentialForAccount(a)
	require.Error(t, err)
	saved, err := model.GetUpstreamAccount(a.ID)
	require.NoError(t, err)
	require.Equal(t, "credential_undecryptable", saved.Status)
	require.Equal(t, "invalid", saved.CredentialCiphertext)
}

func rotationFixture(t *testing.T) (*model.UpstreamAccount, *atomic.Int32) {
	db := setupUpstreamRatioTest(t)
	t.Setenv("CRYPTO_SECRET", "test")
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			calls.Add(1)
			fmt.Fprint(w, `{"code":0,"data":{"refresh_token":"rotated-secret","access_token":"new-access","expires_in":86400}}`)
		case "/api/v1/keys":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"total":0,"page":1,"page_size":200,"pages":0,"items":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	a := &model.UpstreamAccount{CredentialUID: "rotate", Platform: "sub2api", Status: "active", BaseURL: server.URL}
	var err error
	a.CredentialCiphertext, err = sealUpstreamCredential(a, UpstreamCredential{RefreshToken: "old-refresh"})
	require.NoError(t, err)
	require.NoError(t, db.Create(a).Error)
	t.Cleanup(func() { pendingRotations.Delete(a.ID) })
	return a, calls
}
func TestSub2APIConcurrentRefreshSerializesSingleRotation(t *testing.T) {
	a, calls := rotationFixture(t)
	errors := make(chan error, 8)
	for range 8 {
		go func() { _, err := ListManagedKeys(context.Background(), a.ID); errors <- err }()
	}
	for range 8 {
		require.NoError(t, <-errors)
	}
	require.Equal(t, int32(1), calls.Load())
}
func TestSub2APIAdapterRefreshPersistFailureKeepsPendingTokens(t *testing.T) {
	a, calls := rotationFixture(t)
	require.NoError(t, model.DB.Exec("CREATE TRIGGER fail_rotation BEFORE UPDATE OF credential_ciphertext ON upstream_accounts BEGIN SELECT RAISE(ABORT, 'simulated persistence outage'); END").Error)
	_, err := ListManagedKeys(context.Background(), a.ID)
	require.Error(t, err)
	_, err = ListManagedKeys(context.Background(), a.ID)
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.NoError(t, model.DB.Exec("DROP TRIGGER fail_rotation").Error)
	_, err = ListManagedKeys(context.Background(), a.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
	saved, err := model.GetUpstreamAccount(a.ID)
	require.NoError(t, err)
	got, err := openUpstreamCredential(saved)
	require.NoError(t, err)
	require.Equal(t, "rotated-secret", got.RefreshToken)
}

func TestUpstreamChallengeLimitsOwnerExpiryAndSingleUse(t *testing.T) {
	upstreamChallenges.Lock()
	before := upstreamChallenges.entries
	upstreamChallenges.entries = map[string]upstreamChallenge{"expired": {admin: 90, until: time.Now().Add(-time.Minute)}}
	upstreamChallenges.Unlock()
	t.Cleanup(func() { upstreamChallenges.Lock(); upstreamChallenges.entries = before; upstreamChallenges.Unlock() })
	var out UpstreamAuthResult
	for user := 1; user <= 16; user++ {
		for range 4 {
			r, e := storeUpstreamChallenge(upstreamChallenge{admin: user})
			require.NoError(t, e)
			out = r
		}
		_, err := storeUpstreamChallenge(upstreamChallenge{admin: user})
		require.EqualError(t, err, "challenge_limit")
	}
	_, e := storeUpstreamChallenge(upstreamChallenge{admin: 17})
	require.EqualError(t, e, "challenge_limit")
	_, e = CompleteUpstreamAuth(context.Background(), 17, out.Challenge, "")
	require.EqualError(t, e, "challenge_invalid")
	_, e = CompleteUpstreamAuth(context.Background(), 16, out.Challenge, "")
	require.EqualError(t, e, "invalid_verification_code")
	_, e = CompleteUpstreamAuth(context.Background(), 16, out.Challenge, "001234")
	require.EqualError(t, e, "unsupported_platform")
	_, e = CompleteUpstreamAuth(context.Background(), 16, out.Challenge, "")
	require.EqualError(t, e, "challenge_invalid")
}
