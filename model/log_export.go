package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// StreamLogs uses the list's filters and privacy projection, without pagination.
// Channel names are loaded before opening the log cursor so a one-connection
// SQLite database can serve exports without waiting on its own open cursor.
func StreamLogs(ctx context.Context, userID int, all bool, filter LogFilter, emit func(*Log) error) error {
	tx := LOG_DB.WithContext(ctx)
	var err error
	if all {
		tx, err = filter.apply(tx)
	} else {
		tx, err = filter.forUser(tx, userID)
	}
	if err != nil {
		return err
	}
	var channels []struct {
		Id   int
		Name string
	}
	if err = DB.WithContext(ctx).Model(&Channel{}).Select("id", "name").Find(&channels).Error; err != nil {
		return err
	}
	names := make(map[int]string, len(channels))
	for _, channel := range channels {
		names[channel.Id] = channel.Name
	}
	order := "logs.id desc"
	if all {
		order = "logs.created_at desc, logs.id desc"
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	return streamLogRows(tx, order, func(log *Log, index int) error {
		log.ChannelName = names[log.ChannelId]
		if !all {
			formatUserLogs([]*Log{log}, index)
		} else if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			log.Id = index + 1
		}
		return emit(log)
	})
}

func streamLogRows(tx *gorm.DB, order string, emit func(*Log, int) error) error {
	rows, err := tx.Model(&Log{}).Order(order).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var log Log
		if err = tx.ScanRows(rows, &log); err != nil {
			return err
		}
		if err = emit(&log, index); err != nil {
			return err
		}
		index++
	}
	return rows.Err()
}
