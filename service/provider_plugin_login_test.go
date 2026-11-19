package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var loginAdmin = PluginLoginIdentity{UserID: 7, SessionID: "session-a"}

// visitAuthorize follows the step's URL like the browser does and returns the
// query the vendor redirects back with.
func visitAuthorize(t *testing.T, stepURL string) url.Values {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(stepURL)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusFound, response.StatusCode)
	location, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	return location.Query()
}

func callbackParams(values url.Values) map[string]string {
	params := make(map[string]string, len(values))
	for name := range values {
		params[name] = values.Get(name)
	}
	return params
}

func flowRow(t *testing.T, token string) *model.AuthFlow {
	t.Helper()
	flow, err := model.GetAuthFlow(token, model.AuthFlowMatch{Purpose: model.AuthFlowPurposePluginLogin})
	require.NoError(t, err)
	return flow
}

type loginFixture struct {
	server  *fakeAuthServer
	plugin  *jsplugin.LoadedPlugin
	channel *model.Channel
}

func newLoginFixture(t *testing.T, key, mode string) loginFixture {
	t.Helper()
	useProviderPluginDatabase(t)
	server := newFakeAuthServer(t)
	plugin := registerProviderPlugin(t, key, mode)
	channel := createProviderChannel(t, key, server.URL, "")
	return loginFixture{server: server, plugin: plugin, channel: channel}
}

func (f loginFixture) start(t *testing.T) PluginLoginResult {
	t.Helper()
	result, err := StartPluginLogin(context.Background(), loginAdmin, f.channel.Id)
	require.NoError(t, err)
	return result
}

func TestPluginLoginRedirect(t *testing.T) {
	f := newLoginFixture(t, "login-redirect", "redirect")
	PluginLoginNeededStatus(f.channel)
	require.NoError(t, model.DB.Model(f.channel).Updates(map[string]any{"status": f.channel.Status, "other_info": f.channel.OtherInfo}).Error)

	started := f.start(t)

	require.Equal(t, "redirect", started.Step.Type)
	authorize, err := url.Parse(started.Step.URL)
	require.NoError(t, err)
	query := authorize.Query()
	assert.Equal(t, started.Flow, query.Get("state"))
	assert.GreaterOrEqual(t, len(started.Flow), 43, "state carries at least 32 random bytes")
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	assert.NotEmpty(t, query.Get("code_challenge"))
	assert.NotEmpty(t, query.Get("nonce"))
	assert.Equal(t, strings.TrimRight(system_setting.ServerAddress, "/")+"/oauth/plugin/login-redirect/callback", query.Get("redirect_uri"))

	done, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-redirect", callbackParams(visitAuthorize(t, started.Step.URL)))

	require.NoError(t, err)
	require.Equal(t, "done", done.Step.Type)
	assert.Equal(t, f.channel.Id, done.ChannelID)
	require.NotNil(t, done.Step.Credential)
	assert.Equal(t, "acct-1", done.Step.Credential.AccountID)
	assert.Equal(t, "acct-1@example.com", done.Step.Credential.Label)
	stored := storedCredential(t, f.channel.Id)
	assert.Equal(t, "login-redirect", stored.Plugin)
	assert.Equal(t, "Bearer access-1", stored.Headers["Authorization"])
	assert.JSONEq(t, `{"access":"access-1","refresh":"refresh-1"}`, string(stored.Data))
	assert.Equal(t, common.ChannelStatusEnabled, storedChannel(t, f.channel.Id).Status)
}

func TestPluginLoginPaste(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state bool
	}{{"with state", true}, {"vendor returns no state", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLoginFixture(t, "login-paste", "paste")
			started := f.start(t)
			require.Equal(t, "paste", started.Step.Type)
			assert.Equal(t, jsplugin.LocalizedText{"en": "Paste the address"}, started.Step.Hint)
			values := visitAuthorize(t, started.Step.URL)
			values.Del("iss")
			if !tc.state {
				values.Del("state")
			}
			address := "http://localhost:1455/auth/callback?" + values.Encode()

			done, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{CallbackURL: "  " + address + " "})

			require.NoError(t, err)
			assert.Equal(t, "done", done.Step.Type)
			assert.Equal(t, "Bearer access-1", storedCredential(t, f.channel.Id).Headers["Authorization"])
		})
	}
}

