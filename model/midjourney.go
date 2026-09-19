package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
)

type Midjourney struct {
	Id          int          `json:"id"`
	Code        int          `json:"code"`
	UserId      int          `json:"user_id" gorm:"index"`
	Action      string       `json:"action" gorm:"type:varchar(40);index"`
	MjId        string       `json:"mj_id" gorm:"index"`
	Prompt      string       `json:"prompt"`
	PromptEn    string       `json:"prompt_en"`
	Description string       `json:"description"`
	State       string       `json:"state"`
	SubmitTime  int64        `json:"submit_time" gorm:"index"`
	StartTime   int64        `json:"start_time" gorm:"index"`
	FinishTime  int64        `json:"finish_time" gorm:"index"`
	ImageUrl    string       `json:"image_url"`
	VideoUrl    string       `json:"video_url"`
	VideoUrls   string       `json:"video_urls"`
	Status      string       `json:"status" gorm:"type:varchar(20);index"`
	Progress    string       `json:"progress" gorm:"type:varchar(30);index"`
	FailReason  string       `json:"fail_reason"`
	ChannelId   int          `json:"channel_id"`
	Quota       common.Quota `json:"quota" gorm:"type:bigint"`
	Buttons     string       `json:"buttons"`
	Properties  string       `json:"properties"`

	TokenId          int `json:"-" gorm:"default:0"`
	BillingChannelId int `json:"-" gorm:"default:0"`
	// The original task table remains the owner of Midjourney jobs. No raw key
	// is written here; private metadata references the stable credential.
	PrivateData TaskPrivateData `json:"-" gorm:"type:json"`
}

// TaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type TaskQueryParams struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
}

func GetAllUserTask(userId int, startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllTasks(startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllUnFinishTasks(ctx context.Context) ([]*Midjourney, error) {
	var tasks []*Midjourney
	// Only an explicit provider terminal status ends polling.
	err := DB.WithContext(ctx).Where("status IS NULL OR status NOT IN ?", []string{"SUCCESS", "FAILURE"}).Find(&tasks).Error
	return tasks, err
}

// HasUnfinishedMidjourneyTasks reports whether at least one Midjourney task is
// still in progress. It is a cheap existence check (LIMIT 1) used to decide
// whether the midjourney_poll system task needs to run; when no task is pending
// the scheduler skips creating a row entirely.
func HasUnfinishedMidjourneyTasks() bool {
	var id int
	err := DB.Model(&Midjourney{}).
		Where("status IS NULL OR status NOT IN ?", []string{"SUCCESS", "FAILURE"}).
		Limit(1).
		Pluck("id", &id).Error
	if err != nil {
		common.SysError("query unfinished Midjourney tasks: " + err.Error())
		return false
	}
	return id != 0
}

func GetByOnlyMJId(mjId string) *Midjourney {
	var rows []Midjourney
	if err := DB.Where("mj_id = ?", mjId).Limit(2).Find(&rows).Error; err != nil || len(rows) != 1 {
		return nil
	}
	return &rows[0]
}

func GetMjByuId(id int) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("id = ?", id).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func (midjourney *Midjourney) Insert() error {
	var err error
	err = DB.Create(midjourney).Error
	return err
}

func (midjourney *Midjourney) GetBillingChannelId() int {
	if midjourney.BillingChannelId > 0 {
		return midjourney.BillingChannelId
	}
	return midjourney.ChannelId
}

// CountAllTasks returns total midjourney tasks for admin query
func CountAllTasks(queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func CountAllUserTask(userId int, queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
