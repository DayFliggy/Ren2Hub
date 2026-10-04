package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// syncTestUpstream serves the metadata catalog; tests mutate it between calls
// to simulate upstream changes after a preview.
type syncTestUpstream struct {
	mu      sync.Mutex
	models  []map[string]any
	vendors []map[string]any
}

func (u *syncTestUpstream) set(models, vendors []map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.models, u.vendors = models, vendors
}

func (u *syncTestUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var data []map[string]any
	switch {
	case strings.HasSuffix(r.URL.Path, "/newapi/models.json"):
		data = u.models
	case strings.HasSuffix(r.URL.Path, "/newapi/vendors.json"):
		data = u.vendors
	default:
		http.NotFound(w, r)
		return
	}
	body, err := common.Marshal(map[string]any{"success": true, "data": data})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

type syncTestPreview struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Missing   []string `json:"missing"`
		Conflicts []struct {
			ModelName    string `json:"model_name"`
			LocalVersion string `json:"local_version"`
			Fields       []struct {
				Field string `json:"field"`
			} `json:"fields"`
		} `json:"conflicts"`
		Source struct {
			Version string `json:"version"`
		} `json:"source"`
	} `json:"data"`
}

type syncTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code"`
	Data    struct {
		CreatedModels  int      `json:"created_models"`
		CreatedVendors int      `json:"created_vendors"`
		UpdatedModels  int      `json:"updated_models"`
		SkippedModels  []string `json:"skipped_models"`
		CreatedList    []string `json:"created_list"`
		UpdatedList    []string `json:"updated_list"`
	} `json:"data"`
}

// setupModelSyncTest isolates the database, points the upstream base at a local
// server and seeds one enabled channel serving channelModels.
func setupModelSyncTest(t *testing.T, channelModels []string) (*gin.Engine, *syncTestUpstream, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sync.db")), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&model.Channel{
		Id: 1, Name: "primary", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "k1", Group: "default",
	}).Error)
	abilities := make([]model.Ability, 0, len(channelModels))
	for _, name := range channelModels {
		abilities = append(abilities, model.Ability{Group: "default", Model: name, ChannelId: 1, Enabled: true})
	}
	if len(abilities) > 0 {
		require.NoError(t, db.Create(&abilities).Error)
	}

	upstream := &syncTestUpstream{}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	t.Setenv("SYNC_UPSTREAM_BASE", server.URL)
	t.Setenv("SYNC_HTTP_RETRY", "1")

	router := gin.New()
	router.GET("/api/models/sync_upstream/preview", SyncUpstreamPreview)
	router.POST("/api/models/sync_upstream", SyncUpstreamModels)
	return router, upstream, db
}

func previewModelSync(t *testing.T, router *gin.Engine) syncTestPreview {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/models/sync_upstream/preview?locale=en", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var preview syncTestPreview
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &preview))
	require.True(t, preview.Success, preview.Message)
	require.NotEmpty(t, preview.Data.Source.Version)
	return preview
}

func applyModelSync(t *testing.T, router *gin.Engine, body string) (int, syncTestResult) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/models/sync_upstream", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	var result syncTestResult
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result), recorder.Body.String())
	return recorder.Code, result
}

func syncBody(t *testing.T, value map[string]any) string {
	t.Helper()
	body, err := common.Marshal(value)
	require.NoError(t, err)
	return string(body)
}

