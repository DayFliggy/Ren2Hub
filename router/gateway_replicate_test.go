package router

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayReplicateUploadAdmissionAndAcceptance(t *testing.T) {
	for _, scenario := range []struct {
		name, override                      string
		quota, status, uploads, predictions int
	}{
		{"success", `{"operations":[{"path":"input.num_outputs","mode":"set","value":3}]}`, 10000000, 200, 1, 1},
		{"invalid_count", `{"operations":[{"path":"input.num_outputs","mode":"set","value":129}]}`, 10000000, 400, 0, 0},
		{"insufficient_quota", `{"operations":[{"path":"input.num_outputs","mode":"set","value":3}]}`, 1000, 403, 0, 0},
		{"invalid_upload_response", `{"operations":[{"path":"input.num_outputs","mode":"set","value":3}]}`, 10000000, 502, 1, 0},
		{"unknown_generation_result", `{"operations":[{"path":"input.num_outputs","mode":"set","value":3}]}`, 10000000, 502, 1, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var uploads, predictions atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "tenant-fixture", r.Header.Get("X-Tenant"), "upload uses final validated transport headers")
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.Equal(t, scenario.quota-1500, user.Quota, "reserve the final output count before any provider resource creation")
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/files" {
					uploads.Add(1)
					if assert.NoError(t, r.ParseMultipartForm(1024*1024)) {
						file, _, err := r.FormFile("content")
						if assert.NoError(t, err) {
							data, err := io.ReadAll(file)
							assert.NoError(t, err)
							assert.Equal(t, "image fixture", string(data))
							_ = file.Close()
						}
					}
					if scenario.name == "invalid_upload_response" {
						_, _ = io.WriteString(w, `{}`)
						return
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"urls":{"get":"https://example.invalid/upload.png"}}`)
					return
				}
				predictions.Add(1)
				assert.True(t, strings.HasSuffix(r.URL.Path, "/predictions"))
				var payload struct {
					Input map[string]any `json:"input"`
				}
				if assert.NoError(t, common.DecodeJson(r.Body, &payload)) {
					assert.Equal(t, float64(3), payload.Input["num_outputs"])
					assert.Equal(t, "https://example.invalid/upload.png", payload.Input["image_prompt"])
				}
				if scenario.name == "unknown_generation_result" {
					w.WriteHeader(http.StatusBadGateway)
					_, _ = io.WriteString(w, `{"error":{"message":"result unknown","type":"server_error"}}`)
					return
				}
				_, _ = io.WriteString(w, `{"status":"succeeded","output":["https://example.invalid/result.png"]}`)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Updates(map[string]any{
				"type": constant.ChannelTypeReplicate, "param_override": scenario.override, "header_override": `{"X-Tenant":"tenant-fixture"}`,
			}).Error)
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", scenario.quota).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "remain_quota": 100000}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "gpt-4o-mini"))
			require.NoError(t, writer.WriteField("prompt", "edit this image"))
			require.NoError(t, writer.WriteField("n", "1"))
			file, err := writer.CreateFormFile("image", "input.png")
			require.NoError(t, err)
			_, err = io.WriteString(file, "image fixture")
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			request, err := http.NewRequest(http.MethodPost, url+"/v1/images/edits", &body)
			require.NoError(t, err)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			request.Header.Set("Authorization", "Bearer "+key)
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			output, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, scenario.status, response.StatusCode, string(output))
			awaitGatewayPermitsReleased(t)
			assert.EqualValues(t, scenario.uploads, uploads.Load())
			assert.EqualValues(t, scenario.predictions, predictions.Load(), "accepted upload or unknown prediction must not replay")
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			charge := 0
			if scenario.name == "success" {
				charge = 500
			}
			if scenario.name == "unknown_generation_result" {
				charge = 1500
				var recovery model.BillingRecovery
				require.NoError(t, model.DB.First(&recovery).Error)
				assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status)
				assert.Equal(t, model.BillingRecoveryStateUsagePending, recovery.ConsumptionState)
				assert.Zero(t, user.UsedQuota, "unknown generation holds the reservation without final settlement")
			}
			if scenario.name == "invalid_upload_response" {
				var recovery model.BillingRecovery
				require.NoError(t, model.DB.First(&recovery).Error)
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
			}
			assert.Equal(t, scenario.quota-charge, user.Quota)
			assert.Equal(t, 100000-charge, token.RemainQuota)
		})
	}
}
