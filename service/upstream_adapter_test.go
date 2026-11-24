package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mockUpstream(t *testing.T, platform string, f http.HandlerFunc) *upstreamAdapter {
	t.Helper()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	a, err := newUpstreamAdapter(platform, s.URL, "", UpstreamCredential{})
	if err != nil {
		t.Fatal(err)
	}
	a.client = s.Client()
	return a
}

func TestSub2APIAdapterLogin2FAAndRefresh(t *testing.T) {
	a := mockUpstream(t, "sub2api", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Ren2Hub-Upstream/1.0" {
			t.Error("user agent")
		}
		switch r.URL.Path {
		case "/api/v1/auth/login":
			fmt.Fprint(w, `{"code":0,"data":{"requires_2fa":true,"temp_token":"flow"}}`)
		case "/api/v1/auth/login/2fa", "/api/v1/auth/refresh":
			fmt.Fprint(w, `{"code":0,"data":{"access_token":"access","refresh_token":"rotation","expires_in":86400}}`)
		default:
			t.Error(r.URL.Path)
		}
	})
	ctx := context.Background()
	var c UpstreamCredential
	save := func(cred UpstreamCredential) error { c = cred; return nil }
	flow, e := a.Login(ctx, "user", "password", "", 0, save)
	if e != nil || flow != "flow" {
		t.Fatal(e, flow)
	}
	e = a.Complete(ctx, flow, "123456", "", 0, save)
	if e != nil || c.AccessToken != "access" {
		t.Fatal("login failed", e)
	}
	a.cred = c
	c, e = a.Refresh(ctx)
	if e != nil || c.RefreshExpiresAt < time.Now().Add(29*24*time.Hour).Unix() {
		t.Fatal("refresh failed", e)
	}
}

func TestSub2APIAdapterErrorEnvelopesAndNonJSON(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{401, `{"code":401,"reason":"TOKEN_EXPIRED"}`, "token_expired"},
		{401, `{"code":"TOKEN_EXPIRED","message":"private"}`, "token_expired"},
		{401, `{"error":{"code":"TOKEN_EXPIRED","message":"private"}}`, "token_expired"},
		{502, `<html>private</html>`, "upstream_http_502"},
	} {
		a := mockUpstream(t, "sub2api", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
		if _, err := a.Probe(context.Background()); err == nil || err.Error() != tc.code {
			t.Fatal(err, tc.code)
		}
	}
}

func TestSub2APIAdapterKeyCRUDRedactionAndIdempotency(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	ids := make(chan string, 2)
	done := make(chan struct{})
	defer close(done)
	raw := "sk-sensitive-0123456789"
	row := `{"id":2,"key":"sk-sensitive-0123456789","status":"quota_exhausted"}`
	expiry := "2030-11-19T13:00:00Z"
	a := mockUpstream(t, "sub2api", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			ids <- r.Header.Get("Idempotency-Key")
			var req map[string]any
			if e := common.DecodeJson(r.Body, &req); e != nil || req["expires_in_days"] != float64(7) || req["expires_at"] != nil {
				t.Error("create expiry")
			}
			if calls.Add(1) == 1 {
				select {
				case <-done:
				case <-r.Context().Done():
				}
				return
			}
		case "PUT":
			var req map[string]any
			if e := common.DecodeJson(r.Body, &req); e != nil {
				t.Error(e)
			}
			if req["status"] != "inactive" && (req["expires_at"] != expiry || req["expires_in_days"] != nil) {
				t.Error("update fields")
			}
		case "GET":
			if r.URL.Path == "/api/v1/keys" {
				fmt.Fprintf(w, `{"code":0,"data":{"page":1,"page_size":10,"pages":1,"total":1,"items":[%s]}}`, row)
				return
			}
		case "DELETE":
			if r.URL.Path != "/api/v1/keys/2" {
				t.Error(r.URL.Path)
			}
		default:
			t.Error(r.Method)
		}
		fmt.Fprintf(w, `{"code":0,"data":%s}`, row)
	})
	a.client.Timeout = 500 * time.Millisecond
	name := "name"
	days := 7
	k, e := a.CreateKey(ctx, UpstreamKeyInput{Name: &name, ExpiresInDays: &days})
	if e != nil {
		t.Fatal(e)
	}
	first, second := <-ids, <-ids
	if _, e := uuid.Parse(first); e != nil || first != second || calls.Load() != 2 {
		t.Fatal("retry did not reuse UUID")
	}
	if k.Status != "exhausted" || k.KeySHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) {
		t.Fatal("key normalization failed")
	}
	got, e := a.GetKey(ctx, 2)
	require.NoError(t, e)
	require.Equal(t, k, got)
	listed, e := a.ListKeys(ctx)
	require.NoError(t, e)
	require.Equal(t, []UpstreamRemoteKey{k}, listed)
	b, _ := common.Marshal(listed)
	if strings.Contains(string(b), raw) {
		t.Fatal("plaintext exposed")
	}
	if e := a.UpdateKey(ctx, 2, UpstreamKeyInput{ExpiresAt: &expiry}); e != nil {
		t.Fatal(e)
	}
	if e := a.SetKeyEnabled(ctx, 2, false); e != nil {
		t.Fatal(e)
	}
	if e := a.DeleteKey(ctx, 2); e != nil {
		t.Fatal(e)
	}
}

