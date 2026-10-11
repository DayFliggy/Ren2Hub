package i18n

// Message keys for i18n translations
// Use these constants instead of hardcoded strings

// Common error messages
const (
	MsgDatabaseError = "common.database_error"
)

// Auth middleware messages
const (
	MsgAuthUserBanned = "auth.user_banned"
)

// Token related messages
const (
	MsgTokenGetInfoFailed     = "token.get_info_failed"
	MsgTokenInvalid           = "token.invalid"
	MsgTokenNotProvided       = "token.not_provided"
	MsgTokenStatusUnavailable = "token.status_unavailable"
)

// Distributor related messages
const (
	MsgDistributorInvalidRequest               = "distributor.invalid_request"
	MsgDistributorInvalidChannelId             = "distributor.invalid_channel_id"
	MsgDistributorTokenModelForbidden          = "distributor.token_model_forbidden"
	MsgDistributorModelNameRequired            = "distributor.model_name_required"
	MsgDistributorNoAvailableTaskPluginChannel = "distributor.no_available_task_plugin_channel"
	MsgDistributorInvalidMidjourney            = "distributor.invalid_midjourney_request"
	MsgDistributorInvalidParseModel            = "distributor.invalid_request_parse_model"
)

// Errors returned to AI clients (from NewAPI 128089fa7)
const (
	MsgAuthClientIPUnresolved     = "auth.client_ip_unresolved"
	MsgAuthTokenIPNotAllowed      = "auth.token_ip_not_allowed"
	MsgAuthChannelSelectForbidden = "auth.channel_select_forbidden"

	MsgRelayModelPriceNotConfigured = "relay.model_price_not_configured"
	MsgRelayTaskChannelDisabled     = "relay.task_channel_disabled"
	MsgRelayAccessTokenUnsupported  = "relay.access_token_unsupported"
)

// Emails and the name of the token created at registration, in the
// recipient's language
const (
	MsgUserDefaultTokenName = "user.default_token_name"

	MsgEmailVerificationSubject  = "email.verification.subject"
	MsgEmailVerificationGreeting = "email.verification.greeting"
	MsgEmailVerificationCode     = "email.verification.code"
	MsgEmailVerificationValidity = "email.verification.validity"

	MsgEmailPasswordResetSubject      = "email.password_reset.subject"
	MsgEmailPasswordResetGreeting     = "email.password_reset.greeting"
	MsgEmailPasswordResetLink         = "email.password_reset.link"
	MsgEmailPasswordResetLinkFallback = "email.password_reset.link_fallback"
	MsgEmailPasswordResetValidity     = "email.password_reset.validity"
)

// Low-quota notices, in the recipient's saved language
const (
	MsgNotifyQuotaLowTitle  = "notify.quota_low_title"
	MsgNotifyQuotaLowBark   = "notify.quota_low_bark"
	MsgNotifyQuotaLowGotify = "notify.quota_low_gotify"
	MsgNotifyQuotaLowEmail  = "notify.quota_low_email"
)

// Ticket notices, in the recipient's saved language
const (
	MsgTicketNotifyNewSubject         = "ticket.notify_new_subject"
	MsgTicketNotifyNewContent         = "ticket.notify_new_content"
	MsgTicketNotifyReplyNeededSubject = "ticket.notify_reply_needed_subject"
	MsgTicketNotifyReplyNeededContent = "ticket.notify_reply_needed_content"
	MsgTicketNotifyUserReplied        = "ticket.notify_user_replied"
	MsgTicketNotifySupportReplied     = "ticket.notify_support_replied"
	MsgTicketNotifyAssigned           = "ticket.notify_assigned"
)

// Channel notices sent to the root user and model update watchers, in the
// recipient's saved language
const (
	MsgChannelNotifyDisabledSubject = "channel.notify_disabled_subject"
	MsgChannelNotifyDisabledContent = "channel.notify_disabled_content"
	MsgChannelNotifyEnabledSubject  = "channel.notify_enabled_subject"
	MsgChannelNotifyEnabledContent  = "channel.notify_enabled_content"
	MsgChannelTestCompletedSubject  = "channel.test_completed_subject"
	MsgChannelTestCompletedContent  = "channel.test_completed_content"
	MsgChannelResponseTimeExceeded  = "channel.response_time_exceeded"

	MsgChannelUpstreamUpdateNotifySubject   = "channel.upstream_update_notify_subject"
	MsgChannelUpstreamUpdateSummary         = "channel.upstream_update_summary"
	MsgChannelUpstreamUpdateChangedChannels = "channel.upstream_update_changed_channels"
	MsgChannelUpstreamUpdateMoreChannels    = "channel.upstream_update_more_channels"
	MsgChannelUpstreamUpdateAddedModels     = "channel.upstream_update_added_models"
	MsgChannelUpstreamUpdateRemovedModels   = "channel.upstream_update_removed_models"
	MsgChannelUpstreamUpdateFailedChannels  = "channel.upstream_update_failed_channels"
	MsgChannelUpstreamUpdateMoreOmitted     = "channel.upstream_update_more_omitted"
)
