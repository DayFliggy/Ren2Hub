package controller

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTicketAttachmentCleanupCrashChild(t *testing.T) {
	mode := os.Getenv("TICKET_ATTACHMENT_CHILD_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "stage", "promote":
		body, contentType := ticketMultipart(t, map[string]string{"content": "crash"}, tinyPNG(t))
		ctx, _ := ticketContext(strings.NewReader(string(body)), contentType)
		form, err := readTicketForm(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "promote" {
			if err := form.commit(); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(0)
	case "hold-lock":
		path := os.Getenv("TICKET_ATTACHMENT_LOCK_PATH")
		ready := os.Getenv("TICKET_ATTACHMENT_LOCK_READY")
		release := os.Getenv("TICKET_ATTACHMENT_LOCK_RELEASE")
		file, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := lockTicketUpload(file); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(release); err == nil {
				_ = file.Close()
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(3)
	default:
		t.Fatalf("unknown child mode %q", mode)
	}
}

func crashTicketUpload(t *testing.T, dir, mode string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTicketAttachmentCleanupCrashChild$")
	env := append(os.Environ(), "TICKET_ATTACHMENT_CHILD_MODE="+mode, "TICKET_UPLOAD_DIR="+dir)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if mode == "stage" && strings.HasPrefix(entry.Name(), ".upload-") {
			return filepath.Join(dir, entry.Name())
		}
		if mode == "promote" && ticketStorageName.MatchString(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))) {
			return filepath.Join(dir, entry.Name())
		}
	}
	t.Fatalf("child did not leave the expected %s upload: %v", mode, entries)
	return ""
}

func ageTicketFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-ticketAttachmentOrphanAge - time.Minute)
	require.NoError(t, os.Chtimes(path, old, old))
}

func TestTicketAttachmentCleanupDefaultsToDryRun(t *testing.T) {
	t.Setenv("TICKET_ATTACHMENT_CLEANUP_ENABLED", "")
	t.Setenv("TICKET_ATTACHMENT_CLEANUP_DELETE_ENABLED", "")
	handler := ticketAttachmentCleanupHandler{}
	if !ticketAttachmentCleanupSupported {
		assert.False(t, handler.Enabled())
		return
	}
	assert.True(t, handler.Enabled())
	assert.Equal(t, ticketAttachmentCleanupPayload{DryRun: true}, handler.NewPayload())
}

func TestTicketAttachmentCleanupReclaimsActualProcessCrashOrphans(t *testing.T) {
	if !ticketAttachmentCleanupSupported {
		t.Skip("orphan cleanup is disabled outside Linux")
	}
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	staged := crashTicketUpload(t, dir, "stage")
	promoted := crashTicketUpload(t, dir, "promote")
	require.True(t, strings.HasPrefix(filepath.Base(staged), ".upload-"))
	require.Regexp(t, `^[0-9a-f]{48}\.png$`, filepath.Base(promoted))
	ageTicketFile(t, staged)
	ageTicketFile(t, promoted)
	dry, err := runTicketAttachmentCleanup(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, 2, dry.Orphans)
	assert.Zero(t, dry.Deleted)
	assert.Len(t, dry.Candidates, 2)
	require.FileExists(t, staged)
	require.FileExists(t, promoted)
	deleted, err := runTicketAttachmentCleanup(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted.Orphans)
	assert.Equal(t, 2, deleted.Deleted)
	requireEmptyDir(t, dir)
}

func TestTicketAttachmentCleanupProtectsReferencedFreshUnknownAndSymlinkEntries(t *testing.T) {
	if !ticketAttachmentCleanupSupported {
		t.Skip("orphan cleanup is disabled outside Linux")
	}
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	db := model.DB
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	referencedKey := strings.Repeat("a", 48) + ".png"
	referenced := filepath.Join(dir, referencedKey)
	require.NoError(t, os.WriteFile(referenced, tinyPNG(t), 0600))
	ageTicketFile(t, referenced)
	ticket := &model.Ticket{UserID: 5, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", 5, []model.TicketAttachment{{StorageKey: referencedKey, OriginalName: "r.png", MimeType: "image/png", Size: 1}})
	require.NoError(t, err)
	fresh := filepath.Join(dir, strings.Repeat("b", 48)+".png")
	require.NoError(t, os.WriteFile(fresh, tinyPNG(t), 0600))
	orphan := filepath.Join(dir, strings.Repeat("d", 48)+".png")
	require.NoError(t, os.WriteFile(orphan, tinyPNG(t), 0600))
	ageTicketFile(t, orphan)
	unknown := filepath.Join(dir, "legacy-unrecognized.bin")
	require.NoError(t, os.WriteFile(unknown, []byte("keep"), 0600))
	ageTicketFile(t, unknown)
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("do not follow"), 0600))
	symlink := filepath.Join(dir, strings.Repeat("c", 48)+".png")
	require.NoError(t, os.Symlink(outside, symlink))
	ageTicketFile(t, symlink)
	result, err := runTicketAttachmentCleanup(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Orphans)
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, 1, result.Referenced)
	assert.Equal(t, 1, result.Fresh)
	assert.GreaterOrEqual(t, result.Ignored, 2)
	require.NoFileExists(t, filepath.Join(dir, strings.Repeat("d", 48)+".png"))
	require.FileExists(t, referenced)
	require.NoFileExists(t, orphan)
	require.FileExists(t, fresh)
	require.FileExists(t, unknown)
	require.FileExists(t, outside)
	info, err := os.Lstat(symlink)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0)
	require.NoError(t, db.Error)
}

