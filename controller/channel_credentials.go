package controller

import (
	"fmt"
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

func channelCredentialFields(channel *model.Channel) []string {
	fields := make([]string, 0, 3)
	if channel.HeaderOverride != nil && strings.TrimSpace(*channel.HeaderOverride) != "" {
		fields = append(fields, "header_override")
	}
	if channel.Setting != nil && strings.TrimSpace(*channel.Setting) != "" {
		var setting struct {
			Proxy string `json:"proxy"`
		}
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		proxy, parseErr := url.Parse(setting.Proxy)
		if err != nil || parseErr != nil || proxy.User != nil {
			fields = append(fields, "setting")
		}
	}
	if strings.TrimSpace(channel.OtherSettings) != "" {
		var settings struct {
			AdvancedCustom struct {
				Routes []struct {
					Auth struct {
						Value string `json:"value"`
					} `json:"auth"`
				} `json:"advanced_routes"`
			} `json:"advanced_custom"`
		}
		if common.Unmarshal([]byte(channel.OtherSettings), &settings) != nil {
			fields = append(fields, "settings")
		} else {
			for _, route := range settings.AdvancedCustom.Routes {
				if route.Auth.Value != "" {
					fields = append(fields, "settings")
					break
				}
			}
		}
	}
	return fields
}

func projectChannelCredentials(c *gin.Context, channel *model.Channel) channelCredentialView {
	copy := *channel
	view := channelCredentialView{Channel: &copy, RedactedFields: []string{}}
	if c.GetInt("role") == common.RoleRootUser && middleware.CanWithAccessToken(c, authz.ChannelWrite) {
		return view
	}
	view.RedactedFields = channelCredentialFields(channel)
	for _, field := range view.RedactedFields {
		value := channelRedactedValue
		switch field {
		case "header_override":
			copy.HeaderOverride = &value
		case "setting":
			copy.Setting = &value
		case "settings":
			copy.OtherSettings = value
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
	for _, field := range []string{"header_override", "setting", "settings"} {
		raw, ok := request[field].(string)
		if !ok || !containsChannelRedaction(raw) {
			continue
		}
		if raw != channelRedactedValue || !allowed[field] {
			return fmt.Errorf("invalid redacted field")
		}
		delete(request, field)
	}
	// Validation sees the stored config, while the write omits these fields so
	// a concurrent credential update cannot be overwritten by a stale read.
	if _, ok := request["header_override"]; !ok {
		channel.HeaderOverride = origin.HeaderOverride
	}
	if _, ok := request["setting"]; !ok {
		channel.Setting = origin.Setting
	}
	if _, ok := request["settings"]; !ok {
		channel.OtherSettings = origin.OtherSettings
	}
	return nil
}

func omitUnsubmittedChannelCredentials(channel *PatchChannel, request map[string]any) {
	if _, ok := request["header_override"]; !ok {
		channel.HeaderOverride = nil
	}
	if _, ok := request["setting"]; !ok {
		channel.Setting = nil
	}
	if _, ok := request["settings"]; !ok {
		channel.OtherSettings = ""
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
