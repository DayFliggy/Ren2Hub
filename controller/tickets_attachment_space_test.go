package controller

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Attachments are refused before any file is written when reserving the
// 10 MiB per-attachment ceiling would leave less than 5 GiB free, for users and
// support alike, and a failed free-space query is an error rather than "enough
// space".
func TestTicketAttachmentsRespectTheFreeSpaceFloor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	free, err := ticketFilesystemFreeSpace(dir)
	require.NoError(t, err)
	require.Positive(t, free)
	ticket := &model.Ticket{UserID: 9, Title: "t", Category: "api", Priority: "normal"}
	_, err = model.CreateTicket(ticket, "c", 9, nil)
	require.NoError(t, err)
	image := tinyPNG(t)
	adminReply := func() *bytes.Buffer {
		body, contentType := ticketMultipart(t, map[string]string{"content": "support"}, image)
		ctx, recorder := adminTicketContext(http.MethodPost, ticket.ID, bytes.NewReader(body), contentType)
		AddAdminTicketMessage(ctx)
		return recorder.Body
	}
	previous := ticketFreeSpace
	t.Cleanup(func() { ticketFreeSpace = previous })

	ticketFreeSpace = func(string) (uint64, error) { return 5<<30 + 10<<20 - 1, nil }
	requireTicketFailureCode(t, userTicketReply(t, 9, ticket.ID, image), "ATTACHMENT_STORAGE_FULL")
	adminBody := adminReply()
	require.Contains(t, adminBody.String(), `"code":"ATTACHMENT_STORAGE_FULL"`)
	requireEmptyDir(t, dir)
	// Text-only replies do not need attachment storage.
	requireTicketSuccess(t, userTicketReply(t, 9, ticket.ID))

	ticketFreeSpace = func(string) (uint64, error) { return 0, errors.New("statfs failed") }
	recorder := userTicketReply(t, 9, ticket.ID, image)
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Empty(t, response.Code, "a storage error is not a form error")
	requireEmptyDir(t, dir)

	ticketFreeSpace = func(string) (uint64, error) { return 5<<30 + 10<<20, nil }
	requireTicketSuccess(t, userTicketReply(t, 9, ticket.ID, image))
	require.Contains(t, adminReply().String(), `"success":true`)
	require.Equal(t, 2, dirEntryCount(t, dir))
	requireNoTicketReservations(t)
}
