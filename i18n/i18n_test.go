package i18n

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A saved language selects the backend language. Traditional Chinese codes,
// including the underscore forms, select zh-TW; languages without a backend
// locale fall back to English.
func TestLanguageFromUserSetting(t *testing.T) {
	require.NoError(t, Init())
	for _, tc := range []struct {
		saved string
		want  string
	}{
		{"zh-CN", LangZhCN},
		{"zhCN", LangZhCN},
		{"zhTW", LangZhTW},
		{"zh-HK", LangZhTW},
		{"zh_TW", LangZhTW},
		{"zh_HK", LangZhTW},
		{"zh-Hant-TW", LangZhTW},
		{"zh", LangZhCN},
		{"en", LangEn},
		{"fr", LangEn},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/v1/chat/completions", nil)
		c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{Language: tc.saved})
		assert.Equal(t, tc.want, StatedLang(c), tc.saved)
	}
}

// TokenAuth puts the user's settings into the request. A user who saved no
// language must not be loaded again, which would cost a cache or database read
// on every relay request; a request without loaded settings still loads them.
func TestStatedLangLoadsTheUserOnlyWithoutLoadedSettings(t *testing.T) {
	previous := userLangLoaderFunc
	t.Cleanup(func() { userLangLoaderFunc = previous })
	loads := 0
	SetUserLangLoader(func(int) string {
		loads++
		return "zhTW"
	})

	relay, _ := gin.CreateTestContext(httptest.NewRecorder())
	relay.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	relay.Set("id", 7)
	relay.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{})
	assert.Equal(t, "", StatedLang(relay))
	assert.Zero(t, loads)

	dashboard, _ := gin.CreateTestContext(httptest.NewRecorder())
	dashboard.Request = httptest.NewRequest("GET", "/api/user/self", nil)
	dashboard.Set("id", 7)
	assert.Equal(t, LangZhTW, StatedLang(dashboard))
	assert.Equal(t, 1, loads)
}

// A saved language or an Accept-Language header selects the language of a
// backend message. A reader who states neither gets DEFAULT_LANGUAGE, which is
// English when unset.
func TestBackendMessageLanguage(t *testing.T) {
	require.NoError(t, Init())
	t.Cleanup(func() { common.DefaultLanguage = "" })
	model := map[string]any{"Model": "gpt-x"}
	const english = "This token has no access to model gpt-x"
	const chinese = "该令牌无权访问模型 gpt-x"
	for _, tc := range []struct {
		name            string
		defaultLanguage string
		saved           string
		acceptLanguage  string
		want            string
	}{
		{"nothing stated", "", "", "", english},
		{"no preference header", "", "", "*", english},
		{"nothing stated with a Chinese default", "zh-CN", "", "", chinese},
		{"nothing stated with an English default", "en", "", "", english},
		{"header wins over the default", "zh-CN", "", "en-US,en;q=0.9", english},
		{"header in a language without a locale", "zh-CN", "", "fr-FR", english},
		{"Traditional Chinese header", "", "", "zh-HK,zh;q=0.9", "該令牌無權存取模型 gpt-x"},
		{"saved language wins over the header", "", "zhTW", "en-US", "該令牌無權存取模型 gpt-x"},
		{"saved language", "zh-CN", "en", "", english},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.DefaultLanguage = tc.defaultLanguage
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			if tc.acceptLanguage != "" {
				c.Request.Header.Set("Accept-Language", tc.acceptLanguage)
			}
			if tc.saved != "" {
				c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{Language: tc.saved})
			}
			assert.Equal(t, tc.want, T(c, MsgDistributorTokenModelForbidden, model))
		})
	}
}

// The account language setting accepts the backend languages and their
// aliases, stored in their canonical form; anything else is rejected.
func TestSupportedLang(t *testing.T) {
	for _, tc := range []struct {
		code string
		want string
		ok   bool
	}{
		{"en", LangEn, true},
		{"en-US", LangEn, true},
		{"zh-CN", LangZhCN, true},
		{"zhCN", LangZhCN, true},
		{"zh_CN", LangZhCN, true},
		{"zh", LangZhCN, true},
		{"zh-TW", LangZhTW, true},
		{"zhTW", LangZhTW, true},
		{"zh_HK", LangZhTW, true},
		{"zh-Hant", LangZhTW, true},
		{"", "", false},
		{"fr", "", false},
		{"english", "", false},
		{"zhx", "", false},
	} {
		got, ok := SupportedLang(tc.code)
		assert.Equal(t, tc.ok, ok, tc.code)
		assert.Equal(t, tc.want, got, tc.code)
	}
}

