package controller

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	// 每个用户滚动 24 小时内能写入的附件字节，按该用户作为作者的用户消息统计。它是写入
	// 速率预算，不是累计占用上限；附件不删除。
	ticketAttachmentDailyBudget  = 200 * 1024 * 1024
	ticketAttachmentBudgetWindow = 24 * time.Hour
	// 附件目录所在文件系统在全部在途预留之后至少保留的可用空间，管理员同样受限。
	// 它只约束本应用的附件写入，不能阻止同分区的其他写入。
	ticketStorageFreeFloor = 5 * 1024 * 1024 * 1024
)

var errTicketFreeSpaceUnsupported = errors.New("ticket attachment free-space check requires Linux statfs")

// ticketFreeSpace 返回目录所在文件系统对本进程可用的字节；包级变量只为测试可替换。
var ticketFreeSpace = ticketFilesystemFreeSpace

// ticketAttachmentBudgetQueryTimeout bounds the 24-hour usage query of one
// reservation; a timeout rejects the upload instead of holding its request.
var ticketAttachmentBudgetQueryTimeout = 3 * time.Second

// ticketAttachmentBytesSince 是预算查询；包级变量只为测试可替换。
var ticketAttachmentBytesSince = model.TicketAttachmentBytesSince

// ticketUploadReservations 记录进程内在途附件的预留字节（产品是单节点）。每个附件写入前
// 按单附件上限预留，写完收缩到实际大小，请求结束时释放。预留让同时进行的上传互相可见：
// 个人预算比较“24 小时已入库 + 该用户在途预留 + 本次预留”，空间下限比较
// “可用空间 − 全局在途预留 − 本次预留”。已入库到释放之间同一笔字节会被算两次，只会更保守。
// 互斥锁只保护内存里的计数，持锁时不查数据库。
type ticketUploadReservations struct {
	mu     sync.Mutex
	users  map[int]int64
	global int64
}

var ticketUploads = ticketUploadReservations{users: map[int]int64{}}

// reserve 在一个附件写入前预留 maxTicketAttachmentSize。userID 为 0 时只检查空间下限。
//
// 个人预算按“先登记、后查询”检查：持锁检查空间并登记本次预留，同时记下该用户此刻的在途
// 预留合计（含本次）作为快照；解锁后查询 24 小时内已入库的字节，与快照相加后和预算比较。
// 用登记时的快照而不是查询之后再读在途预留，是为了不漏算：另一个请求若在本次登记之前已经
// 释放，它的字节一定已经入库，会被之后的查询读到；若登记时它还在途，就在快照里。两种情况
// 最多重复计算，不会少算。查询失败、超时、请求取消或超出预算时都释放本次预留。
func (r *ticketUploadReservations) reserve(ctx context.Context, userID int, dir string) error {
	inFlight, err := r.register(userID, dir)
	if err != nil || userID == 0 {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, ticketAttachmentBudgetQueryTimeout)
	defer cancel()
	used, err := ticketAttachmentBytesSince(queryCtx, userID, time.Now().Add(-ticketAttachmentBudgetWindow).Unix())
	if err == nil && used+inFlight > ticketAttachmentDailyBudget {
		err = &ticketFormError{common.NewMessage("Attachments are limited to {{size}} MB per 24 hours", map[string]any{"size": ticketAttachmentDailyBudget >> 20}), "ATTACHMENT_QUOTA_EXCEEDED"}
	}
	if err != nil {
		r.release(userID, maxTicketAttachmentSize)
		return err
	}
	return nil
}

// register 持锁检查空间下限并登记一个附件的预留，返回该用户此刻的在途预留合计（含本次）。
func (r *ticketUploadReservations) register(userID int, dir string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkSpaceLocked(dir); err != nil {
		return 0, err
	}
	r.global += maxTicketAttachmentSize
	if userID == 0 {
		return 0, nil
	}
	r.users[userID] += maxTicketAttachmentSize
	return r.users[userID], nil
}

func (r *ticketUploadReservations) checkSpaceLocked(dir string) error {
	free, err := ticketFreeSpace(dir)
	if errors.Is(err, errTicketFreeSpaceUnsupported) {
		// 产品只交付 Linux 镜像；其他平台的开发构建不做这项检查。
		return nil
	}
	if err != nil {
		return err
	}
	if free < uint64(ticketStorageFreeFloor+r.global+maxTicketAttachmentSize) {
		return &ticketFormError{common.NewMessage("Attachment storage is nearly full, please try again later"), "ATTACHMENT_STORAGE_FULL"}
	}
	return nil
}

// release 归还 amount 字节的预留。
func (r *ticketUploadReservations) release(userID int, amount int64) {
	if amount <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.global -= amount
	if userID == 0 {
		return
	}
	r.users[userID] -= amount
	if r.users[userID] <= 0 {
		delete(r.users, userID)
	}
}

// reserveAttachment 为下一个附件预留单附件上限；没有附件的请求不经过这里。
func (form *ticketForm) reserveAttachment() error {
	dir := ticketStorageDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := ticketUploads.reserve(form.ctx, form.budgetUserID, dir); err != nil {
		return err
	}
	form.reserved += maxTicketAttachmentSize
	return nil
}

// shrinkReservation 在附件写完后把预留收缩到实际大小。
func (form *ticketForm) shrinkReservation(unused int64) {
	ticketUploads.release(form.budgetUserID, unused)
	form.reserved -= unused
}

func (form *ticketForm) releaseReservation() {
	ticketUploads.release(form.budgetUserID, form.reserved)
	form.reserved = 0
}
