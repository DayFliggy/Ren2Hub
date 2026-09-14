package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayVeoFinalParametersAndPrecharge(t *testing.T) {
	const requestModel = "veo-3.1-generate-preview"
	const imagePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/f3sAAAAASUVORK5CYII="
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		t.Run(constant.GetChannelTypeName(channelType), func(t *testing.T) {
			var sends, authCalls atomic.Int32
			var expectedSeconds, expectedQuota, quotaBefore int
			var expectedResolution string
			url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
				index := sends.Add(1)
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					http.Error(w, "read fixture request", 400)
					return
				}
				assert.Contains(t, r.URL.Path, requestModel+":predictLongRunning")
				assert.Equal(t, int64(expectedSeconds), gjson.GetBytes(body, "parameters.durationSeconds").Int())
				assert.Equal(t, expectedResolution, gjson.GetBytes(body, "parameters.resolution").String())
				assert.EqualValues(t, 1, gjson.GetBytes(body, "parameters.sampleCount").Int())
				if gjson.GetBytes(body, "instances.0.prompt").String() == "multipart image" {
					assert.Equal(t, imagePNG, gjson.GetBytes(body, "instances.0.image.bytesBase64Encoded").String())
					assert.Equal(t, "image/png", gjson.GetBytes(body, "instances.0.image.mimeType").String())
				}
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.Equal(t, quotaBefore-expectedQuota, user.Quota, "precharge must use the parameters sent to Veo")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"name":"models/%s/operations/local-%d"}`, requestModel, index)
			})
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"veo-3.1-generate-preview":0.001}`))
			ch, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			ch.Type, ch.Models = channelType, requestModel
			if channelType == constant.ChannelTypeVertexAi {
				privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
				require.NoError(t, err)
				credentials, err := common.Marshal(map[string]string{"project_id": "veo-local", "client_email": "veo@local.invalid", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))})
				require.NoError(t, err)
				ch.Key = string(credentials)
				client := service.GetHttpClient()
				original := client.Transport
				t.Cleanup(func() { client.Transport = original })
				client.Transport = gatewayPromptTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.Host == "www.googleapis.com" && r.URL.Path == "/oauth2/v4/token" {
						authCalls.Add(1)
						if err := r.ParseForm(); err != nil {
							return nil, err
						}
						_, err := jwt.Parse(r.Form.Get("assertion"), func(*jwt.Token) (any, error) { return &privateKey.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}))
						if err != nil {
							return nil, fmt.Errorf("invalid fixture JWT: %w", err)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"veo-local-token","expires_in":3600}`)), Request: r}, nil
					}
					if r.URL.Host != strings.TrimPrefix(*ch.BaseURL, "http://") {
						return nil, fmt.Errorf("unexpected nonlocal Veo test request")
					}
					assert.Equal(t, "Bearer veo-local-token", r.Header.Get("Authorization"))
					return original.RoundTrip(r)
				})
			}
			require.NoError(t, ch.Update())
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			for _, tc := range []struct {
				name, fields, resolution string
				seconds                  int
				multiplier               float64
			}{
				{"seconds", `,"seconds":"4"`, "720p", 4, 1},
				{"default", ``, "720p", 8, 1},
				{"metadata", `,"seconds":"4","metadata":{"durationSeconds":8,"resolution":"4K","seed":0,"generateAudio":false}`, "4k", 8, 1.5},
			} {
				t.Run(tc.name, func(t *testing.T) {
					expectedSeconds, expectedResolution = tc.seconds, tc.resolution
					expectedQuota = common.QuotaFromFloat(0.001 * common.QuotaPerUnit * float64(tc.seconds) * tc.multiplier)
					var before model.User
					require.NoError(t, model.DB.First(&before, 1).Error)
					quotaBefore = before.Quota
					code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"`+requestModel+`","prompt":"isolated video"`+tc.fields+`}`)
					require.Equal(t, 200, code, body)
					var task model.Task
					require.NoError(t, model.DB.Order("id desc").First(&task).Error)
					assert.Equal(t, expectedQuota, task.Quota)
					var after model.User
					require.NoError(t, model.DB.First(&after, 1).Error)
					assert.Equal(t, expectedQuota, after.UsedQuota-before.UsedQuota)
				})
			}
			t.Run("multipart image", func(t *testing.T) {
				expectedSeconds, expectedResolution = 4, "720p"
				expectedQuota = common.QuotaFromFloat(0.001 * common.QuotaPerUnit * 4)
				var before model.User
				require.NoError(t, model.DB.First(&before, 1).Error)
				quotaBefore = before.Quota
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				for field, value := range map[string]string{"model": requestModel, "prompt": "multipart image", "seconds": "4"} {
					require.NoError(t, writer.WriteField(field, value))
				}
				part, err := writer.CreateFormFile("input_reference", "input.png")
				require.NoError(t, err)
				data, err := base64.StdEncoding.DecodeString(imagePNG)
				require.NoError(t, err)
				_, err = part.Write(data)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				req, err := http.NewRequest(http.MethodPost, url+"/v1/videos", &body)
				require.NoError(t, err)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				req.Header.Set("Authorization", "Bearer "+key)
				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				defer resp.Body.Close()
				response, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, 200, resp.StatusCode, string(response))
				var task model.Task
				require.NoError(t, model.DB.Order("id desc").First(&task).Error)
				assert.Equal(t, expectedQuota, task.Quota)
			})
			for _, fields := range []string{`,"metadata":{"durationSeconds":3601}`, `,"metadata":{"sampleCount":2}`, `,"seconds":"bad"`, `,"duration":"bad"`, `,"metadata":"bad"`, `,"images":["data:image/png;base64,bad"]`, `,"input_reference":"https://image.invalid/input.png"`,
				`,"metadata":{"DurationSeconds":-1}`, `,"metadata":{"SampleCount":2}`, `,"metadata":{"Resolution":"invalid"}`,
				`,"metadata":{"DurationSeconds":8,"durationSeconds":4}`, `,"images":["` + imagePNG + `"],"input_reference":"invalid"`, `,"images":["` + imagePNG + `"],"image":"invalid"`,
			} {
				t.Run("reject/"+fields, func(t *testing.T) {
					var before, after model.User
					require.NoError(t, model.DB.First(&before, 1).Error)
					var recoveriesBefore, recoveriesAfter int64
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveriesBefore).Error)
					beforeSends, beforeAuth := sends.Load(), authCalls.Load()
					code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"`+requestModel+`","prompt":"isolated video"`+fields+`}`)
					assert.Equal(t, 400, code, body)
					assert.Equal(t, beforeSends, sends.Load())
					assert.Equal(t, beforeAuth, authCalls.Load(), "invalid requests cannot initiate token exchange")
					require.NoError(t, model.DB.First(&after, 1).Error)
					assert.Equal(t, before.Quota, after.Quota)
					assert.Equal(t, before.UsedQuota, after.UsedQuota)
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveriesAfter).Error)
					assert.Equal(t, recoveriesBefore, recoveriesAfter)
				})
			}
			assert.EqualValues(t, 4, sends.Load())
		})
	}
}
