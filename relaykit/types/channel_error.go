package types

type ChannelError struct {
	ChannelId   int    `json:"channel_id"`
	ChannelType int    `json:"channel_type"`
	ChannelName string `json:"channel_name"`
	AutoBan     bool   `json:"auto_ban"`
	UsingKey    string `json:"using_key"`
}

func NewChannelError(channelId int, channelType int, channelName string, usingKey string, autoBan bool) *ChannelError {
	return &ChannelError{
		ChannelId:   channelId,
		ChannelType: channelType,
		ChannelName: channelName,
		AutoBan:     autoBan,
		UsingKey:    usingKey,
	}
}