// Web console responses carry the English source text as message_key plus its
// params, and the web console translates them. The backend sends the same body
// whatever language the request asks for.
func TestWebConsoleMessageResponse(t *testing.T) {
	require.NoError(t, Init())
	for _, tc := range []struct {
		name    string
		respond func(c *gin.Context)
		status  int
		want    string
	}{
		{
			name: "params",
			respond: func(c *gin.Context) {
				common.ApiError(c, common.NewMessage("Channel {{name}} (#{{id}}) does not exist", map[string]any{"name": "{{id}}", "id": 7}))
			},
			status: http.StatusOK,
			want:   `{"success":false,"message":"Channel {{id}} (#7) does not exist","message_key":"Channel {{name}} (#{{id}}) does not exist","message_params":{"id":7,"name":"{{id}}"}}`,
		},
		{
			name:    "no params",
			respond: func(c *gin.Context) { common.ApiErrorT(c, "Invalid parameters") },
			status:  http.StatusOK,
			want:    `{"success":false,"message":"Invalid parameters","message_key":"Invalid parameters"}`,
		},
		{
			name:    "plain error",
			respond: func(c *gin.Context) { common.ApiError(c, errors.New("record not found")) },
			status:  http.StatusOK,
			want:    `{"success":false,"message":"record not found"}`,
		},
		{
			name: "wrapped message",
			respond: func(c *gin.Context) {
				common.ApiError(c, fmt.Errorf("lookup: %w", common.NewMessage("Invalid parameters")))
			},
			status: http.StatusOK,
			want:   `{"success":false,"message":"lookup: Invalid parameters","message_key":"Invalid parameters"}`,
		},
		{
			name: "another status with a stable code",
			respond: func(c *gin.Context) {
				common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"), gin.H{"code": "VALIDATION_ERROR"})
			},
			status: http.StatusBadRequest,
			want:   `{"success":false,"message":"Invalid parameters","message_key":"Invalid parameters","code":"VALIDATION_ERROR"}`,
		},
		{
			name: "business error code",
			respond: func(c *gin.Context) {
				common.ApiErrorWithCode(c, errors.New("ticket not found"), "NOT_FOUND")
			},
			status: http.StatusOK,
			want:   `{"success":false,"message":"ticket not found","code":"NOT_FOUND"}`,
		},
		{
			name: "success",
			respond: func(c *gin.Context) {
				common.ApiSuccessT(c, "Saved {{count}} items", gin.H{"id": 1}, map[string]any{"count": 2})
			},
			status: http.StatusOK,
			want:   `{"success":true,"message":"Saved 2 items","data":{"id":1},"message_key":"Saved {{count}} items","message_params":{"count":2}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", "/api/channel/7", nil)
			c.Request.Header.Set("Accept-Language", "zh-CN")
			tc.respond(c)
			assert.Equal(t, tc.status, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

// A key missing from a locale, or a template that does not parse, reaches
// clients as the raw key.
func TestLocalesRenderEveryKey(t *testing.T) {
	require.NoError(t, Init())
	files, err := filepath.Glob("*keys.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	var declared []string
	for _, file := range files {
		keysFile, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		ast.Inspect(keysFile, func(node ast.Node) bool {
			if spec, ok := node.(*ast.ValueSpec); ok {
				for _, value := range spec.Values {
					if literal, ok := value.(*ast.BasicLit); ok {
						key, err := strconv.Unquote(literal.Value)
						require.NoError(t, err)
						declared = append(declared, key)
					}
				}
			}
			return true
		})
	}
	slices.Sort(declared)

	for _, lang := range SupportedLanguages() {
		data, err := localeFS.ReadFile("locales/" + lang + ".yaml")
		require.NoError(t, err)
		var messages map[string]string
		require.NoError(t, yaml.Unmarshal(data, &messages))
		var keys []string
		for key := range messages {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		assert.Equal(t, declared, keys, lang)
		for _, key := range keys {
			text := Translate(lang, key, map[string]any{})
			assert.NotEqual(t, key, text, "%s %s", lang, key)
			assert.False(t, strings.Contains(text, "{{"), "%s %s: %s", lang, key, text)
		}
	}
}