func TestSub2APIAdapterSnapshotGroupsAndRates(t *testing.T) {
	ctx := context.Background()
	a := mockUpstream(t, "sub2api", func(w http.ResponseWriter, r *http.Request) {
		data := map[string]string{
			"/api/v1/settings/public":        `{"site_name":"test","version":"0.2.25","turnstile_enabled":true}`,
			"/api/v1/auth/me":                `{"email":"member@example.test","balance":12.3}`,
			"/api/v1/groups/available":       `[{"id":3,"name":"group","platform":"antigravity","rate_multiplier":2}]`,
			"/api/v1/groups/rates":           `{"3":1.25}`,
			"/api/v1/subscriptions/progress": `[{"subscription":{"id":17,"group_id":3,"status":"active","access_token":"REDACT"},"progress":{}}]`,
		}
		fmt.Fprintf(w, `{"code":0,"data":%s}`, data[r.URL.Path])
	})
	if caps, e := a.Probe(ctx); e != nil || caps.Version != "0.2.25" || !caps.TurnstileEnabled || caps.PasswordLoginAvailable {
		t.Fatal(caps, e)
	}
	s, e := a.Snapshot(ctx)
	if e != nil || s.Username != "member@example.test" || s.Balance != "12.3" || len(s.Groups) != 1 || s.Groups[0].Ratio != "1.25" || len(s.Subscriptions) != 1 || s.Subscriptions[0].GroupID != 3 {
		t.Fatal(s, e)
	}
	b, _ := common.Marshal(s)
	if strings.Contains(string(b), "REDACT") {
		t.Fatal("snapshot leaked unprojected field")
	}
}