func TestPluginLoginPasteRejectsBadAddresses(t *testing.T) {
	f := newLoginFixture(t, "login-paste-bad", "paste")
	started := f.start(t)
	for name, tc := range map[string]struct{ address, message string }{
		"no query":     {"http://localhost:1455/auth/callback", "not a valid callback address"},
		"wrong state":  {"http://localhost:1455/auth/callback?code=c&state=other", "does not belong to this login"},
		"oversized":    {"http://localhost/?code=" + strings.Repeat("a", 65<<10), "not a valid callback address"},
		"vendor error": {"http://localhost/?error=access_denied&error_description=secret-detail", "The login was refused"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{CallbackURL: tc.address})
			require.ErrorContains(t, err, tc.message)
			assert.NotContains(t, err.Error(), "secret-detail")
		})
	}
	assert.Zero(t, f.server.tokenCalls.Load(), "none of them reached the plugin")
}

func TestPluginLoginDevice(t *testing.T) {
	f := newLoginFixture(t, "login-device", "device")
	f.server.devicePend.Store(1)

	started := f.start(t)

	require.Equal(t, "device", started.Step.Type)
	assert.Equal(t, "ABCD-EFGH", started.Step.UserCode)
	assert.Equal(t, f.server.URL+"/device", started.Step.VerificationURI)
	assert.Equal(t, 1, started.Step.Interval)
	assert.InDelta(t, time.Now().Add(10*time.Minute).Unix(), started.Step.ExpiresAt, 5)

	poll := PluginLoginInput{Poll: true}
	early, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, poll)
	require.NoError(t, err)
	assert.Equal(t, "pending", early.Step.Type)
	assert.Equal(t, 1, early.Step.Interval)
	assert.Zero(t, f.server.devicePolls.Load(), "a poll that comes too early does not reach the plugin")

	time.Sleep(1100 * time.Millisecond)
	pending, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, poll)
	require.NoError(t, err)
	assert.Equal(t, "pending", pending.Step.Type)
	assert.EqualValues(t, 1, f.server.devicePolls.Load())

	early, err = ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, poll)
	require.NoError(t, err)
	assert.Equal(t, "pending", early.Step.Type)
	assert.EqualValues(t, 1, f.server.devicePolls.Load())

	time.Sleep(1100 * time.Millisecond)
	done, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, poll)
	require.NoError(t, err)
	assert.Equal(t, "done", done.Step.Type)
	assert.EqualValues(t, 2, f.server.devicePolls.Load())
	assert.Equal(t, "Bearer access-1", storedCredential(t, f.channel.Id).Headers["Authorization"])
}

func TestPluginLoginDeviceExpires(t *testing.T) {
	f := newLoginFixture(t, "login-device-end", "shortdevice")
	started := f.start(t)
	assert.InDelta(t, time.Now().Add(time.Second).Unix(), started.Step.ExpiresAt, 1)

	time.Sleep(1100 * time.Millisecond)
	_, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{Poll: true})

	require.ErrorContains(t, err, "The device code has expired")
	assert.Zero(t, f.server.devicePolls.Load())
}

func TestPluginLoginFields(t *testing.T) {
	f := newLoginFixture(t, "login-fields", "fields")
	started := f.start(t)
	require.Equal(t, "fields", started.Step.Type)
	require.Len(t, started.Step.Fields, 2)
	assert.Equal(t, "password", started.Step.Fields[0].Type)

	continueWith := func(fields map[string]string) (PluginLoginResult, error) {
		return ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{Fields: fields})
	}
	_, err := continueWith(map[string]string{"note": "x"})
	require.ErrorContains(t, err, "Login field api_token is required")
	_, err = continueWith(map[string]string{"api_token": "  "})
	require.ErrorContains(t, err, "Login field api_token is required")
	_, err = continueWith(map[string]string{"api_token": "t", "other": "x"})
	require.ErrorContains(t, err, "Login field other is invalid")
	_, err = continueWith(map[string]string{"api_token": strings.Repeat("a", 65<<10)})
	require.ErrorContains(t, err, "does not accept this input")

	done, err := continueWith(map[string]string{"api_token": "secret-token", "note": "main"})

	require.NoError(t, err)
	assert.Equal(t, "done", done.Step.Type)
	assert.Equal(t, "Field main", done.Step.Credential.Label)
	assert.Equal(t, "Bearer secret-token", storedCredential(t, f.channel.Id).Headers["Authorization"])
}

