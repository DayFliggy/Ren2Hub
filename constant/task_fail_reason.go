package constant

// Fail reasons the gateway writes to a task itself, as opposed to the reasons
// a provider reports. Each is a fixed English sentence that the web console
// translates by exact match against
// frontend/src/i18n/locales/zh-CN/server-messages, so it takes no params;
// statuses and provider detail go to the server log instead.
const (
	TaskFailReasonTimedOut          = "Task timed out without returning a result"
	TaskFailReasonMissingUpstreamID = "Missing the upstream task identifier"
	TaskFailReasonUpstreamNotFound  = "Upstream task not found"
	TaskFailReasonPollGaveUp        = "Failed to query the upstream task status"
)

// TaskFailReasons lists every fail reason the gateway writes; the server
// message catalog test requires a translation for each.
var TaskFailReasons = []string{
	TaskFailReasonTimedOut,
	TaskFailReasonMissingUpstreamID,
	TaskFailReasonUpstreamNotFound,
	TaskFailReasonPollGaveUp,
}