func TestNewAPIAdapterPasswordV2PATExchangeAndOldPATCleanup(t *testing.T) {
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	pub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	ctx := context.Background()
	var seen []string
	a := mockUpstream(t, "newapi", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		var req map[string]any
		if r.Method == "POST" && r.ContentLength > 0 {
			if e := common.DecodeJson(r.Body, &req); e != nil {
				t.Error(e)
			}
		}
		var data any
		switch r.URL.Path {
		case "/api/user/login/encryption-key":
			data = map[string]any{"kid": "test", "public_key": pub, "enabled": true}
		case "/api/user/login":
			ciphertext, _ := req["password_encrypted"].(string)
			parts := strings.Split(ciphertext, ".")
			if len(parts) != 4 || parts[0] != "v2" || req["password"] != nil {
				t.Error("invalid hybrid envelope")
				return
			}
			wrapped, _ := base64.StdEncoding.DecodeString(parts[1])
			nonce, _ := base64.StdEncoding.DecodeString(parts[2])
			ct, _ := base64.StdEncoding.DecodeString(parts[3])
			secret, e := rsa.DecryptOAEP(sha256.New(), rand.Reader, k, wrapped, []byte("password-v2"))
			if e != nil {
				t.Error(e)
				return
			}
			block, e := aes.NewCipher(secret)
			if e != nil {
				t.Error(e)
				return
			}
			box, e := cipher.NewGCM(block)
			if e != nil {
				t.Error(e)
				return
			}
			p, e := box.Open(nil, nonce, ct, []byte("password-v2:test"))
			if e != nil || string(p) != "password" {
				t.Error("password did not decrypt")
			}
			data = map[string]any{"flow_token": "flow", "require_verification": true}
		case "/api/user/login/verify":
			if req["code"] != "123456" || req["flow_token"] != "flow" {
				t.Error("invalid login verify")
			}
			data = map[string]any{"access_token": "session"}
		case "/api/verify":
			if req["code"] != "123456" || r.Header.Get("Authorization") != "Bearer session" {
				t.Error("invalid proof request")
			}
			data = map[string]any{"proof_token": req["scope"]}
		case "/api/user/access_tokens":
			if req["name"] != "ren2hub-12345678" || r.Header.Get("X-Security-Proof") != "access_token.generate" {
				t.Error("invalid issuance")
			}
			data = map[string]any{"item": map[string]int{"id": 4}, "token": "nap_new"}
		case "/api/user/access_tokens/3":
			if r.Header.Get("X-Security-Proof") != "access_token.revoke" {
				t.Error("invalid revocation")
			}
		case "/api/user/auth/logout":
			if r.Header.Get("Origin") != "http://"+r.Host {
				t.Error("logout missing Origin")
			}
		default:
			t.Error(r.URL.Path)
		}
		b, _ := common.Marshal(map[string]any{"success": true, "data": data})
		w.Write(b)
	})
	var cred UpstreamCredential
	save := func(c UpstreamCredential) error { cred = c; seen = append(seen, "persist"); return nil }
	flow, e := a.Login(ctx, "username", "password", "12345678", 3, save)
	if e != nil {
		t.Fatal(e)
	}
	e = a.Complete(ctx, flow, "123456", "12345678", 3, save)
	if e != nil || cred.PAT != "nap_new" || cred.RemotePATID != 4 {
		t.Fatal("PAT exchange failed", e)
	}
	if strings.Join(seen, "|") != "GET /api/user/login/encryption-key|POST /api/user/login|POST /api/user/login/verify|POST /api/verify|POST /api/user/access_tokens|persist|POST /api/verify|DELETE /api/user/access_tokens/3|POST /api/user/auth/logout" {
		t.Fatal(seen)
	}
	t.Run("PAT limit logs out", func(t *testing.T) {
		logout := false
		a := mockUpstream(t, "newapi", func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/verify":
				fmt.Fprint(w, `{"success":true,"data":{"proof_token":"p"}}`)
			case "/api/user/access_tokens":
				w.WriteHeader(400)
				fmt.Fprint(w, `{"success":false,"code":"ACCESS_TOKEN_LIMIT"}`)
			case "/api/user/auth/logout":
				logout = true
				fmt.Fprint(w, `{"success":true}`)
			default:
				t.Error(r.URL.Path)
			}
		})
		e := a.issuePAT(ctx, "session", "12345678", 3, map[string]any{"method": "2fa", "code": "code"}, save)
		if e == nil || e.Error() != "remote_pat_limit" || !logout {
			t.Fatal(e, logout)
		}
	})
}