func TestPluginLoginRejectsInputOfAnotherStep(t *testing.T) {
	f := newLoginFixture(t, "login-input", "fields")
	started := f.start(t)
	for name, input := range map[string]PluginLoginInput{
		"poll":     {Poll: true},
		"callback": {CallbackURL: "http://localhost/?code=c"},
		"nothing":  {},
		"two":      {Poll: true, Fields: map[string]string{"api_token": "t"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, input)
			require.ErrorContains(t, err, "does not accept this input")
		})
	}
	_, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-input", map[string]string{"state": started.Flow, "code": "c"})
	require.ErrorContains(t, err, "does not accept this input")
}

func TestPluginLoginCallbackRejections(t *testing.T) {
	const invalid = "Login flow is invalid or has expired"
	const key = "login-reject"
	for name, tc := range map[string]struct {
		identity PluginLoginIdentity
		plugin   string
		params   func(values url.Values) map[string]string
		message  string
	}{
		"wrong state": {params: func(values url.Values) map[string]string {
			values.Set("state", "not-the-flow")
			return callbackParams(values)
		}, message: invalid},
		"no state": {params: func(values url.Values) map[string]string {
			values.Del("state")
			return callbackParams(values)
		}, message: invalid},
		"another session": {identity: PluginLoginIdentity{UserID: loginAdmin.UserID, SessionID: "session-b"}, message: invalid},
		"another admin":   {identity: PluginLoginIdentity{UserID: 8, SessionID: loginAdmin.SessionID}, message: invalid},
		"no session":      {identity: PluginLoginIdentity{UserID: loginAdmin.UserID}, message: invalid},
		"another plugin":  {plugin: "login-reject-other", message: invalid},
		"issuer mismatch": {params: func(values url.Values) map[string]string {
			values.Set("iss", "https://attacker.example")
			return callbackParams(values)
		}, message: "The callback issuer does not match"},
		"issuer missing": {params: func(values url.Values) map[string]string {
			values.Del("iss")
			return callbackParams(values)
		}, message: "The callback issuer does not match"},
		"issuer mismatch on an error": {params: func(values url.Values) map[string]string {
			values.Set("iss", "https://attacker.example")
			values.Set("error", "access_denied")
			return callbackParams(values)
		}, message: "The callback issuer does not match"},
		"vendor error": {params: func(values url.Values) map[string]string {
			values.Del("code")
			values.Set("error", "access_denied")
			values.Set("error_description", "secret-detail")
			return callbackParams(values)
		}, message: "The login was refused: access_denied"},
		"too many params": {params: func(values url.Values) map[string]string {
			params := callbackParams(values)
			for index := range 64 {
				params["p"+string(rune('a'+index%26))+strings.Repeat("x", index)] = "v"
			}
			return params
		}, message: invalid},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLoginFixture(t, key, "redirect")
			registerProviderPlugin(t, "login-reject-other", "redirect")
			createProviderChannel(t, "login-reject-other", f.server.URL, "")
			started := f.start(t)
			values := visitAuthorize(t, started.Step.URL)
			params := callbackParams(values)
			if tc.params != nil {
				params = tc.params(values)
			}
			identity := loginAdmin
			if tc.identity != (PluginLoginIdentity{}) {
				identity = tc.identity
			}
			plugin := key
			if tc.plugin != "" {
				plugin = tc.plugin
			}

			_, err := CompletePluginLoginCallback(context.Background(), identity, plugin, params)

			require.ErrorContains(t, err, tc.message)
			assert.NotContains(t, err.Error(), "secret-detail")
			assert.Zero(t, f.server.tokenCalls.Load(), "the plugin must not run on a rejected callback")
			assert.Empty(t, storedChannel(t, f.channel.Id).Key)

			// The rightful callback still works: a rejection burns nothing.
			done, err := CompletePluginLoginCallback(context.Background(), loginAdmin, key, callbackParams(visitAuthorize(t, started.Step.URL)))
			require.NoError(t, err)
			assert.Equal(t, "done", done.Step.Type)
		})
	}
}

