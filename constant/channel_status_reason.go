package constant

// Channel status reasons the gateway writes itself, as opposed to the provider
// text it records after redaction. Each is a fixed English sentence that the
// web console translates by exact match against
// frontend/src/i18n/locales/zh-CN/server-messages, so it takes no params.
const (
	ChannelStatusReasonInsufficientBalance = "Insufficient balance"
	ChannelStatusReasonUnparsableBody      = "The upstream error response could not be parsed; its body is hidden"
	ChannelStatusReasonNoDisplayableText   = "The upstream error response has no displayable message; its body is hidden"
)

// ChannelStatusReasons lists every status reason the gateway writes; the server
// message catalog test requires a translation for each.
var ChannelStatusReasons = []string{
	ChannelStatusReasonInsufficientBalance,
	ChannelStatusReasonUnparsableBody,
	ChannelStatusReasonNoDisplayableText,
}
