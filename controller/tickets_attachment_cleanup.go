package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	ticketAttachmentOrphanAge = 24 * time.Hour
	ticketCleanupReportLimit  = 100
)

var (
	errTicketCleanupUnsupported = errors.New("ticket attachment cleanup requires Linux file locking")
	ticketTemporaryName         = regexp.MustCompile(`^\.upload-[A-Za-z0-9]+$`)
	ticketStorageName           = regexp.MustCompile(`^[0-9a-f]{48}$`)
)

type ticketAttachmentCleanupPayload struct {
	DryRun bool `json:"dry_run"`
}

type ticketAttachmentCleanupCandidate struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modified_at"`
}

type ticketAttachmentCleanupResult struct {
	DryRun              bool                               `json:"dry_run"`
	MinimumAgeSeconds   int64                              `json:"minimum_age_seconds"`
	Scanned             int                                `json:"scanned"`
	Orphans             int                                `json:"orphans"`
	Deleted             int                                `json:"deleted"`
	Referenced          int                                `json:"referenced"`
	Active              int                                `json:"active"`
	Fresh               int                                `json:"fresh"`
	Ignored             int                                `json:"ignored"`
	Candidates          []ticketAttachmentCleanupCandidate `json:"candidates"`
	CandidatesTruncated bool                               `json:"candidates_truncated"`
}

type ticketAttachmentCleanupHandler struct{}

func (ticketAttachmentCleanupHandler) Type() string {
	return model.SystemTaskTypeTicketAttachmentCleanup
}

func (ticketAttachmentCleanupHandler) Enabled() bool {
	return ticketAttachmentCleanupSupported && common.GetEnvOrDefaultBool("TICKET_ATTACHMENT_CLEANUP_ENABLED", true)
}

func (ticketAttachmentCleanupHandler) Interval() time.Duration { return 24 * time.Hour }

func (ticketAttachmentCleanupHandler) NewPayload() any {
	return ticketAttachmentCleanupPayload{DryRun: !common.GetEnvOrDefaultBool("TICKET_ATTACHMENT_CLEANUP_DELETE_ENABLED", false)}
}

func (ticketAttachmentCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ticketAttachmentCleanupPayload{DryRun: true}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	if !payload.DryRun && !common.GetEnvOrDefaultBool("TICKET_ATTACHMENT_CLEANUP_DELETE_ENABLED", false) {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, errors.New("ticket attachment deletion requires TICKET_ATTACHMENT_CLEANUP_DELETE_ENABLED=true"))
		return
	}
	result, err := runTicketAttachmentCleanup(ctx, payload.DryRun)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, result, err)
}

// The root is fixed by upload configuration. os.Root anchors all subsequent
// lookups/removals to that directory, even if its parent path is renamed.
func runTicketAttachmentCleanup(ctx context.Context, dryRun bool) (*ticketAttachmentCleanupResult, error) {
	result := &ticketAttachmentCleanupResult{DryRun: dryRun, MinimumAgeSeconds: int64(ticketAttachmentOrphanAge.Seconds()), Candidates: []ticketAttachmentCleanupCandidate{}}
	if !ticketAttachmentCleanupSupported {
		return result, errTicketCleanupUnsupported
	}
	info, err := os.Lstat(ticketStorageDir())
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, errors.New("ticket attachment cleanup root must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(ticketStorageDir())
	if err != nil {
		return result, err
	}
	defer root.Close()
	openedInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, openedInfo) {
		return result, errors.New("ticket attachment cleanup root changed while opening")
	}
	dir, err := root.Open(".")
	if err != nil {
		return result, err
	}
	defer dir.Close()
	cutoff := time.Now().Add(-ticketAttachmentOrphanAge)
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entries, readErr := dir.ReadDir(256)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			result.Scanned++
			if !isManagedTicketAttachmentName(entry.Name()) {
				result.Ignored++
				continue
			}
			if err := cleanTicketAttachmentFile(ctx, root, entry.Name(), cutoff, result); err != nil {
				return result, fmt.Errorf("ticket attachment cleanup stopped at %s: %w", entry.Name(), err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return result, nil
		}
		if readErr != nil {
			return result, readErr
		}
	}
}

func isManagedTicketAttachmentName(name string) bool {
	if ticketTemporaryName.MatchString(name) {
		return true
	}
	extension := filepath.Ext(name)
	if !ticketStorageName.MatchString(strings.TrimSuffix(name, extension)) {
		return false
	}
	for _, allowed := range ticketAttachmentExtensions {
		if extension == allowed {
			return true
		}
	}
	return false
}

func cleanTicketAttachmentFile(ctx context.Context, root *os.Root, name string, cutoff time.Time, result *ticketAttachmentCleanupResult) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		result.Ignored++
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		result.Ignored++
		return nil
	}
	if !info.ModTime().Before(cutoff) {
		result.Fresh++
		return nil
	}
	file, err := openTicketCleanupFile(root, name)
	if errors.Is(err, os.ErrNotExist) {
		result.Ignored++
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	locked, err := tryLockTicketCleanupFile(file)
	if err != nil {
		return err
	}
	if !locked {
		result.Active++
		return nil
	}
	// Rename preserves the inode lock. A live uploader holds it until the DB
	// transaction commits or discard removes the promoted file.
	valid, lockedInfo, err := ticketCleanupFileUnchanged(root, name, file, info, cutoff)
	if err != nil {
		return err
	}
	if !valid {
		result.Ignored++
		return nil
	}
	if model.DB == nil {
		return errors.New("ticket attachment database is unavailable")
	}
	var references int64
	if err := model.DB.WithContext(ctx).Model(&model.TicketAttachment{}).Where("storage_key = ?", name).Count(&references).Error; err != nil {
		return err
	}
	if references != 0 {
		result.Referenced++
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The DB lookup may have taken time; never unlink a new path replacement.
	valid, _, err = ticketCleanupFileUnchanged(root, name, file, lockedInfo, cutoff)
	if err != nil {
		return err
	}
	if !valid {
		result.Ignored++
		return nil
	}
	result.Orphans++
	if len(result.Candidates) < ticketCleanupReportLimit {
		result.Candidates = append(result.Candidates, ticketAttachmentCleanupCandidate{Name: name, Size: lockedInfo.Size(), ModifiedAt: lockedInfo.ModTime().Unix()})
	} else {
		result.CandidatesTruncated = true
	}
	if !result.DryRun {
		if err := root.Remove(name); err != nil {
			return err
		}
		result.Deleted++
	}
	return nil
}

func ticketCleanupFileUnchanged(root *os.Root, name string, file *os.File, previous os.FileInfo, cutoff time.Time) (bool, os.FileInfo, error) {
	current, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return false, nil, err
	}
	valid := current.Mode().IsRegular() && opened.Mode().IsRegular() && os.SameFile(previous, opened) && os.SameFile(current, opened) && current.ModTime().Before(cutoff) && opened.ModTime().Before(cutoff)
	return valid, opened, nil
}
