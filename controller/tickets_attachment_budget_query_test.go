package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func useTicketBudgetQuery(t *testing.T, query func(ctx context.Context, authorID int, since int64) (int64, error)) {
	t.Helper()
	previous := ticketAttachmentBytesSince
	ticketAttachmentBytesSince = query
	t.Cleanup(func() { ticketAttachmentBytesSince = previous })
}

func useAmpleTicketFreeSpace(t *testing.T) {
	t.Helper()
	previous := ticketFreeSpace
	ticketFreeSpace = func(string) (uint64, error) { return 20 << 30, nil }
	t.Cleanup(func() { ticketFreeSpace = previous })
}

func createUserTicket(t *testing.T, userID int) int {
	t.Helper()
	ticket := &model.Ticket{UserID: userID, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", userID, nil)
	require.NoError(t, err)
	return ticket.ID
}

func adminTicketReply(t *testing.T, ticketID int, image []byte) *bytes.Buffer {
	t.Helper()
	body, contentType := ticketMultipart(t, map[string]string{"content": "support"}, image)
	ctx, recorder := adminTicketContext(http.MethodPost, ticketID, bytes.NewReader(body), contentType)
	AddAdminTicketMessage(ctx)
	return recorder.Body
}

// ticketReservationSnapshotWithin fails instead of hanging when the
// reservation lock is held.
func ticketReservationSnapshotWithin(t *testing.T, limit time.Duration) (map[int]int64, int64) {
	t.Helper()
	type snapshot struct {
		users  map[int]int64
		global int64
	}
	done := make(chan snapshot, 1)
	go func() { users, global := ticketReservationSnapshot(); done <- snapshot{users, global} }()
	select {
	case got := <-done:
		return got.users, got.global
	case <-time.After(limit):
		t.Fatal("the reservation lock is held while a budget query waits")
		return nil, 0
	}
}

// While one user's budget query waits on the database, the reservation lock is
// free: another user's upload and a support reply reserve, write and release.
func TestTicketAttachmentBudgetQueryDoesNotBlockOtherReservations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	useAmpleTicketFreeSpace(t)
	previousTimeout := ticketAttachmentBudgetQueryTimeout
	ticketAttachmentBudgetQueryTimeout = time.Minute
	t.Cleanup(func() { ticketAttachmentBudgetQueryTimeout = previousTimeout })
	slowTicket, otherTicket := createUserTicket(t, 9), createUserTicket(t, 10)
	entered, proceed := make(chan struct{}), make(chan struct{})
	// Releasing the waiting query on failure too lets a blocked run fail cleanly.
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	var requests sync.WaitGroup
	t.Cleanup(func() { release(); requests.Wait() })
	useTicketBudgetQuery(t, func(ctx context.Context, authorID int, since int64) (int64, error) {
		if authorID == 9 {
			close(entered)
			<-proceed
		}
		return model.TicketAttachmentBytesSince(ctx, authorID, since)
	})
	image := tinyPNG(t)

	slow := make(chan []byte, 1)
	requests.Add(1)
	go func() { defer requests.Done(); slow <- userTicketReply(t, 9, slowTicket, image).Body.Bytes() }()
	<-entered
	users, global := ticketReservationSnapshotWithin(t, 5*time.Second)
	require.Equal(t, map[int]int64{9: maxTicketAttachmentSize}, users, "the waiting upload holds its reservation")
	require.Equal(t, int64(maxTicketAttachmentSize), global)

	others := make(chan []string, 1)
	requests.Add(1)
	go func() {
		defer requests.Done()
		others <- []string{userTicketReply(t, 10, otherTicket, image).Body.String(), adminTicketReply(t, slowTicket, image).String()}
	}()
	select {
	case replies := <-others:
		for _, reply := range replies {
			require.Contains(t, reply, `"success":true`)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("other reservations waited for the blocked budget query")
	}
	users, global = ticketReservationSnapshot()
	require.Equal(t, map[int]int64{9: maxTicketAttachmentSize}, users, "the other requests released what they reserved")
	require.Equal(t, int64(maxTicketAttachmentSize), global)

	release()
	require.Contains(t, string(<-slow), `"success":true`)
	requireNoTicketReservations(t)
	require.Equal(t, 3, dirEntryCount(t, dir))
}

// A budget query that fails, times out or is cancelled with its request rejects
// the upload, returns the reservation and leaves no file behind.
func TestTicketAttachmentBudgetQueryFailuresRejectAndRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		query func(cancel context.CancelFunc) func(context.Context, int, int64) (int64, error)
		want  error
	}{
		{"query error", func(context.CancelFunc) func(context.Context, int, int64) (int64, error) {
			return func(context.Context, int, int64) (int64, error) { return 0, errors.New("database unavailable") }
		}, nil},
		{"timeout", func(context.CancelFunc) func(context.Context, int, int64) (int64, error) {
			return func(ctx context.Context, _ int, _ int64) (int64, error) { <-ctx.Done(); return 0, ctx.Err() }
		}, context.DeadlineExceeded},
		{"request cancelled", func(cancel context.CancelFunc) func(context.Context, int, int64) (int64, error) {
			return func(ctx context.Context, _ int, _ int64) (int64, error) { cancel(); <-ctx.Done(); return 0, ctx.Err() }
		}, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useTicketTestDatabase(t)
			dir := t.TempDir()
			t.Setenv("TICKET_UPLOAD_DIR", dir)
			useAmpleTicketFreeSpace(t)
			previousTimeout := ticketAttachmentBudgetQueryTimeout
			ticketAttachmentBudgetQueryTimeout = 50 * time.Millisecond
			if tc.want != context.DeadlineExceeded {
				ticketAttachmentBudgetQueryTimeout = time.Minute
			}
			t.Cleanup(func() { ticketAttachmentBudgetQueryTimeout = previousTimeout })
			ticketID := createUserTicket(t, 9)
			requestCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// The query runs on the request goroutine, so plain variables suffice.
			calls, queryErr := 0, error(nil)
			inner := tc.query(cancel)
			useTicketBudgetQuery(t, func(ctx context.Context, authorID int, since int64) (int64, error) {
				calls++
				used, err := inner(ctx, authorID, since)
				queryErr = err
				return used, err
			})

			body, contentType := ticketMultipart(t, map[string]string{"content": "reply"}, tinyPNG(t))
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequestWithContext(requestCtx, http.MethodPost, "/api/tickets/"+strconv.Itoa(ticketID)+"/messages", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", contentType)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(ticketID)}}
			ctx.Set("id", 9)
			AddTicketMessage(ctx)

			require.Contains(t, recorder.Body.String(), `"success":false`)
			require.Equal(t, 1, calls)
			require.Error(t, queryErr)
			if tc.want != nil {
				require.ErrorIs(t, queryErr, tc.want)
			}
			requireNoTicketReservations(t)
			requireEmptyDir(t, dir)
			messages, err := model.ListTicketMessages(ticketID)
			require.NoError(t, err)
			require.Len(t, messages, 1, "no reply is stored")
		})
	}
}
