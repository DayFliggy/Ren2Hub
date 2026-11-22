package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

const channelRedactedValue = `{"__ren2hub_redacted__":true}`
const channelRedactedMarker = "__ren2hub_redacted__"

type channelCredentialView struct {
	*model.Channel
	RedactedFields []string `json:"redacted_fields"`
}

// The same field definitions drive reads, omission, and placeholder rejection.
type channelCredentialField struct {
	name      string
	get       func(*model.Channel) *string
	set       func(*model.Channel, *string)
	sensitive func(string) bool
}

var channelCredentialDefinitions = []channelCredentialField{
	{"header_override", func(c *model.Channel) *string { return c.HeaderOverride }, func(c *model.Channel, v *string) { c.HeaderOverride = v }, nil},
	{"param_override", func(c *model.Channel) *string { return c.ParamOverride }, func(c *model.Channel, v *string) { c.ParamOverride = v }, nil},
	{"setting", func(c *model.Channel) *string { return c.Setting }, func(c *model.Channel, v *string) { c.Setting = v }, channelSettingHasCredentials},
	{"settings", func(c *model.Channel) *string { return &c.OtherSettings }, func(c *model.Channel, v *string) {
		c.OtherSettings = ""
		if v != nil {
			c.OtherSettings = *v
		}
	}, channelSettingsHaveCredentials},
}

func channelCredentialFields(channel *model.Channel) []string {
	fields := make([]string, 0, len(channelCredentialDefinitions))
	for _, field := range channelCredentialDefinitions {
		value := field.get(channel)
		if value != nil && strings.TrimSpace(*value) != "" && (field.sensitive == nil || field.sensitive(*value)) {
			fields = append(fields, field.name)
		}
	}
	return fields
}

func channelSettingHasCredentials(raw string) bool {
	var setting struct {
		Proxy string `json:"proxy"`
	}
	if common.Unmarshal([]byte(raw), &setting) != nil {
		return true
	}
	proxy, err := url.Parse(setting.Proxy)
	return err != nil || proxy.User != nil
}

func channelSettingsHaveCredentials(raw string) bool {
	var settings struct {
		PluginConfig   any `json:"plugin_config"`
		AdvancedCustom struct {
			Routes []struct {
				Auth struct {
					Value string `json:"value"`
				} `json:"auth"`
			} `json:"advanced_routes"`
		} `json:"advanced_custom"`
	}
	if common.Unmarshal([]byte(raw), &settings) != nil {
		return true
	}
	if channelConfigHasValue(settings.PluginConfig) {
		return true
	}
	for _, route := range settings.AdvancedCustom.Routes {
		if route.Auth.Value != "" {
			return true
		}
	}
	return false
}

// False and zero remain configured values; only empty containers, null and
// empty strings are empty. Plugin schemas need not be loaded to hide secrets.
func channelConfigHasValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case []any:
		for _, item := range v {
			if channelConfigHasValue(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range v {
			if channelConfigHasValue(item) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func channelContainsRedaction(channel *model.Channel) bool {
	for _, field := range channelCredentialDefinitions {
		value := field.get(channel)
		if value != nil && containsChannelRedaction(*value) {
			return true
		}
	}
	return false
}

func rejectChannelRedaction(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "CHANNEL_REDACTED_FIELD_INVALID", "message": "Invalid redacted channel field"})
}

func projectChannelCredentials(c *gin.Context, channel *model.Channel) channelCredentialView {
	copy := *channel
	view := channelCredentialView{Channel: &copy, RedactedFields: []string{}}
	if c.GetInt("role") == common.RoleRootUser && middleware.CanWithAccessToken(c, authz.ChannelWrite) {
		return view
	}
	view.RedactedFields = channelCredentialFields(channel)
	for _, field := range channelCredentialDefinitions {
		for _, name := range view.RedactedFields {
			if field.name == name {
				value := channelRedactedValue
				field.set(&copy, &value)
				break
			}
		}
	}
	return view
}

func projectChannelsCredentials(c *gin.Context, channels []*model.Channel) []channelCredentialView {
	views := make([]channelCredentialView, len(channels))
	for i, channel := range channels {
		views[i] = projectChannelCredentials(c, channel)
	}
	return views
}

// A placeholder is an instruction to leave the entire field untouched, never
// credential data. Validate against the caller's current projection to reject
// forged markers and mixed objects. Omitted fields follow the same write path.
func normalizeChannelRedactions(c *gin.Context, channel *PatchChannel, origin *model.Channel, request map[string]any) error {
	projected := projectChannelCredentials(c, origin)
	allowed := make(map[string]bool, len(projected.RedactedFields))
	for _, field := range projected.RedactedFields {
		allowed[field] = true
	}
	for _, field := range channelCredentialDefinitions {
		raw, ok := request[field.name].(string)
		if ok && containsChannelRedaction(raw) {
			if raw != channelRedactedValue || !allowed[field.name] {
				return fmt.Errorf("invalid redacted field")
			}
			delete(request, field.name)
		}
		// Validation sees stored config; persistence omits it to avoid restoring a
		// stale credential over a concurrent update.
		if _, ok := request[field.name]; !ok {
			field.set(&channel.Channel, field.get(origin))
		}
	}
	return nil
}

func omitUnsubmittedChannelCredentials(channel *PatchChannel, request map[string]any) {
	for _, field := range channelCredentialDefinitions {
		if _, ok := request[field.name]; !ok {
			field.set(&channel.Channel, nil)
		}
	}
}

func containsChannelRedaction(raw string) bool {
	if strings.Contains(raw, channelRedactedMarker) {
		return true
	}
	var value any
	if common.Unmarshal([]byte(raw), &value) != nil {
		return false
	}
	canonical, err := common.Marshal(value)
	return err == nil && strings.Contains(string(canonical), channelRedactedMarker)
}

// Validation needs the retained type's configuration even when a patch omits
// its type. Borrow the dependent fields only for validation, never persistence.
func validateChannelPatch(channel *PatchChannel, origin *model.Channel, request map[string]any) error {
	if _, provided := request["type"]; provided {
		return validateChannel(&channel.Channel, false)
	}
	requestedType, requestedBaseURL, requestedOther := channel.Type, channel.BaseURL, channel.Other
	channel.Type = origin.Type
	_, baseURLProvided := request["base_url"]
	_, otherProvided := request["other"]
	if !baseURLProvided {
		channel.BaseURL = origin.BaseURL
	}
	if !otherProvided {
		channel.Other = origin.Other
	}
	defer func() {
		channel.Type = requestedType
		if !baseURLProvided {
			channel.BaseURL = requestedBaseURL
		}
		if !otherProvided {
			channel.Other = requestedOther
		}
	}()
	return validateChannel(&channel.Channel, false)
}
