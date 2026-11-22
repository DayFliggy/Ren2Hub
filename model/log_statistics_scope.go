package model

import "errors"

// Administrator searches retain their explicit text filters. Self-service
// statistics constrain both queries by immutable identity instead of username.
func SumUsedQuota(logType int, start, end int64, modelName, username, tokenName string, channel int) (Stat, error) {
	return sumUsedQuota(logType, start, end, modelName, username, tokenName, channel, 0)
}

func SumUsedQuotaForUser(userID, logType int, start, end int64, modelName, tokenName string, channel int) (Stat, error) {
	if userID <= 0 {
		return Stat{}, errors.New("invalid statistics user ID")
	}
	return sumUsedQuota(logType, start, end, modelName, "", tokenName, channel, userID)
}
