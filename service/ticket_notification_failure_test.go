package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTicketNotificationFailurePreservesCommittedReply(t *testing.T) {
	allowLocalTicketNotifications(t)
	oldDB, oldType := model.DB, common.MainDatabaseType()
	oldClient, oldWorker := httpClient, system_setting.WorkerUrl
	fetch := system_setting.GetFetchSetting()
	oldProtection := fetch.EnableSSRFProtection
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	fetch.EnableSSRFProtection, system_setting.WorkerUrl = false, ""
	t.Cleanup(func() {
		model.DB = oldDB
		common.SetMainDatabaseType(oldType)
		httpClient, system_setting.WorkerUrl = oldClient, oldWorker
		fetch.EnableSSRFProtection = oldProtection
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Ticket{}, &model.TicketMessage{}, &model.TicketAttachment{}))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	httpClient = server.Client()
	settings := dto.UserSetting{NotifyType: dto.NotifyTypeWebhook, WebhookUrl: server.URL}
	user := model.User{Id: 918203}
	encoded, err := common.Marshal(settings)
	require.NoError(t, err)
	user.Setting = string(encoded)
	ticket := model.Ticket{UserID: user.Id, Title: "Local notification failure", Category: "api", Priority: "normal"}
	_, err = model.CreateTicket(&ticket, "Need help", user.Id, nil)
	require.NoError(t, err)
	reply, updated, err := model.AddSupportTicketMessage(ticket.ID, 99, "Persist this support response", nil)
	require.NoError(t, err)
	// The provider error is observable at the delivery boundary. Ticket handlers
	// notify only after the model transaction has committed its business result.
	err = NotifyUser(user.Id, "", settings, dto.NewNotify(ticketNotifyType, "Ticket", ticket.Title, nil))
	require.ErrorContains(t, err, "503")
	notifyLoadedTicketUser(&user, ticket.ID, i18n.MsgTicketNotifySupportReplied, ticket.Title)
	assert.EqualValues(t, 2, calls.Load())
	var stored model.Ticket
	require.NoError(t, db.First(&stored, ticket.ID).Error)
	assert.Equal(t, model.TicketStatusReplied, stored.Status)
	assert.Equal(t, updated.AssigneeID, stored.AssigneeID)
	var messages []model.TicketMessage
	require.NoError(t, db.Where("ticket_id = ?", ticket.ID).Order("id").Find(&messages).Error)
	require.Len(t, messages, 2)
	assert.Equal(t, reply.ID, messages[1].ID)
	assert.Equal(t, "Persist this support response", messages[1].Content)
}