func TestTicketAttachmentCleanupWaitsForCrossProcessUploadLock(t *testing.T) {
	if !ticketAttachmentCleanupSupported {
		t.Skip("orphan cleanup is disabled outside Linux")
	}
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	path := filepath.Join(dir, strings.Repeat("e", 48)+".png")
	require.NoError(t, os.WriteFile(path, tinyPNG(t), 0600))
	ageTicketFile(t, path)
	ready, release := filepath.Join(t.TempDir(), "ready"), filepath.Join(t.TempDir(), "release")
	cmd := exec.Command(os.Args[0], "-test.run=^TestTicketAttachmentCleanupCrashChild$")
	cmd.Env = append(os.Environ(), "TICKET_ATTACHMENT_CHILD_MODE=hold-lock", "TICKET_ATTACHMENT_LOCK_PATH="+path, "TICKET_ATTACHMENT_LOCK_READY="+ready, "TICKET_ATTACHMENT_LOCK_RELEASE="+release)
	require.NoError(t, cmd.Start())
	waitFor := func(file string) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(file); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for child signal %s", file)
	}
	waitFor(ready)
	active, err := runTicketAttachmentCleanup(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, active.Active)
	assert.Zero(t, active.Deleted)
	require.FileExists(t, path)
	require.NoError(t, os.WriteFile(release, []byte("release"), 0600))
	require.NoError(t, cmd.Wait())
	after, err := runTicketAttachmentCleanup(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, after.Deleted)
	require.NoFileExists(t, path)
}

func TestTicketAttachmentCleanupStopsOnDatabaseErrorWithoutDeleting(t *testing.T) {
	if !ticketAttachmentCleanupSupported {
		t.Skip("orphan cleanup is disabled outside Linux")
	}
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	db := model.DB
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	path := filepath.Join(dir, strings.Repeat("f", 48)+".png")
	require.NoError(t, os.WriteFile(path, tinyPNG(t), 0600))
	ageTicketFile(t, path)
	require.NoError(t, db.Migrator().DropTable(&model.TicketAttachment{}))
	result, err := runTicketAttachmentCleanup(context.Background(), false)
	require.Error(t, err)
	assert.Zero(t, result.Deleted)
	require.FileExists(t, path)
}

func TestTicketAttachmentCleanupRejectsSymlinkRootAndBoundsReport(t *testing.T) {
	if !ticketAttachmentCleanupSupported {
		t.Skip("orphan cleanup is disabled outside Linux")
	}
	useTicketTestDatabase(t)
	outside := t.TempDir()
	link := filepath.Join(t.TempDir(), "attachment-link")
	require.NoError(t, os.Symlink(outside, link))
	t.Setenv("TICKET_UPLOAD_DIR", link)
	_, err := runTicketAttachmentCleanup(context.Background(), true)
	require.Error(t, err)
	require.ErrorContains(t, err, "root must be a directory, not a symlink")
}

func TestTicketAttachmentCleanupTaskDefaultPayloadCannotDelete(t *testing.T) {
	t.Setenv("TICKET_ATTACHMENT_CLEANUP_DELETE_ENABLED", "")
	payload := ticketAttachmentCleanupHandler{}.NewPayload()
	value, ok := payload.(ticketAttachmentCleanupPayload)
	require.True(t, ok)
	assert.True(t, value.DryRun)
	require.Equal(t, model.SystemTaskTypeTicketAttachmentCleanup, ticketAttachmentCleanupHandler{}.Type())
	assert.Equal(t, 100, ticketCleanupReportLimit)
}