func TestSyncUpstreamCreatesOnlyRequestedMissingModels(t *testing.T) {
	router, upstream, db := setupModelSyncTest(t, []string{"alpha", "beta", "ruled-x"})
	upstream.set([]map[string]any{
		{"model_name": "alpha", "description": "Alpha", "vendor_name": "Acme", "status": 1},
		{"model_name": "beta", "description": "Beta", "status": 1},
		{"model_name": "ruled-x", "description": "Ruled", "status": 1},
		{"model_name": "late", "description": "Late", "status": 1},
	}, []map[string]any{{"name": "Acme", "description": "Acme vendor", "status": 1}})
	hidden := &model.Model{ModelName: "ruled-", NameRule: model.NameRulePrefix, Status: 0, SyncOfficial: 1}
	require.NoError(t, hidden.Insert())

	preview := previewModelSync(t, router)
	assert.ElementsMatch(t, []string{"alpha", "beta"}, preview.Data.Missing, "rule-covered names are not missing")

	// A channel model added after the preview is missing too, but was not requested.
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "late", ChannelId: 1, Enabled: true}).Error)
	status, result := applyModelSync(t, router, syncBody(t, map[string]any{
		"locale":         "en",
		"source_version": preview.Data.Source.Version,
		"create_models":  []string{"alpha", "ruled-x", "ghost"},
	}))
	require.Equal(t, http.StatusOK, status)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, []string{"alpha"}, result.Data.CreatedList)
	assert.Equal(t, 1, result.Data.CreatedModels)
	assert.Equal(t, 1, result.Data.CreatedVendors)
	assert.ElementsMatch(t, []string{"ruled-x", "ghost"}, result.Data.SkippedModels)

	var names []string
	require.NoError(t, db.Model(&model.Model{}).Order("model_name").Pluck("model_name", &names).Error)
	assert.Equal(t, []string{"alpha", "ruled-"}, names)
	var created model.Model
	require.NoError(t, db.Where("model_name = ?", "alpha").First(&created).Error)
	assert.Equal(t, 1, created.SyncOfficial, "synced models stay eligible for later previews and overwrites")
	assert.NotZero(t, created.VendorID)
}

func TestSyncUpstreamRejectsInvalidOrStaleRequests(t *testing.T) {
	tests := []struct {
		name           string
		body           func(version string) string
		changeUpstream bool
		wantStatus     int
		wantCode       string
	}{
		{
			name:       "malformed body",
			body:       func(string) string { return `{"create_models":` },
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing source version",
			body:       func(string) string { return `{"create_models":["alpha"]}` },
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unknown overwrite field",
			body: func(version string) string {
				return fmt.Sprintf(`{"source_version":%q,"overwrite":[{"model_name":"alpha","fields":["price"]}]}`, version)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:           "upstream changed after preview",
			changeUpstream: true,
			body: func(version string) string {
				return fmt.Sprintf(`{"source_version":%q,"create_models":["alpha"]}`, version)
			},
			wantStatus: http.StatusConflict,
			wantCode:   "METADATA_SOURCE_CHANGED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, upstream, db := setupModelSyncTest(t, []string{"alpha"})
			upstream.set([]map[string]any{{"model_name": "alpha", "description": "Alpha", "status": 1}}, []map[string]any{})
			preview := previewModelSync(t, router)
			if test.changeUpstream {
				upstream.set([]map[string]any{{"model_name": "alpha", "description": "Alpha v2", "status": 1}}, []map[string]any{})
			}

			status, result := applyModelSync(t, router, test.body(preview.Data.Source.Version))
			assert.Equal(t, test.wantStatus, status)
			assert.False(t, result.Success)
			assert.Equal(t, test.wantCode, result.Code)
			var count int64
			require.NoError(t, db.Model(&model.Model{}).Count(&count).Error)
			assert.Zero(t, count, "a rejected request writes nothing")
		})
	}
}

