package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	serviceauthz "github.com/QuantumNous/new-api/service/authz"
	"github.com/bytedance/gopkg/util/gopool"
)

func NotifyNewTicket(ticket model.Ticket) {
	gopool.Go(func() {
		lang := RootUserLanguage()
		params := map[string]any{"Id": ticket.ID, "Category": ticket.Category, "Title": ticket.Title}
		NotifyRootUser(
			dto.NotifyTypeTicketUpdate,
			i18n.Translate(lang, i18n.MsgTicketNotifyNewSubject, params),
			i18n.Translate(lang, i18n.MsgTicketNotifyNewContent, params),
		)
	})
}

func NotifyTicketUserReply(ticket model.Ticket) {
	gopool.Go(func() {
		if ticket.AssigneeID == nil {
			notifyRootAboutTicketReply(ticket)
			return
		}
		agent, ok := getEligibleTicketAgent(*ticket.AssigneeID)
		if !ok {
			notifyRootAboutTicketReply(ticket)
			return
		}
		notifyLoadedTicketUser(agent, ticket.ID, i18n.MsgTicketNotifyUserReplied, ticket.Title)
	})
}

func NotifyTicketSupportReply(ticket model.Ticket) {
	gopool.Go(func() {
		notifyTicketUser(ticket.UserID, ticket.ID, i18n.MsgTicketNotifySupportReplied, ticket.Title)
	})
}

func NotifyTicketAssignment(ticket model.Ticket, assigneeID int) {
	gopool.Go(func() {
		agent, ok := getEligibleTicketAgent(assigneeID)
		if !ok {
			return
		}
		notifyLoadedTicketUser(agent, ticket.ID, i18n.MsgTicketNotifyAssigned, ticket.Title)
	})
}

func notifyRootAboutTicketReply(ticket model.Ticket) {
	lang := RootUserLanguage()
	params := map[string]any{"Id": ticket.ID, "Title": ticket.Title}
	NotifyRootUser(
		dto.NotifyTypeTicketUpdate,
		i18n.Translate(lang, i18n.MsgTicketNotifyReplyNeededSubject, params),
		i18n.Translate(lang, i18n.MsgTicketNotifyReplyNeededContent, params),
	)
}

func getEligibleTicketAgent(userID int) (*model.User, bool) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		common.SysLog(common.LogText("failed to load ticket agent %d for notification: %s", userID, err.Error()))
		return nil, false
	}
	if user.Status != common.UserStatusEnabled || !serviceauthz.Can(user.Id, user.Role, serviceauthz.TicketReply) {
		common.SysLog(common.LogText("ticket agent %d is no longer eligible for notifications", userID))
		return nil, false
	}
	return user, true
}

func notifyTicketUser(userID, ticketID int, subjectKey, title string) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		common.SysLog(common.LogText("failed to load ticket notification user %d: %s", userID, err.Error()))
		return
	}
	notifyLoadedTicketUser(user, ticketID, subjectKey, title)
}

// notifyLoadedTicketUser renders the subject in the recipient's saved language;
// the content is the ticket title as the user wrote it.
func notifyLoadedTicketUser(user *model.User, ticketID int, subjectKey, title string) {
	setting := user.GetSetting()
	subject := i18n.Translate(setting.Language, subjectKey, map[string]any{"Id": ticketID})
	data := dto.NewNotify(dto.NotifyTypeTicketUpdate, subject, title, nil)
	if err := NotifyUser(user.Id, user.Email, setting, data); err != nil {
		common.SysLog(common.LogText("failed to notify user %d for ticket %d: %s", user.Id, ticketID, err.Error()))
	}
}
