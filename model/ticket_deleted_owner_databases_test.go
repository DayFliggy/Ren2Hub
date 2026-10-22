package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The admin ticket SQL names tickets, users and ticket_messages directly, so
// these tables cannot take a per-test prefix. Other isolated-database tests
// leave these tables empty; the run refuses any table that still holds rows
// instead of dropping someone else's data.
var ticketOwnerTables = []any{&TicketAttachment{}, &TicketMessage{}, &Ticket{}, &TopUp{}, &User{}}

// The LEFT JOIN, the CASE flag scanned into bool and COALESCE(SUM) differ per
// dialect; each must keep deleted requesters visible and counted once.
func TestAdminTicketsKeepDeletedOwnersDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTicketOwnerDatabase(t, dialect)
			verifyAdminTicketsKeepDeletedOwners(t, db)
		})
	}
}

func openTicketOwnerDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector = sqlite.Open(":memory:")
	dbType := common.DatabaseTypeSQLite
	if dialect != "sqlite" {
		key := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
		if dialect == "postgres" {
			key = "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
		}
		dsn := os.Getenv(key)
		if dsn == "" {
			t.Skip(key + " is not configured; isolated database required")
		}
		driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
		if dialect == "postgres" {
			driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
		}
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		connection.SetMaxOpenConns(1)
	}
	for _, table := range ticketOwnerTables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var rows int64
		require.NoError(t, db.Unscoped().Model(table).Count(&rows).Error)
		require.Zero(t, rows, "isolated test database already has %T rows", table)
	}
	previousDB, previousType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(dbType)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		require.NoError(t, db.Migrator().DropTable(ticketOwnerTables...))
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.AutoMigrate(&User{}, &Ticket{}, &TicketMessage{}, &TicketAttachment{}, &TopUp{}))
	return db
}

func verifyAdminTicketsKeepDeletedOwners(t *testing.T, db *gorm.DB) {
	t.Helper()
	empty, err := GetTicketQueueSummary(1)
	require.NoError(t, err, "COALESCE keeps an empty queue countable")
	assert.Equal(t, TicketQueueSummary{}, empty)

	users := map[string]*User{}
	for _, name := range []string{"agent", "active", "soft", "hard"} {
		user := &User{Username: name, DisplayName: name + " name", AffCode: "OWNER-" + name, Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
		require.NoError(t, db.Create(user).Error)
		users[name] = user
	}
	agentID := users["agent"].Id
	tickets := map[string]*Ticket{
		"active-open":   {UserID: users["active"].Id, Title: "active open", Status: TicketStatusOpen},
		"active-closed": {UserID: users["active"].Id, Title: "active closed", Status: TicketStatusClosed, AssigneeID: &agentID},
		"soft-open":     {UserID: users["soft"].Id, Title: "soft open", Status: TicketStatusOpen},
		"hard-open":     {UserID: users["hard"].Id, Title: "hard open", Status: TicketStatusOpen},
		"hard-replied":  {UserID: users["hard"].Id, Title: "hard replied", Status: TicketStatusReplied, AssigneeID: &agentID},
	}
	for _, ticket := range tickets {
		require.NoError(t, db.Create(ticket).Error)
	}
	for _, order := range []TopUp{
		{UserId: users["soft"].Id, TradeNo: "owner-soft", PaymentMethod: "stripe", Currency: "USD", PayAmount: "1", Status: common.TopUpStatusSuccess},
		{UserId: users["hard"].Id, TradeNo: "owner-hard", PaymentMethod: "stripe", Currency: "USD", PayAmount: "1", Status: common.TopUpStatusSuccess},
	} {
		require.NoError(t, db.Create(&order).Error)
	}
	require.NoError(t, db.Delete(users["soft"]).Error)
	require.NoError(t, db.Unscoped().Delete(users["hard"]).Error)

	list := func(filter AdminTicketFilter) ([]TicketSummary, int64) {
		filter.Page, filter.PageSize = 1, 20
		items, total, err := ListAdminTickets(filter)
		require.NoError(t, err)
		return items, total
	}
	items, total := list(AdminTicketFilter{})
	require.EqualValues(t, 5, total)
	type owner struct {
		name    string
		deleted bool
	}
	got := map[int]owner{}
	for _, item := range items {
		got[item.ID] = owner{item.UserName, item.UserDeleted}
	}
	assert.Equal(t, owner{"active", false}, got[tickets["active-open"].ID])
	assert.Equal(t, owner{"soft", true}, got[tickets["soft-open"].ID])
	assert.Equal(t, owner{"", true}, got[tickets["hard-open"].ID])
	assert.Equal(t, owner{"", true}, got[tickets["hard-replied"].ID])

	// Every badge count names a set the matching list filter can show.
	summary, err := GetTicketQueueSummary(agentID)
	require.NoError(t, err)
	_, open := list(AdminTicketFilter{Status: TicketStatusOpen})
	unassignedID := 0
	_, unassigned := list(AdminTicketFilter{Status: TicketStatusOpen, AssigneeID: &unassignedID})
	_, mineOpen := list(AdminTicketFilter{Status: TicketStatusOpen, AssigneeID: &agentID})
	_, mineReplied := list(AdminTicketFilter{Status: TicketStatusReplied, AssigneeID: &agentID})
	assert.Equal(t, TicketQueueSummary{Pending: 3, Unassigned: 3, Mine: 1}, summary)
	assert.Equal(t, summary.Pending, open)
	assert.Equal(t, summary.Unassigned, unassigned)
	assert.Equal(t, summary.Mine, mineOpen+mineReplied)

	_, softMatches := list(AdminTicketFilter{Keyword: "soft name"})
	assert.EqualValues(t, 1, softMatches)
	_, hardMatches := list(AdminTicketFilter{Keyword: "hard open"})
	assert.EqualValues(t, 1, hardMatches)

	soft, err := GetOwnerIncludingDeleted(users["soft"].Id)
	require.NoError(t, err)
	require.NotNil(t, soft)
	assert.Equal(t, "soft", soft.Username)
	assert.True(t, OwnerDeleted(soft))
	hard, err := GetOwnerIncludingDeleted(users["hard"].Id)
	require.NoError(t, err)
	assert.Nil(t, hard)
	active, err := GetOwnerIncludingDeleted(users["active"].Id)
	require.NoError(t, err)
	assert.False(t, OwnerDeleted(active))

	softOrder, err := GetTopUpDetail("owner-soft", 0)
	require.NoError(t, err)
	assert.Equal(t, "soft", softOrder.Username)
	assert.True(t, softOrder.UserDeleted)
	hardOrder, err := GetTopUpDetail("owner-hard", 0)
	require.NoError(t, err)
	assert.Equal(t, users["hard"].Id, hardOrder.UserId)
	assert.Empty(t, hardOrder.Username)
	assert.True(t, hardOrder.UserDeleted)
}
