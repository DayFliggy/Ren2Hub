package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const mjUserID = 71

// mjUpstream records what Midjourney continuations send to the proxy.
type mjUpstream struct {
	mu      sync.Mutex
	sends   int
	paths   []string
	secrets []string
}

func (u *mjUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sends++
	u.paths = append(u.paths, r.URL.Path)
	u.secrets = append(u.secrets, r.Header.Get("mj-api-secret"))
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"code":1,"description":"submitted","result":"mj-next-%d"}`, u.sends)
}

type mjRoute struct {
	engine   *gin.Engine
	upstream *mjUpstream
	baseURL  string
}

func newMjRoute(t *testing.T) *mjRoute {
	t.Helper()
	require.NoError(t, appI18n.Init())
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}, &model.Log{}, &model.TopUpDiscountUsageHourly{}, &model.TopUpDiscountConfigVersion{}))
	service.InitHttpClient()
	previousPrices := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.1,"mj_upscale":0.1,"mj_modal":0.1,"mj_video":0.1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices)) })

	upstream := &mjUpstream{}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	require.NoError(t, model.DB.Create(&model.User{Id: mjUserID, Username: "mj-user", Status: common.UserStatusEnabled, Quota: 1_000_000, AffCode: "mj-user"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: mjUserID + 1, Username: "mj-other", Status: common.UserStatusEnabled, Quota: 1_000_000, AffCode: "mj-other"}).Error)
	engine := gin.New()
	SetRelayRouter(engine)
	return &mjRoute{engine: engine, upstream: upstream, baseURL: server.URL}
}

func (r *mjRoute) token(t *testing.T, key string, modelLimits string) model.Token {
	t.Helper()
	token := model.Token{
		UserId: mjUserID, Key: key, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true,
		ModelLimitsEnabled: modelLimits != "", ModelLimits: modelLimits,
	}
	require.NoError(t, model.DB.Create(&token).Error)
	return token
}

// manual makes the Key's route a manual list of exactly channelIDs.
func (r *mjRoute) manual(t *testing.T, token model.Token, channelIDs ...int) {
	t.Helper()
	entries := make([]model.TokenRouteEntry, 0, len(channelIDs))
	allowed := map[int]bool{}
	for position, id := range channelIDs {
		entries = append(entries, model.TokenRouteEntry{ChannelID: id, Position: position, Enabled: true})
		allowed[id] = true
	}
	_, err := model.SaveTokenRouting(context.Background(), mjUserID, token.Id, model.TokenRouting{
		TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual},
		Entries:           entries,
	}, allowed)
	require.NoError(t, err)
}

// channel creates a Midjourney channel with room for one request, so a held
// permit fills it. The limit is republished because channel ids repeat across
// this package's test databases.
func (r *mjRoute) channel(t *testing.T, name, key string) model.Channel {
	t.Helper()
	channel := model.Channel{
		Type: constant.ChannelTypeMidjourney, Name: name, Key: key, Models: "mj_imagine,mj_upscale,mj_modal,mj_video",
		Status: common.ChannelStatusEnabled, BaseURL: &r.baseURL, MaxConcurrency: 1,
	}
	if strings.Contains(key, "\n") {
		channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeyMode: constant.MultiKeyModePolling}
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), channel.Id))
	return channel
}

// fill holds the channel's only slot until the test ends.
func (r *mjRoute) fill(t *testing.T, channel model.Channel) {
	t.Helper()
	permit, err := service.AcquireChannelConcurrency(context.Background(), &channel)
	require.NoError(t, err)
	t.Cleanup(permit.Release)
}

func (r *mjRoute) origin(t *testing.T, userID, channelID int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Midjourney{
		UserId: userID, MjId: "mj-origin", ChannelId: channelID, Action: constant.MjActionImagine,
		Prompt: "a lighthouse", Status: "SUCCESS", Progress: "100%",
	}).Error)
}

func (r *mjRoute) submit(t *testing.T, token model.Token, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	method := http.MethodPost
	if body == "" {
		method = http.MethodGet
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	recorder := httptest.NewRecorder()
	r.engine.ServeHTTP(recorder, request)
	return recorder
}

func mjUserQuota(t *testing.T) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, mjUserID).Error)
	return user.Quota
}

func mjPollingIndex(t *testing.T, channelID int) int {
	t.Helper()
	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channelID).Error)
	return stored.ChannelInfo.MultiKeyPollingIndex
}

// Every request below continues mj-origin with all of its fields present.
// Like NewAPI, the simple-change path runs in change mode, so it carries the
// change fields; content alone names no model there.
var mjContinuations = []struct{ name, path, body string }{
	{"change", "/mj/submit/change", `{"taskId":"mj-origin","action":"UPSCALE","index":1}`},
	{"simple-change", "/mj/submit/simple-change", `{"taskId":"mj-origin","action":"UPSCALE","index":1,"content":"mj-origin U1"}`},
	{"modal", "/mj/submit/modal", `{"taskId":"mj-origin","prompt":"add a moon"}`},
	{"video", "/mj/submit/video", `{"taskId":"mj-origin","prompt":"pan left"}`},
	{"action", "/mj/submit/action", `{"taskId":"mj-origin","customId":"MJ::JOB::upsample::1::f0f0f0f0"}`},
}

// A continuation is admitted on its origin channel A whatever the Key's
// manual list now says. Before the fix ordinary selection ran first: an empty
// list was 503, a list of only B never reached A, and a full B was 429.
func TestMidjourneyContinuationAdmittedOnOriginChannel(t *testing.T) {
	for _, condition := range []string{"empty manual list", "list names only B", "B is full"} {
		t.Run(condition, func(t *testing.T) {
			route := newMjRoute(t)
			a := route.channel(t, "origin-a", "key-a")
			b := route.channel(t, "other-b", "key-b")
			route.origin(t, mjUserID, a.Id)
			token := route.token(t, "mjorigin", "")
			switch condition {
			case "empty manual list":
				route.manual(t, token)
			case "list names only B":
				route.manual(t, token, b.Id)
			case "B is full":
				route.manual(t, token, b.Id)
				route.fill(t, b)
			}
			before := mjUserQuota(t)

			recorder := route.submit(t, token, "/mj/submit/change", mjContinuations[0].body)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Equal(t, []string{"key-a"}, route.upstream.secrets, "sent once, with the origin channel's key")
			assert.Equal(t, 50_000, before-mjUserQuota(t), "0.1 x 500000 on the origin channel")
			var next model.Midjourney
			require.NoError(t, model.DB.Where("mj_id = ?", "mj-next-1").First(&next).Error)
			assert.Equal(t, a.Id, next.ChannelId)
		})
	}
}

func TestMidjourneyContinuationPriceLimitRejectsBeforeChargeAndSend(t *testing.T) {
	continuations := append([]struct{ name, path, body string }{}, mjContinuations...)
	continuations = append(continuations, struct{ name, path, body string }{
		"image-seed", "/mj/task/mj-origin/image-seed", "",
	})
	for _, mode := range []string{model.TokenRouteAuto, model.TokenRouteManual} {
		for _, tc := range continuations {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				route := newMjRoute(t)
				a := route.channel(t, "origin-a", "key-a1\nkey-a2")
				b := route.channel(t, "cheaper-b", "key-b")
				route.origin(t, mjUserID, a.Id)
				token := route.token(t, "mjpricelimit", "")
				route.manual(t, token, b.Id)
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", a.Id).Update("channel_ratio", "2").Error)
				require.NoError(t, model.DB.Model(&model.TokenRouteProfile{}).Where("token_id = ?", token.Id).Updates(map[string]any{
					"mode": mode, "max_channel_ratio": "1",
				}).Error)
				before := mjUserQuota(t)
				var quotaWrites atomic.Int64
				require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register("test:reject-before-quota-write", func(tx *gorm.DB) {
					if (tx.Statement.Table == "users" || tx.Statement.Table == "tokens") && strings.Contains(tx.Statement.SQL.String(), "quota") {
						quotaWrites.Add(1)
					}
				}))

				recorder := route.submit(t, token, tc.path, tc.body)

				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				if tc.name == "image-seed" {
					assert.Contains(t, recorder.Body.String(), `"code":4`)
					assert.Contains(t, recorder.Body.String(), "maximum channel multiplier")
				} else {
					assert.Contains(t, recorder.Body.String(), `"code":"channel_ratio_exceeded"`)
				}
				assert.Zero(t, route.upstream.sends, "neither the origin nor the cheaper channel receives a request")
				assert.Zero(t, quotaWrites.Load(), "a debit followed by a refund would also fail this assertion")
				assert.Equal(t, before, mjUserQuota(t))
				assert.Zero(t, mjPollingIndex(t, a.Id), "rejection precedes key rotation")
				assert.Zero(t, service.ChannelInFlight(a.Id))
				assert.Zero(t, service.ChannelInFlight(b.Id))
				for _, user := range service.RoutingSnapshot().Users {
					if user.ID == mjUserID {
						assert.Zero(t, user.InFlight, "user admission is released on rejection")
					}
				}
			})
		}
	}
}

// An unresolved origin must not bypass the existing model and route checks.
// The desired not-found-first and storage-500 behavior is an upstream probe.
func TestMidjourneyUnresolvedOriginKeepsExistingErrorOrder(t *testing.T) {
	for _, origin := range []string{"missing", "foreign", "storage failure"} {
		for _, tc := range []struct {
			name   string
			status int
		}{
			{"empty manual list", http.StatusServiceUnavailable},
			{"all candidates full", http.StatusTooManyRequests},
			{"model not allowed", http.StatusForbidden},
			{"available channel", http.StatusBadRequest},
		} {
			t.Run(origin+"/"+tc.name, func(t *testing.T) {
				route := newMjRoute(t)
				a := route.channel(t, "origin-a", "key-a1\nkey-a2")
				b := route.channel(t, "other-b", "key-b")
				if origin == "foreign" {
					route.origin(t, mjUserID+1, a.Id)
				} else if origin == "storage failure" {
					route.origin(t, mjUserID, a.Id)
					require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:mj-unresolved-storage", func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Midjourney" {
							tx.AddError(errors.New("private database failure fixture"))
						}
					}))
				}
				limits := ""
				if tc.name == "model not allowed" {
					limits = "gpt-4o"
				}
				token := route.token(t, "mjunresolved", limits)
				switch tc.name {
				case "empty manual list":
					route.manual(t, token)
				case "all candidates full":
					route.fill(t, a)
					route.fill(t, b)
				}
				before := mjUserQuota(t)

				recorder := route.submit(t, token, "/mj/submit/change", mjContinuations[0].body)

				assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
				if tc.name == "available channel" {
					assert.JSONEq(t, `{"code":4,"description":"task_not_found ","type":"upstream_error"}`, recorder.Body.String())
				}
				assert.NotContains(t, recorder.Body.String(), "get_origin_task_failed")
				assert.NotContains(t, recorder.Body.String(), "private database")
				assert.Zero(t, route.upstream.sends)
				assert.Equal(t, before, mjUserQuota(t))
			})
		}
	}
}

// A found task is still checked against the Key's model limit.
func TestMidjourneyContinuationChecksKeyModelLimit(t *testing.T) {
	route := newMjRoute(t)
	a := route.channel(t, "origin-a", "key-a")
	route.origin(t, mjUserID, a.Id)
	before := mjUserQuota(t)

	denied := route.submit(t, route.token(t, "mjdenied", "mj_imagine"), "/mj/submit/change", mjContinuations[0].body)

	assert.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	assert.Zero(t, route.upstream.sends)
	assert.Equal(t, before, mjUserQuota(t))
	allowed := route.submit(t, route.token(t, "mjallowed", "mj_upscale"), "/mj/submit/change", mjContinuations[0].body)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
}

// The origin channel's own state decides, as for every origin task (A1
// error table); the request never moves to another channel.
func TestMidjourneyContinuationOriginChannelFailures(t *testing.T) {
	for _, tc := range []struct {
		condition string
		status    int
		code      string
	}{
		{"disabled", http.StatusBadRequest, "origin_task_channel_disabled"},
		{"deleted", http.StatusBadRequest, "origin_task_channel_disabled"},
		{"full", http.StatusTooManyRequests, "channel_concurrency_exceeded"},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			route := newMjRoute(t)
			a := route.channel(t, "origin-a", "key-a")
			route.channel(t, "other-b", "key-b")
			route.origin(t, mjUserID, a.Id)
			switch tc.condition {
			case "disabled":
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", a.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
			case "deleted":
				require.NoError(t, model.DB.Delete(&model.Channel{}, a.Id).Error)
			case "full":
				route.fill(t, a)
			}
			token := route.token(t, "mjfailure", "")
			before := mjUserQuota(t)

			recorder := route.submit(t, token, "/mj/submit/change", mjContinuations[0].body)

			assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), tc.code)
			assert.Zero(t, route.upstream.sends, "nothing goes to B")
			assert.Equal(t, before, mjUserQuota(t))
		})
	}
}

// Distribute admits and sets up the origin channel once; the handler reuses
// it. Before the fix each continuation rotated a multi-key channel twice and
// sent the second key.
func TestMidjourneyContinuationRotatesOriginKeyOnce(t *testing.T) {
	for _, tc := range mjContinuations {
		t.Run(tc.name, func(t *testing.T) {
			route := newMjRoute(t)
			a := route.channel(t, "origin-a", "key-a1\nkey-a2\nkey-a3")
			route.origin(t, mjUserID, a.Id)

			recorder := route.submit(t, route.token(t, "mjrotate", ""), tc.path, tc.body)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Equal(t, []string{tc.path}, route.upstream.paths)
			assert.Equal(t, []string{"key-a1"}, route.upstream.secrets)
			assert.Equal(t, 1, mjPollingIndex(t, a.Id), "the cursor advances once")
		})
	}
}

// image-seed reads the origin channel under the origin-task rule too. This is
// Kimi's TestProbeMidjourneyOriginChannelAfterManualRevocation asserting the
// fix: before it, a manual list without A rejected the origin channel.
func TestMidjourneyImageSeedUsesOriginChannelAfterManualListChanges(t *testing.T) {
	route := newMjRoute(t)
	a := route.channel(t, "origin-a", "key-a")
	b := route.channel(t, "other-b", "key-b")
	route.origin(t, mjUserID, a.Id)
	token := route.token(t, "mjseed", "")
	route.manual(t, token, b.Id)

	recorder := route.submit(t, token, "/mj/task/mj-origin/image-seed", "")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, []string{"/mj/task/mj-origin/image-seed"}, route.upstream.paths)
	assert.Equal(t, []string{"key-a"}, route.upstream.secrets)
}