func TestNewAPIAdapterGroupsAutoEmptyGroupAndEmpty429(t *testing.T) {
	ctx := context.Background()
	a := mockUpstream(t, "newapi", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			fmt.Fprint(w, `{"success":true,"data":{"version":"1","turnstile_check":false,"password_login_enabled":true,"quota_per_unit":1234,"quota_display_type":"CUSTOM","custom_currency_symbol":"points","custom_currency_exchange_rate":2.5}}`)
		case "/api/user/self":
			fmt.Fprint(w, `{"success":true,"data":{"group":"vip","quota":123}}`)
		case "/api/user/self/groups":
			fmt.Fprint(w, `{"success":true,"data":{"auto":{"ratio":"auto"},"vip":{"ratio":1.45}}}`)
		default:
			w.Header().Set("Retry-After", "90")
			w.WriteHeader(429)
		}
	})
	s, e := a.Snapshot(ctx)
	if e != nil || s.QuotaPerUnit != 1234 || s.QuotaDisplayType != "CUSTOM" || s.CustomCurrencyExchangeRate != 2.5 || s.UserGroup != "vip" || len(s.Groups) != 2 || s.Groups[0].Ratio != "auto" || s.Groups[1].Ratio != "1.45" {
		t.Fatal(s, e)
	}
	keys, e := a.ListKeys(ctx)
	var re *UpstreamRemoteError
	if keys != nil || !errors.As(e, &re) || re.RetryAfter != 90*time.Second {
		t.Fatal(keys, e)
	}
}

func TestNewAPIAdapterTokenCreateAmbiguousLocateAndSafeUpdate(t *testing.T) {
	ctx := context.Background()
	name := "remote"
	item := `{"id":2,"name":"remote","status":1,"model_limits":"m","allow_ips":"127.0.0.1","model_limits_enabled":true,"remain_quota":320,"unlimited_quota":false,"expired_time":-1,"group":"vip","cross_group_retry":false}`
	for _, matches := range []int{0, 1, 2} {
		created := false
		a := mockUpstream(t, "newapi", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				if r.URL.Path == "/api/token/2" {
					fmt.Fprintf(w, `{"success":true,"data":%s}`, item)
					return
				}
				if r.URL.Query().Get("page_size") != "20" {
					t.Error("create lookup size")
				}
				rows := ""
				if created && matches > 0 {
					rows = item
					if matches > 1 {
						rows += "," + strings.Replace(item, `"id":2`, `"id":3`, 1)
					}
				}
				fmt.Fprintf(w, `{"success":true,"data":{"total":%d,"items":[%s]}}`, strings.Count(rows, `"id"`), rows)
			case "POST":
				if r.URL.Path == "/api/token/2/key" {
					fmt.Fprint(w, `{"success":true,"data":{"key":"revealed"}}`)
					return
				}
				var req map[string]any
				_ = common.DecodeJson(r.Body, &req)
				if req["expired_time"] != float64(-1) {
					t.Error("bad expiry")
				}
				created = true
				fmt.Fprint(w, `{"success":true}`)
			case "PUT":
				var req map[string]any
				_ = common.DecodeJson(r.Body, &req)
				if r.URL.Query().Has("status_only") {
					if r.URL.Query().Get("status_only") != "1" || req["status"] != float64(2) {
						t.Error("invalid status change")
					}
				} else if req["allow_ips"] != "127.0.0.1" || req["model_limits"] != "m" || req["group"] != "vip" || req["remain_quota"] != float64(320) {
					t.Error("overwrote unrelated token settings")
				}
				fmt.Fprint(w, `{"success":true}`)
			default:
				t.Error(r.Method)
			}
		})
		long := strings.Repeat("字", 17)
		_, e := a.CreateKey(ctx, UpstreamKeyInput{Name: &long})
		if e == nil || created {
			t.Fatal("long name accepted")
		}
		k, e := a.CreateKey(ctx, UpstreamKeyInput{Name: &name})
		if matches != 1 {
			if e == nil || e.Error() != "remote_key_locate_ambiguous" {
				t.Fatal(e)
			}
			continue
		}
		if e != nil || k.ID != 2 {
			t.Fatal(e, k)
		}
		if e := a.UpdateKey(ctx, 2, UpstreamKeyInput{Name: &name}); e != nil {
			t.Fatal(e)
		}
		if e := a.SetKeyEnabled(ctx, 2, false); e != nil {
			t.Fatal(e)
		}
		if raw, e := a.Reveal(ctx, 2); e != nil || raw != "revealed" {
			t.Fatal("reveal failed", e)
		}
	}
}
