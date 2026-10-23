package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManageUserQuotaFirstAndReplayReturnCommittedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		mode         string
		value, after int
	}{{"add", 150, 1150}, {"subtract", 150, 850}, {"override", 300, 300}} {
		t.Run(tc.mode, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, common.RoleRootUser)
			user := model.User{Username: "snapshot-user", Role: common.RoleCommonUser, Quota: 1000, AffCode: "SNAPSHOT-USER"}
			require.NoError(t, db.Create(&user).Error)
			body := fmt.Sprintf(`{"id":%d,"action":"add_quota","operation_id":"quota-snapshot-operation","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value)
			for attempt := range 2 {
				response := performManageUserRequest(t, body)
				require.Equal(t, http.StatusOK, response.Code)
				var result struct {
					Success bool `json:"success"`
					Data    *struct {
						Before, After int
						Replayed      bool
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.True(t, result.Success, response.Body.String())
				require.NotNil(t, result.Data)
				assert.Equal(t, 1000, result.Data.Before)
				assert.Equal(t, tc.after, result.Data.After)
				assert.Equal(t, attempt == 1, result.Data.Replayed)
				if attempt == 0 {
					// A later wallet change must not change the replayed snapshot.
					require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", tc.after+5).Error)
				}
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.after+5, user.Quota)
			var audits int64
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&audits).Error)
			assert.EqualValues(t, 1, audits, "a replay must not create another financial audit event")
		})
	}
}
