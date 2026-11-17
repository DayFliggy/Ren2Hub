package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func userTicketReply(t *testing.T, userID, ticketID int, files ...[]byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := ticketMultipart(t, map[string]string{"content": "reply"}, files...)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tickets/"+strconv.Itoa(ticketID)+"/messages", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", contentType)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(ticketID)}}
	ctx.Set("id", userID)
	AddTicketMessage(ctx)
	return recorder
}

func requireTicketFailureCode(t *testing.T, recorder *httptest.ResponseRecorder, code string) {
	t.Helper()
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	require.False(t, response.Success, recorder.Body.String())
	require.Equal(t, code, response.Code, recorder.Body.String())
}

func dirEntryCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	return len(entries)
}

// Each user may write 200 MiB of ticket attachments per rolling 24 hours,
// counted from the user messages they wrote; every attachment reserves the
// 10 MiB per-attachment ceiling before it is written. Older uploads and support
// replies do not count, the budget is per user, and a refused upload leaves no
// file and no reservation.
func TestTicketAttachmentDailyBudgetPerUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	const budget = int64(200 << 20)
	image := tinyPNG(t)
	const ceiling = int64(10 << 20)

	ticket := &model.Ticket{UserID: 9, Title: "t", Category: "api", Priority: "normal"}
	// Exactly one more attachment reservation fits in what is left.
	_, err := model.CreateTicket(ticket, "c", 9, []model.TicketAttachment{{StorageKey: "recent.png", Size: budget - ceiling}})
	require.NoError(t, err)
	old, _, err := model.AddTicketMessage(ticket.ID, 9, "old", []model.TicketAttachment{{StorageKey: "old.png", Size: budget}})
	require.NoError(t, err)
	dayAgo := time.Now().Add(-24*time.Hour - time.Minute).Unix()
	require.NoError(t, model.DB.Model(&model.TicketMessage{}).Where("id = ?", old.ID).Update("created_at", dayAgo).Error)
	require.NoError(t, model.DB.Model(&model.TicketAttachment{}).Where("message_id = ?", old.ID).Update("created_at", dayAgo).Error)
	_, _, err = model.AddSupportTicketMessage(ticket.ID, 9, "support", []model.TicketAttachment{{StorageKey: "support.png", Size: budget}})
	require.NoError(t, err)

	requireTicketSuccess(t, userTicketReply(t, 9, ticket.ID, image))
	require.Equal(t, 1, dirEntryCount(t, dir))
	requireTicketFailureCode(t, userTicketReply(t, 9, ticket.ID, image), "ATTACHMENT_QUOTA_EXCEEDED")
	require.Equal(t, 1, dirEntryCount(t, dir), "a rejected upload must not leave a file")
	// A text-only reply needs no attachment budget.
	requireTicketSuccess(t, userTicketReply(t, 9, ticket.ID))

	other := &model.Ticket{UserID: 10, Title: "t", Category: "api", Priority: "normal"}
	_, err = model.CreateTicket(other, "c", 10, nil)
	require.NoError(t, err)
	requireTicketSuccess(t, userTicketReply(t, 10, other.ID, image))

	// Support replies are not bound by the replier's personal budget.
	body, contentType := ticketMultipart(t, map[string]string{"content": "support"}, image)
	replyCtx, reply := adminTicketContext(http.MethodPost, ticket.ID, bytes.NewReader(body), contentType)
	replyCtx.Set("id", 9)
	AddAdminTicketMessage(replyCtx)
	requireTicketSuccess(t, reply)
	require.Equal(t, 3, dirEntryCount(t, dir))
	requireNoTicketReservations(t)
}

func requireNoTicketReservations(t *testing.T) {
	t.Helper()
	ticketUploads.mu.Lock()
	defer ticketUploads.mu.Unlock()
	require.Empty(t, ticketUploads.users)
	require.Zero(t, ticketUploads.global)
}