func TestPluginLoginVendorErrorCodeIsSanitized(t *testing.T) {
	f := newLoginFixture(t, "login-vendor-error", "paste")
	started := f.start(t)

	_, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{CallbackURL: "http://localhost/?error=" + url.QueryEscape("<script>alert(1)</script>")})

	var message *common.Message
	require.ErrorAs(t, err, &message)
	assert.Equal(t, "unknown_error", message.Params["error"])
}

func TestPluginLoginExpiredFlow(t *testing.T) {
	f := newLoginFixture(t, "login-expired", "redirect")
	started := f.start(t)
	values := visitAuthorize(t, started.Step.URL)
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposePluginLogin).Update("expires_at", time.Now().Add(-time.Minute)).Error)

	_, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-expired", callbackParams(values))

	require.ErrorContains(t, err, "Login flow is invalid or has expired")
	assert.Zero(t, f.server.tokenCalls.Load())
}

func TestPluginLoginFlowLivesFifteenMinutes(t *testing.T) {
	f := newLoginFixture(t, "login-ttl", "redirect")
	started := f.start(t)
	flow := flowRow(t, started.Flow)
	assert.Equal(t, "login-ttl", flow.Provider)
	assert.Equal(t, loginAdmin.UserID, flow.UserId)
	assert.Equal(t, loginAdmin.SessionID, flow.SessionId)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), flow.ExpiresAt, 5*time.Second)
	assert.NotContains(t, flow.TokenHash, started.Flow, "only the hash of the state is stored")
}

func TestPluginLoginDoneConsumesTheFlow(t *testing.T) {
	f := newLoginFixture(t, "login-replay", "redirect")
	started := f.start(t)
	params := callbackParams(visitAuthorize(t, started.Step.URL))
	_, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-replay", params)
	require.NoError(t, err)

	_, err = CompletePluginLoginCallback(context.Background(), loginAdmin, "login-replay", params)

	require.ErrorContains(t, err, "Login flow is invalid or has expired")
	assert.EqualValues(t, 1, f.server.tokenCalls.Load())
	_, err = ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{Poll: true})
	require.ErrorContains(t, err, "Login flow is invalid or has expired")
}

func TestPluginLoginConcurrentSubmitRunsThePluginOnce(t *testing.T) {
	f := newLoginFixture(t, "login-concurrent", "redirect")
	started := f.start(t)
	params := callbackParams(visitAuthorize(t, started.Step.URL))

	const submits = 8
	var done, rejected atomic.Int32
	var group sync.WaitGroup
	for range submits {
		group.Go(func() {
			result, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-concurrent", params)
			if err != nil {
				rejected.Add(1)
				return
			}
			if assert.Equal(t, "done", result.Step.Type) {
				done.Add(1)
			}
		})
	}
	group.Wait()

	assert.EqualValues(t, 1, done.Load())
	assert.EqualValues(t, submits-1, rejected.Load())
	assert.EqualValues(t, 1, f.server.tokenCalls.Load(), "the authorization code is sent once")
}

func TestPluginLoginThrowLeavesTheStepUsable(t *testing.T) {
	f := newLoginFixture(t, "login-retry", "redirect")
	started := f.start(t)
	params := callbackParams(visitAuthorize(t, started.Step.URL))
	f.server.failNext.Store(true)

	_, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-retry", params)

	var message *common.Message
	require.ErrorAs(t, err, &message)
	assert.Equal(t, "Plugin login failed: {{error}}", message.Key)
	assert.Contains(t, message.Params["error"], "token endpoint answered 502")

	done, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-retry", params)
	require.NoError(t, err)
	assert.Equal(t, "done", done.Step.Type)
}