func TestSyncUpstreamOverwriteAppliesOnlySelectedFields(t *testing.T) {
	router, upstream, db := setupModelSyncTest(t, []string{"kept", "pinned"})
	local := &model.Vendor{Name: "Local Vendor", Status: 1}
	require.NoError(t, local.Insert())
	kept := &model.Model{ModelName: "kept", Description: "old", Icon: "OldIcon", VendorID: local.Id, Status: 1, SyncOfficial: 1}
	require.NoError(t, kept.Insert())
	pinned := &model.Model{ModelName: "pinned", Description: "local only", Status: 1, SyncOfficial: 0}
	require.NoError(t, pinned.Insert())
	upstream.set([]map[string]any{
		{"model_name": "kept", "description": "new", "icon": "NewIcon", "vendor_name": "Fresh Vendor", "status": 1},
		{"model_name": "pinned", "description": "upstream", "status": 1},
	}, []map[string]any{{"name": "Fresh Vendor", "status": 1}})

	preview := previewModelSync(t, router)
	assert.Empty(t, preview.Data.Missing)
	require.Len(t, preview.Data.Conflicts, 1, "models that keep local metadata are not offered")
	assert.Equal(t, "kept", preview.Data.Conflicts[0].ModelName)
	fields := make([]string, 0, len(preview.Data.Conflicts[0].Fields))
	for _, field := range preview.Data.Conflicts[0].Fields {
		fields = append(fields, field.Field)
	}
	assert.ElementsMatch(t, []string{"description", "icon", "vendor"}, fields)

	status, result := applyModelSync(t, router, syncBody(t, map[string]any{
		"source_version": preview.Data.Source.Version,
		"locale":         "en",
		"overwrite": []map[string]any{
			{"model_name": "kept", "fields": []string{"description"}, "local_version": preview.Data.Conflicts[0].LocalVersion},
			{"model_name": "pinned", "fields": []string{"description"}, "local_version": "pinned-version"},
		},
	}))
	require.Equal(t, http.StatusOK, status)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, []string{"kept"}, result.Data.UpdatedList)
	assert.Equal(t, []string{"pinned"}, result.Data.SkippedModels)
	assert.Zero(t, result.Data.CreatedVendors)

	var gotKept, gotPinned model.Model
	require.NoError(t, db.Where("model_name = ?", "kept").First(&gotKept).Error)
	assert.Equal(t, "new", gotKept.Description)
	assert.Equal(t, "OldIcon", gotKept.Icon)
	assert.Equal(t, local.Id, gotKept.VendorID)
	require.NoError(t, db.Where("model_name = ?", "pinned").First(&gotPinned).Error)
	assert.Equal(t, "local only", gotPinned.Description)
	var vendorNames []string
	require.NoError(t, db.Model(&model.Vendor{}).Pluck("name", &vendorNames).Error)
	assert.Equal(t, []string{"Local Vendor"}, vendorNames, "a description-only overwrite never creates vendors")
}

func TestSyncUpstreamRejectsLocalMetadataChangesAfterPreview(t *testing.T) {
	router, upstream, db := setupModelSyncTest(t, []string{"kept"})
	local := &model.Model{ModelName: "kept", Description: "old", Status: 1, SyncOfficial: 1}
	require.NoError(t, local.Insert())
	upstream.set([]map[string]any{{
		"model_name": "kept", "description": "new", "status": 1,
	}}, []map[string]any{})
	preview := previewModelSync(t, router)
	require.Len(t, preview.Data.Conflicts, 1)
	require.NoError(t, db.Model(&model.Model{}).Where("id = ?", local.Id).Update("description", "edited locally").Error)

	status, result := applyModelSync(t, router, syncBody(t, map[string]any{
		"locale":         "en",
		"source_version": preview.Data.Source.Version,
		"overwrite": []map[string]any{{
			"model_name":    "kept",
			"fields":        []string{"description"},
			"local_version": preview.Data.Conflicts[0].LocalVersion,
		}},
	}))
	require.Equal(t, http.StatusConflict, status)
	assert.False(t, result.Success)
	assert.Equal(t, "METADATA_LOCAL_CHANGED", result.Code)
	var got model.Model
	require.NoError(t, db.Where("id = ?", local.Id).First(&got).Error)
	assert.Equal(t, "edited locally", got.Description)
}

func TestSyncUpstreamReportsModelsWhoseVendorCannotBeResolved(t *testing.T) {
	router, upstream, db := setupModelSyncTest(t, []string{"vendored", "plain"})
	upstream.set([]map[string]any{
		{"model_name": "vendored", "vendor_name": "Acme", "status": 1},
		{"model_name": "plain", "status": 1},
	}, []map[string]any{{"name": "Acme", "status": 1}})
	preview := previewModelSync(t, router)
	require.NoError(t, db.Migrator().DropTable(&model.Vendor{}))

	status, result := applyModelSync(t, router, syncBody(t, map[string]any{
		"locale":         "en",
		"source_version": preview.Data.Source.Version,
		"create_models":  preview.Data.Missing,
	}))
	require.Equal(t, http.StatusOK, status)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, []string{"plain"}, result.Data.CreatedList)
	assert.Equal(t, []string{"vendored"}, result.Data.SkippedModels)
	var count int64
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "vendored").Count(&count).Error)
	assert.Zero(t, count, "a vendor failure must not create the model without its vendor")
}
