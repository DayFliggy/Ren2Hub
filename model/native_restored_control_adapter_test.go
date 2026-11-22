//go:build known_upstream_restored

package model

import (
	"gorm.io/gorm"
)

func restoredControlAdminLogs() ([]*Log, int64, error) { return GetAllLogs(LogFilter{}, 0, 100) }
func restoredControlAdminQuota() ([]*QuotaData, error) { return GetQuotaDataGroupByUser(0, 2000) }

func restoredControlSeedFlowGroup(db *gorm.DB) error { return nil }
