package controller

import "time"

// checkinToday 是 GET /api/user/checkin 的本地追加字段 today。
// 口径与 model.HasCheckedInToday 相同（服务器本地日期），前端显示的“今天”
// 因此与 checked_in_today 的判定一致，不受浏览器时区和时钟影响。
func checkinToday() string {
	return time.Now().Format("2006-01-02")
}