func TestPluginLoginInvalidStepIsRejected(t *testing.T) {
	useProviderPluginDatabase(t)
	_, err := jsplugin.DefaultRegistry.Register(`
export const meta = {apiVersion: 1, key: "login-bad-step", name: "Bad", version: "1.0.0", author: {name: "Test"}, relay: [{incoming_path: "/v1/chat/completions", upstream_path: "/c"}]};
export function login() { return {type: "redirect", url: "javascript:alert(1)"}; }`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister("login-bad-step")) })
	channel := createProviderChannel(t, "login-bad-step", "https://api.example", "")

	_, err = StartPluginLogin(context.Background(), loginAdmin, channel.Id)

	require.ErrorContains(t, err, "Plugin login failed")
}

// Nothing the plugin holds for the flow reaches the browser or the log.
func TestPluginLoginKeepsSecretsOutOfViewsAndLogs(t *testing.T) {
	var logs bytes.Buffer
	previous := gin.DefaultWriter
	common.LogWriterMu.Lock()
	gin.DefaultWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previous
		common.LogWriterMu.Unlock()
	})
	f := newLoginFixture(t, "login-secret", "redirect")

	started := f.start(t)
	var payload pluginLoginPayload
	require.NoError(t, common.UnmarshalJsonStr(flowRow(t, started.Flow).Payload, &payload))
	require.NotEmpty(t, payload.CodeVerifier)
	require.NotEmpty(t, payload.Nonce)
	require.JSONEq(t, `{"secret":"session-secret"}`, string(payload.Session))

	values := visitAuthorize(t, started.Step.URL)
	wrong := callbackParams(values)
	wrong["iss"] = "https://attacker.example"
	_, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-secret", wrong)
	require.Error(t, err)
	f.server.failNext.Store(true)
	_, err = CompletePluginLoginCallback(context.Background(), loginAdmin, "login-secret", callbackParams(values))
	require.Error(t, err)
	done, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-secret", callbackParams(values))
	require.NoError(t, err)

	views, err := json.Marshal([]any{started.Step, done.Step, PluginCredentialViews(storedChannel(t, f.channel.Id))})
	require.NoError(t, err)
	for _, secret := range []string{payload.CodeVerifier, "session-secret", "access-1", "refresh-1"} {
		assert.NotContains(t, string(views), secret)
		assert.NotContains(t, logs.String(), secret)
	}
	assert.NotContains(t, logs.String(), payload.Nonce)
	assert.NotContains(t, logs.String(), started.Flow)
	assert.NotContains(t, logs.String(), values.Get("code"))
	assert.Contains(t, logs.String(), "plugin login failed: plugin=login-secret")
	assert.Contains(t, logs.String(), "category=issuer_mismatch")
}

