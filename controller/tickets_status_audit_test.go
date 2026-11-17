package controller

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Support's status update is audited only when the status actually changes;
// repeating the current status, or "open" on a replied ticket, records nothing.
func TestAdminTicketStatusAuditsOnlyRealTransitions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuditLog{}))
	ticket := &model.Ticket{UserID: 9, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", 9, nil)
	require.NoError(t, err)
	audits := func() int64 {
		var count int64
		require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("action = ?", "ticket.status_update").Count(&count).Error)
		return count
	}
	setStatus := func(status string) map[string]any {
		ctx, recorder := adminTicketContext(http.MethodPatch, ticket.ID, bytes.NewReader([]byte(`{"status":"`+status+`"}`)), "application/json")
		UpdateAdminTicketStatus(ctx)
		return requireTicketSuccess(t, recorder)["ticket"].(map[string]any)
	}
	require.Equal(t, "open", setStatus("open")["status"])
	require.Zero(t, audits())
	require.Equal(t, "closed", setStatus("closed")["status"])
	require.EqualValues(t, 1, audits())
	require.Equal(t, "closed", setStatus("closed")["status"])
	require.EqualValues(t, 1, audits())

	require.Equal(t, "open", setStatus("open")["status"])
	_, _, err = model.AddSupportTicketMessage(ticket.ID, 1, "reply", nil)
	require.NoError(t, err)
	require.Equal(t, "replied", setStatus("open")["status"])
	require.EqualValues(t, 2, audits())
}
