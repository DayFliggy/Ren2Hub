package service

import "github.com/QuantumNous/new-api/model"

// updateSyncUsedQuotaAndTopUpBonusWindow is the only writer of the top-up bonus
// window. Only the synchronous billing paths call it: text, audio and realtime
// settlement and the violation fee. Task billing (tasks, Midjourney and
// task-plugin requests) keeps calling the native counters and stays out of the
// window. The hourly write and the used_quota update are not in one
// transaction; either can fail without the other.
func updateSyncUsedQuotaAndTopUpBonusWindow(userID int, quota int) {
	model.RecordTopUpBonusUsage(userID, quota)
	model.UpdateUserUsedQuotaAndRequestCount(userID, quota)
}