func TestPluginLoginOnExistingCredential(t *testing.T) {
	f := newLoginFixture(t, "login-again", "redirect")
	line := expiringLine(t, "login-again", "r", "acct-1", 1)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", f.channel.Id).Update("key", line).Error)

	// The plugin reads the stored data as ctx.credential.
	started, err := StartPluginLogin(context.Background(), loginAdmin, f.channel.Id)
	require.NoError(t, err)

	f.server.account.Store("acct-2")
	_, err = CompletePluginLoginCallback(context.Background(), loginAdmin, "login-again", callbackParams(visitAuthorize(t, started.Step.URL)))
	require.ErrorContains(t, err, "different account")
	assert.Equal(t, line, storedChannel(t, f.channel.Id).Key)
	require.Eventually(t, func() bool { return f.server.revoked.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, "refresh-1", f.server.revokedToken.Load(), "a grant of an account stored nowhere is revoked")

	f.server.account.Store("acct-1")
	started, err = StartPluginLogin(context.Background(), loginAdmin, f.channel.Id)
	require.NoError(t, err)
	done, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-again", callbackParams(visitAuthorize(t, started.Step.URL)))
	require.NoError(t, err)
	assert.Equal(t, "acct-1", done.Step.Credential.AccountID)
	stored := storedChannel(t, f.channel.Id)
	assert.NotContains(t, stored.Key, "\n")
	assert.Contains(t, stored.Key, "access-2")
}

func TestPluginLoginOneAccountPerPlugin(t *testing.T) {
	f := newLoginFixture(t, "login-account", "redirect")
	createProviderChannel(t, "login-account", f.server.URL, expiringLine(t, "login-account", "r", "acct-1", 0))

	started := f.start(t)
	_, err := CompletePluginLoginCallback(context.Background(), loginAdmin, "login-account", callbackParams(visitAuthorize(t, started.Step.URL)))
	require.ErrorContains(t, err, "already logged in on another credential")
	assert.Empty(t, storedChannel(t, f.channel.Id).Key)
	// The account stays logged in on the other channel, so its new grant is
	// kept: a vendor may end every session of the account on revoke. A revoke
	// sent afterwards arrives alone.
	RevokePluginCredentials("login-account", f.server.URL, "", f.channel.Id, nil, []string{expiringLine(t, "login-account", "later", "acct-9", 0)})
	require.Eventually(t, func() bool { return f.server.revokedToken.Load() == "later" }, 5*time.Second, 5*time.Millisecond)
	assert.EqualValues(t, 1, f.server.revoked.Load())

	f.server.account.Store("acct-2")
	started = f.start(t)
	_, err = CompletePluginLoginCallback(context.Background(), loginAdmin, "login-account", callbackParams(visitAuthorize(t, started.Step.URL)))
	require.NoError(t, err)

	// Another plugin may hold the same account.
	registerProviderPlugin(t, "login-account-b", "redirect")
	other := createProviderChannel(t, "login-account-b", f.server.URL, "")
	_, err = StorePluginLoginCredential(other.Id, "login-account-b", jsplugin.CredentialResult{Credential: json.RawMessage(`{}`), AccountID: "acct-2"})
	require.NoError(t, err)
}

func TestPluginLoginConcurrentLoginsHoldOneAccountOnce(t *testing.T) {
	f := newLoginFixture(t, "login-race", "redirect")
	channels := []*model.Channel{f.channel, createProviderChannel(t, "login-race", f.server.URL, ""), createProviderChannel(t, "login-race", f.server.URL, "")}

	var stored atomic.Int32
	var group sync.WaitGroup
	for _, channel := range channels {
		group.Go(func() {
			_, err := StorePluginLoginCredential(channel.Id, "login-race", jsplugin.CredentialResult{Credential: json.RawMessage(`{}`), AccountID: "acct-1"})
			if err == nil {
				stored.Add(1)
			}
		})
	}
	group.Wait()

	assert.EqualValues(t, 1, stored.Load())
}

// A channel holds one credential (Ren2Hub single-key-channel): a login
// replaces the key, a different account than the stored credential's is
// refused, and the channel never holds a second line.
func TestPluginLoginReplacesTheChannelCredential(t *testing.T) {
	const key = "login-replace"
	stored := expiringLine(t, key, "r", "acct-a", 0)
	needsLogin := credentialLine(t, jsplugin.Credential{Plugin: key, Data: json.RawMessage(`{}`), AccountID: "acct-a", NeedsLogin: true})
	for _, tc := range []struct {
		name    string
		key     string
		account string
		message string
	}{
		{name: "an empty channel takes its first account", account: "acct-b"},
		{name: "a static key is replaced", key: "sk-static", account: "acct-b"},
		{name: "the stored account logs in again", key: stored, account: "acct-a"},
		{name: "a credential that needs a login is replaced", key: needsLogin, account: "acct-a"},
		{name: "another account than the stored credential's", key: stored, account: "acct-b", message: "different account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useProviderPluginDatabase(t)
			registerProviderPlugin(t, key, "redirect")
			channel := createProviderChannel(t, key, "https://vendor.example", tc.key)

			view, err := StorePluginLoginCredential(channel.Id, key, jsplugin.CredentialResult{Credential: json.RawMessage(`{"fresh":true}`), AccountID: tc.account, Label: "new"})

			after := storedChannel(t, channel.Id)
			if tc.message != "" {
				require.ErrorContains(t, err, tc.message)
				assert.Equal(t, tc.key, after.Key)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, PluginCredentialView{Label: "new", AccountID: tc.account}, view)
			credential, ok := jsplugin.ParseCredential(after.Key)
			require.True(t, ok)
			assert.NotContains(t, after.Key, "\n")
			assert.JSONEq(t, `{"fresh":true}`, string(credential.Data))
			assert.False(t, credential.NeedsLogin)
			assert.Equal(t, []PluginCredentialView{{Label: "new", AccountID: tc.account}}, PluginCredentialViews(after))
		})
	}
}

