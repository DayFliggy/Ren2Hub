package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSub2APISyncPaginationBoundsAndMissingConfirmation(t *testing.T) {
	for _, tc := range []struct {
		mode, code string
		pages      int
	}{
		{"huge", "upstream_pagination_limit_exceeded", 1}, {"many_pages", "upstream_pagination_limit_exceeded", 1}, {"repeat", "upstream_pagination_incomplete", 2}, {"deleted", "upstream_pagination_incomplete", 2}, {"empty", "upstream_pagination_incomplete", 2}, {"changed_total", "upstream_pagination_incomplete", 2}, {"deadline", "upstream_sync_deadline", 1}, {"html404", "upstream_http_404", 1}, {"empty404", "upstream_http_404", 1}, {"proxy_json404", "upstream_http_404", 1}, {"json404", "remote_missing", 1},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			db := setupUpstreamRatioTest(t)
			require.NoError(t, db.AutoMigrate(&model.Ability{}))
			t.Setenv("CRYPTO_SECRET", "fixture")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := "{}"
				switch r.URL.Path {
				case "/api/v1/auth/me":
					data = `{"username":"fixture","balance":2}`
				case "/api/v1/groups/available", "/api/v1/subscriptions/progress":
					data = "[]"
				case "/api/v1/groups/rates":
				case "/api/v1/keys":
					page, _ := strconv.Atoi(r.URL.Query().Get("page"))
					calls.Add(1)
					size, total := 1, 3
					items := fmt.Sprintf(`{"id":%d,"status":"active","key":"test-key"}`, page)
					switch tc.mode {
					case "huge":
						if page == 1 {
							total = 2147483647
						}
					case "many_pages":
						if page == 1 {
							total = 51
						}
					case "repeat":
						items = `{"id":1,"status":"active","key":"test-key"}`
					case "deleted":
						size, total = 2, 4
						if page == 1 {
							items += `,{"id":2,"status":"active","key":"test-key"}`
						} else {
							items = `{"id":4,"status":"active","key":"test-key"}`
						}
					case "empty":
						if page == 2 {
							items = ""
						}
					case "changed_total":
						if page == 2 {
							total = 2
						}
					case "deadline":
						<-r.Context().Done()
						return
					default:
						total = 0
						items = ""
					}
					data = fmt.Sprintf(`{"total":%d,"page_size":%d,"page":%d,"items":[%s]}`, total, size, page, items)
				case "/api/v1/keys/17":
					w.WriteHeader(404)
					switch tc.mode {
					case "html404":
						fmt.Fprint(w, "<html>nginx 404</html>")
					case "proxy_json404":
						fmt.Fprint(w, `{"code":404,"message":"route not found"}`)
					case "json404":
						fmt.Fprint(w, `{"code":404,"reason":"API_KEY_NOT_FOUND","message":"key not found"}`)
					}
					return
				default:
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"code":0,"data":%s}`, data)
			}))
			defer server.Close()
			a := model.UpstreamAccount{Platform: "sub2api", Name: "test", Status: "active", BaseURL: server.URL, CredentialUID: "test-account"}
			now := time.Now()
			c := UpstreamCredential{AccessToken: "test-access", RefreshToken: "test-refresh", AccessExpiresAt: now.Add(24 * time.Hour).Unix(), RefreshExpiresAt: now.Add(30 * 24 * time.Hour).Unix()}
			var err error
			a.CredentialCiphertext, err = sealUpstreamCredential(&a, c)
			require.NoError(t, err)
			require.NoError(t, model.CreateUpstreamAccount(&a))
			ch := model.Channel{Type: 59, CreatedTime: 99, Status: common.ChannelStatusEnabled, Name: "key", Key: "bound-key", BaseURL: &server.URL, Models: "model"}
			require.NoError(t, ch.Insert())
			b := model.UpstreamChannelBinding{AccountID: a.ID, ChannelID: ch.Id, RemoteKeyID: 17, ChannelFP: upstreamRatioFingerprint(&ch), ChannelCreatedTime: 99, RemoteStatus: "active"}
			require.NoError(t, model.BindUpstreamChannel(&b, func(*model.Channel) error { return nil }))
			limit := 3 * time.Second
			if tc.mode == "deadline" {
				limit = 500 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), limit)
			defer cancel()
			err = SyncUpstreamAccount(ctx, a.ID)
			require.EqualValues(t, tc.pages, calls.Load())
			rows, e := model.ListUpstreamBindings(a.ID)
			require.NoError(t, e)
			require.Len(t, rows, 1)
			actual, e := model.GetChannelById(ch.Id, true)
			require.NoError(t, e)
			if tc.code == "remote_missing" {
				require.NoError(t, err)
				require.Equal(t, "missing", rows[0].RemoteStatus)
				require.Equal(t, common.ChannelStatusAutoDisabled, actual.Status)
				return
			}
			require.EqualError(t, err, tc.code)
			require.Equal(t, b, rows[0])
			require.Equal(t, common.ChannelStatusEnabled, actual.Status)
		})
	}
}
