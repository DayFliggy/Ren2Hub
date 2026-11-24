package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAuthPATPersistFailureKeepsOldAndCleansNew(t *testing.T) {
	for _, secondFactor := range []bool{false, true} {
		t.Run(fmt.Sprintf("two_factor_%t", secondFactor), func(t *testing.T) {
			db := setupUpstreamRatioTest(t)
			t.Setenv("CRYPTO_SECRET", "fixture")
			const previous = 7
			var a model.UpstreamAccount
			var issued, revoked, logouts atomic.Int32
			issued.Store(previous)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data any
				switch r.URL.Path {
				case "/api/user/login/encryption-key":
					data = map[string]bool{"enabled": false}
				case "/api/user/login":
					if secondFactor {
						data = map[string]any{"require_verification": true, "flow_token": "flow"}
					} else {
						data = map[string]string{"access_token": "session"}
					}
				case "/api/user/login/verify":
					data = map[string]string{"access_token": "session"}
				case "/api/verify":
					data = map[string]string{"proof_token": "proof"}
				case "/api/user/access_tokens":
					id := issued.Add(1)
					data = map[string]any{"item": map[string]int32{"id": id}, "token": fmt.Sprintf("nap_fixture_%d", id)}
				case "/api/user/auth/logout":
					logouts.Add(1)
				default:
					id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/user/access_tokens/"))
					if err != nil || r.Method != "DELETE" {
						t.Error("unexpected endpoint", r.Method, r.URL.Path)
						w.WriteHeader(400)
						return
					}
					saved, e := model.GetUpstreamAccount(a.ID)
					if e != nil {
						t.Error(e)
						return
					}
					cred, e := openUpstreamCredential(saved)
					if e != nil || cred.RemotePATID == id {
						t.Error("revoked locally active credential", id, e)
					}
					revoked.Store(int32(id))
				}
				b, err := common.Marshal(map[string]any{"success": true, "data": data})
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = w.Write(b)
			}))
			t.Cleanup(server.Close)
			a = model.UpstreamAccount{Name: "fixture", Platform: "newapi", BaseURL: server.URL, CredentialUID: "12345678-uid", Status: "active"}
			var err error
			a.CredentialCiphertext, err = sealUpstreamCredential(&a, UpstreamCredential{PAT: "nap_old", RemotePATID: previous})
			require.NoError(t, err)
			require.NoError(t, model.CreateUpstreamAccount(&a))
			run := func() error {
				out, err := StartUpstreamAuth(context.Background(), 1, UpstreamAuthRequest{AccountID: a.ID, Username: "u", Password: "p"})
				if err != nil {
					return err
				}
				if secondFactor {
					require.NotEmpty(t, out.Challenge)
					out, err = CompleteUpstreamAuth(context.Background(), 1, out.Challenge, "001234")
				}
				if err == nil {
					require.Equal(t, a.ID, out.AccountID)
				}
				return err
			}
			require.NoError(t, db.Exec("CREATE TRIGGER fail_reauth BEFORE UPDATE OF credential_ciphertext ON upstream_accounts BEGIN SELECT RAISE(ABORT, 'write failed'); END").Error)
			require.Error(t, run())
			require.EqualValues(t, 8, revoked.Load())
			require.EqualValues(t, 1, logouts.Load())
			saved, e := model.GetUpstreamAccount(a.ID)
			require.NoError(t, e)
			require.Equal(t, a.CredentialCiphertext, saved.CredentialCiphertext)
			require.NoError(t, db.Exec("DROP TRIGGER fail_reauth").Error)
			require.NoError(t, run())
			require.EqualValues(t, previous, revoked.Load())
			require.EqualValues(t, 2, logouts.Load())
			saved, e = model.GetUpstreamAccount(a.ID)
			require.NoError(t, e)
			cred, e := openUpstreamCredential(saved)
			require.NoError(t, e)
			require.Equal(t, "nap_fixture_9", cred.PAT)
		})
	}
}

func TestUpstreamAuthInvalidVerificationPreservesChallenge(t *testing.T) {
	setupUpstreamRatioTest(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(400) }))
	defer server.Close()
	challenge, e := storeUpstreamChallenge(upstreamChallenge{admin: 7, platform: "sub2api", base: server.URL, token: "flow"})
	require.NoError(t, e)
	for _, code := range []string{"", "12345", "1234567", "12345a", "+12345", "12 345", "１２３４５６"} {
		_, e = CompleteUpstreamAuth(context.Background(), 7, challenge.Challenge, code)
		require.EqualError(t, e, "invalid_verification_code")
	}
	require.Zero(t, calls.Load())
	_, e = CompleteUpstreamAuth(context.Background(), 7, challenge.Challenge, "001234")
	require.EqualError(t, e, "upstream_http_400")
	_, e = CompleteUpstreamAuth(context.Background(), 7, challenge.Challenge, "001234")
	require.EqualError(t, e, "challenge_invalid")
	require.EqualValues(t, 1, calls.Load())
}