func TestPluginLoginEnablesOnlyWhatTheHostDisabled(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		reason  string
		enabled bool
	}{
		"waiting for a first login":   {common.ChannelStatusAutoDisabled, model.ChannelStatusReasonPluginLoginRequired, true},
		"needs a new login":           {common.ChannelStatusAutoDisabled, model.ChannelStatusReasonPluginNeedsLogin, true},
		"disabled by an admin":        {common.ChannelStatusManuallyDisabled, "", false},
		"disabled for another reason": {common.ChannelStatusAutoDisabled, "upstream said no", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLoginFixture(t, "login-enable", "redirect")
			f.channel.Status = tc.status
			f.channel.SetOtherInfo(map[string]any{"status_reason": tc.reason})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", f.channel.Id).Updates(map[string]any{"status": tc.status, "other_info": f.channel.OtherInfo}).Error)
			require.NoError(t, model.UpdateAbilityStatus(f.channel.Id, false))

			_, err := StorePluginLoginCredential(f.channel.Id, "login-enable", jsplugin.CredentialResult{Credential: json.RawMessage(`{}`)})

			require.NoError(t, err)
			after := storedChannel(t, f.channel.Id)
			want := tc.status
			if tc.enabled {
				want = common.ChannelStatusEnabled
			}
			assert.Equal(t, want, after.Status)
			var enabledAbilities int64
			require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ? AND enabled = ?", f.channel.Id, true).Count(&enabledAbilities).Error)
			assert.Equal(t, tc.enabled, enabledAbilities > 0)
		})
	}
}

func TestPluginLoginRefusesJSONArrayKeys(t *testing.T) {
	f := newLoginFixture(t, "login-array", "redirect")
	channel := createProviderChannel(t, "login-array", f.server.URL, `["a","b"]`)

	_, err := StorePluginLoginCredential(channel.Id, "login-array", jsplugin.CredentialResult{Credential: json.RawMessage(`{}`)})

	require.ErrorContains(t, err, "key format cannot hold plugin credentials")
}

func TestPluginLoginNeedsALoginPlugin(t *testing.T) {
	useProviderPluginDatabase(t)
	_, err := jsplugin.DefaultRegistry.Register(`
export const meta = {apiVersion: 1, key: "login-none", name: "None", version: "1.0.0", author: {name: "Test"}, relay: [{incoming_path: "/v1/chat/completions", upstream_path: "/c"}]};`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister("login-none")) })
	channel := createProviderChannel(t, "login-none", "https://api.example", "sk")

	_, err = StartPluginLogin(context.Background(), loginAdmin, channel.Id)

	require.ErrorContains(t, err, "does not support login")
	assert.False(t, ChannelSupportsPluginLogin(channel))
}

func TestPluginLoginFlowFollowsTheChannel(t *testing.T) {
	f := newLoginFixture(t, "login-channel", "fields")
	started := f.start(t)

	_, err := ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id+1, started.Flow, PluginLoginInput{Fields: map[string]string{"api_token": "t"}})
	require.ErrorContains(t, err, "Login flow is invalid or has expired")

	require.NoError(t, model.DB.Delete(&model.Channel{}, f.channel.Id).Error)
	_, err = ContinuePluginLogin(context.Background(), loginAdmin, f.channel.Id, started.Flow, PluginLoginInput{Fields: map[string]string{"api_token": "t"}})
	require.ErrorContains(t, err, "Login flow is invalid or has expired")
}
