package constant

import (
	"errors"
	"sort"
)

type ChannelCategory string
type ChannelLifecycle string

const (
	ChannelCategoryOpenAICompatible ChannelCategory  = "openai-compatible"
	ChannelCategoryNativeProvider   ChannelCategory  = "native-provider"
	ChannelCategoryGateway          ChannelCategory  = "gateway"
	ChannelCategoryLocal            ChannelCategory  = "local"
	ChannelCategoryMediaTask        ChannelCategory  = "media-task"
	ChannelCategoryReserved         ChannelCategory  = "reserved"
	ChannelLifecycleActive          ChannelLifecycle = "active"
	ChannelLifecycleRetired         ChannelLifecycle = "retired"
	ChannelLifecycleReserved        ChannelLifecycle = "reserved"
)

type ChannelTypeDefinition struct {
	Name      string           `json:"name"`
	Category  ChannelCategory  `json:"category"`
	Lifecycle ChannelLifecycle `json:"lifecycle"`
}

var ErrRetiredChannelType = errors.New("retired channel type")

func IsRetiredChannelType(channelType int) bool {
	return GetChannelTypeDefinition(channelType).Lifecycle == ChannelLifecycleRetired
}

func ValidateChannelTypeLifecycle(channelType int) error {
	if IsRetiredChannelType(channelType) {
		return ErrRetiredChannelType
	}
	return nil
}

func RetiredChannelTypes() []int {
	var retired []int
	for channelType, definition := range ChannelTypes {
		if definition.Lifecycle == ChannelLifecycleRetired {
			retired = append(retired, channelType)
		}
	}
	sort.Ints(retired)
	return retired
}

func GetChannelTypeDefinition(channelType int) ChannelTypeDefinition {
	if definition, exists := ChannelTypes[channelType]; exists {
		return definition
	}
	return ChannelTypeDefinition{Name: "Unknown", Category: ChannelCategoryReserved, Lifecycle: ChannelLifecycleReserved}
}
