package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A ticket outlives its owner: support keeps reading, replying to and closing
// it after the account is soft- or hard-deleted.
func TestAdminTicketActionsKeepDeletedOwners(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	t.Setenv("TICKET_UPLOAD_DIR", t.TempDir())
	cases := []struct {
		name         string
		hard         bool
		wantUsername string
	}{
		// A soft-deleted row still holds the name; a hard-deleted one only leaves user_id.
		{name: "soft", wantUsername: "owner-soft"},
		{name: "hard", hard: true, wantUsername: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := &model.User{Username: "owner-" + tc.name, DisplayName: tc.name, Email: tc.name + "@example.com", AffCode: "aff-" + tc.name}
			require.NoError(t, model.DB.Create(owner).Error)
			ticket := &model.Ticket{UserID: owner.Id, Title: "t", Category: "api", Priority: "normal"}
			_, err := model.CreateTicket(ticket, "c", owner.Id, nil)
			require.NoError(t, err)
			deleteQuery := model.DB
			if tc.hard {
				deleteQuery = model.DB.Unscoped()
			}
			require.NoError(t, deleteQuery.Delete(&model.User{}, owner.Id).Error)

			detail := adminTicketDetail(t, ticket.ID)
			require.Equal(t, true, detail["user_deleted"])
			user := detail["user"].(map[string]any)
			require.EqualValues(t, owner.Id, user["id"])
			require.Equal(t, tc.wantUsername, user["username"])

			body, contentType := ticketMultipart(t, map[string]string{"content": "reply"})
			replyCtx, reply := adminTicketContext(http.MethodPost, ticket.ID, bytes.NewReader(body), contentType)
			AddAdminTicketMessage(replyCtx)
			requireTicketSuccess(t, reply)

			statusCtx, status := adminTicketContext(http.MethodPatch, ticket.ID, bytes.NewReader([]byte(`{"status":"closed"}`)), "application/json")
			UpdateAdminTicketStatus(statusCtx)
			requireTicketSuccess(t, status)
			stored, err := model.GetTicket(ticket.ID)
			require.NoError(t, err)
			require.Equal(t, model.TicketStatusClosed, stored.Status)
		})
	}
}

func adminTicketContext(method string, ticketID int, body *bytes.Reader, contentType string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/admin/tickets/"+strconv.Itoa(ticketID), body)
	ctx.Request.Header.Set("Content-Type", contentType)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(ticketID)}}
	ctx.Set("id", 1)
	return ctx, recorder
}

func adminTicketDetail(t *testing.T, ticketID int) map[string]any {
	t.Helper()
	ctx, recorder := adminTicketContext(http.MethodGet, ticketID, bytes.NewReader(nil), "application/json")
	GetAdminTicket(ctx)
	data := requireTicketSuccess(t, recorder)
	return data["ticket"].(map[string]any)
}

func requireTicketSuccess(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	return response.Data
}
