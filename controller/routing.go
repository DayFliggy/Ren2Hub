package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type eligibleRouteChannel struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	Status   int    `json:"status"`
	Models   string `json:"models"`
	Priority int64  `json:"priority"`
	Weight   int    `json:"weight"`
}

type routeCapabilityItem struct {
	ID             int     `json:"id"`
	ChannelID      int     `json:"channel_id"`
	RequestModel   string  `json:"request_model"`
	ActualModel    string  `json:"actual_model"`
	LabSlug        string  `json:"lab_slug"`
	Confidence     float64 `json:"confidence"`
	Source         string  `json:"source"`
	CatalogVersion string  `json:"catalog_version"`
	State          string  `json:"state"`
}

func ListRouteProfiles(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	profiles, err := service.ListUserRouteProfiles(c.GetInt("id"))
	if err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func CreateRouteProfile(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	var input service.RouteProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeRouteBindingError(c)
		return
	}
	input.UserID = c.GetInt("id")
	profile, err := service.CreateUserRouteProfile(input)
	if err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func GetRouteProfile(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	profileID, ok := parseRouteProfileID(c)
	if !ok {
		return
	}
	profile, err := service.GetUserRouteProfile(c.GetInt("id"), profileID)
	if err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func UpdateRouteProfile(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	profileID, ok := parseRouteProfileID(c)
	if !ok {
		return
	}
	var input service.RouteProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeRouteBindingError(c)
		return
	}
	input.UserID = c.GetInt("id")
	profile, err := service.UpdateUserRouteProfile(profileID, input)
	if err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func DeleteRouteProfile(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	profileID, ok := parseRouteProfileID(c)
	if !ok {
		return
	}
	if err := service.DeleteUserRouteProfile(c.GetInt("id"), profileID); err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

func PreviewRouteProfile(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	profileID, ok := parseRouteProfileID(c)
	if !ok {
		return
	}
	profile, err := service.GetUserRouteProfile(c.GetInt("id"), profileID)
	if err != nil {
		writeRouteError(c, err)
		return
	}
	channels, err := listEligibleRouteChannels(c.GetInt("id"))
	if err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"profile": profile, "eligible_channels": channels, "live_selection": false})
}

func ListEligibleRouteChannels(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	channels, err := listEligibleRouteChannels(c.GetInt("id"))
	if err != nil {
		writeRouteError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

func ListRouteCatalog(c *gin.Context) {
	if !requireTokenPrivateRouting(c) {
		return
	}
	eligibleChannels, err := listEligibleRouteChannels(c.GetInt("id"))
	if err != nil {
		writeRouteError(c, err)
		return
	}
	channelIDs := make([]int, 0, len(eligibleChannels))
	for _, channel := range eligibleChannels {
		channelIDs = append(channelIDs, channel.ID)
	}
	if len(channelIDs) == 0 {
		common.ApiSuccess(c, gin.H{"catalog_version": "", "items": []routeCapabilityItem{}})
		return
	}
	query := model.DB.Model(&model.ChannelModelCapability{}).Order("request_model asc, channel_id asc")
	query = query.Where("channel_id IN ?", channelIDs)
	if requestModel := strings.TrimSpace(c.Query("model")); requestModel != "" {
		query = query.Where("request_model = ?", requestModel)
	}
	if lab := strings.TrimSpace(c.Query("lab")); lab != "" {
		query = query.Where("lab_slug = ?", lab)
	}
	var capabilities []model.ChannelModelCapability
	if err := query.Find(&capabilities).Error; err != nil {
		writeRouteError(c, err)
		return
	}
	items := make([]routeCapabilityItem, 0, len(capabilities))
	for _, capability := range capabilities {
		items = append(items, routeCapabilityItem{
			ID: capability.ID, ChannelID: capability.ChannelID,
			RequestModel: capability.RequestModel, ActualModel: capability.ActualModel,
			LabSlug: capability.LabSlug, Confidence: capability.Confidence,
			Source: capability.Source, CatalogVersion: capability.CatalogVersion,
			State: capability.State,
		})
	}
	common.ApiSuccess(c, gin.H{"catalog_version": "", "items": items})
}

func requireTokenPrivateRouting(c *gin.Context) bool {
	if tokenPrivateRoutingEnabled() {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": "token private routing is disabled",
		"code":    "feature_disabled",
	})
	return false
}

func tokenPrivateRoutingEnabled() bool {
	// The data/API foundation is intentionally not live until a selector,
	// billing invariants, and shadow rollout are implemented.
	return false
}

func parseRouteProfileID(c *gin.Context) (int, bool) {
	profileID, err := strconv.Atoi(c.Param("id"))
	if err != nil || profileID <= 0 {
		writeRouteError(c, errors.New("invalid route profile id"))
		return 0, false
	}
	return profileID, true
}

func listEligibleRouteChannels(userID int) ([]eligibleRouteChannel, error) {
	var channels []model.Channel
	if err := model.DB.Where("status = ?", common.ChannelStatusEnabled).Order("priority desc, id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	items := make([]eligibleRouteChannel, 0, len(channels))
	for _, channel := range channels {
		if err := service.ValidatePlatformChannelEntitlement(userID, channel.Id); err != nil {
			continue
		}
		items = append(items, eligibleRouteChannel{
			ID: channel.Id, Name: channel.Name, Type: channel.Type, Status: channel.Status,
			Models: channel.Models, Priority: channel.GetPriority(), Weight: channel.GetWeight(),
		})
	}
	return items, nil
}

func writeRouteError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	code := "VALIDATION_ERROR"
	switch {
	case errors.Is(err, service.ErrRouteProfileNotFound):
		status = http.StatusNotFound
		code = "NOT_FOUND"
	case errors.Is(err, service.ErrRouteProfileForbidden):
		status = http.StatusForbidden
		code = "FORBIDDEN"
	case errors.Is(err, service.ErrRouteProfileConflict):
		status = http.StatusConflict
		code = "VERSION_CONFLICT"
	case errors.Is(err, service.ErrRouteProfileAlreadyExists):
		status = http.StatusConflict
		code = "ALREADY_EXISTS"
	case errors.Is(err, service.ErrRouteProfileValidation):
		status = http.StatusUnprocessableEntity
		code = "VALIDATION_ERROR"
	default:
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error(), "code": code})
}

func writeRouteBindingError(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"message": "invalid route profile request",
		"code":    "BAD_REQUEST",
	})
}
