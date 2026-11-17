package controller

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// streamedTicketUpload sends a multipart reply of unknown length whose
// attachment pauses after its first bytes until proceed is called.
type streamedTicketUpload struct {
	request  *http.Request
	recorder *httptest.ResponseRecorder
	reader   *io.PipeReader
	writer   *io.PipeWriter
	resume   chan error
	done     chan struct{}
}

func startStreamedTicketUpload(t *testing.T, path string, image []byte) *streamedTicketUpload {
	t.Helper()
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	upload := &streamedTicketUpload{reader: reader, writer: writer, resume: make(chan error, 1), done: make(chan struct{}), recorder: httptest.NewRecorder()}
	upload.request = httptest.NewRequest(http.MethodPost, path, reader)
	upload.request.ContentLength = -1
	upload.request.Header.Set("Content-Type", form.FormDataContentType())
	go func() {
		if form.WriteField("content", "reply") != nil {
			return
		}
		part, err := form.CreateFormFile("attachments", "image.png")
		if err != nil {
			return
		}
		if _, err := part.Write(image[:8]); err != nil {
			return
		}
		if failure := <-upload.resume; failure != nil {
			writer.CloseWithError(failure)
			return
		}
		if _, err := part.Write(image[8:]); err != nil {
			return
		}
		if form.Close() == nil {
			writer.Close()
		}
	}()
	t.Cleanup(func() {
		reader.Close()
		select {
		case upload.resume <- errors.New("test finished"):
		default:
		}
	})
	return upload
}

func (u *streamedTicketUpload) serve(handler gin.HandlerFunc, userID int, ticketID int) {
	ctx, _ := gin.CreateTestContext(u.recorder)
	ctx.Request = u.request
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(ticketID)}}
	ctx.Set("id", userID)
	go func() {
		defer close(u.done)
		handler(ctx)
	}()
}

func ticketReservationSnapshot() (map[int]int64, int64) {
	ticketUploads.mu.Lock()
	defer ticketUploads.mu.Unlock()
	users := map[int]int64{}
	for id, reserved := range ticketUploads.users {
		users[id] = reserved
	}
	return users, ticketUploads.global
}

func waitTicketUploads(t *testing.T, uploads []*streamedTicketUpload, want int) (finished []*streamedTicketUpload) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for len(finished) < want {
		finished = finished[:0]
		for _, upload := range uploads {
			select {
			case <-upload.done:
				finished = append(finished, upload)
			default:
			}
		}
		if len(finished) >= want {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("%d of %d uploads finished, want %d", len(finished), len(uploads), want)
		case <-time.After(5 * time.Millisecond):
		}
	}
	return finished
}

// Concurrent uploads that all read the same stored usage still cannot exceed
// the personal budget or the free-space floor together: each attachment holds
// its 10 MiB reservation while it streams, so only as many uploads as fit are
// admitted, whatever the request length says (here it is unknown).
func TestTicketAttachmentReservationsBoundConcurrentUploads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const ceiling = int64(10 << 20)
	for _, tc := range []struct {
		name    string
		support bool
		code    string
	}{
		{"personal budget", false, "ATTACHMENT_QUOTA_EXCEEDED"},
		{"free-space floor", true, "ATTACHMENT_STORAGE_FULL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useTicketTestDatabase(t)
			dir := t.TempDir()
			t.Setenv("TICKET_UPLOAD_DIR", dir)
			previous := ticketFreeSpace
			t.Cleanup(func() { ticketFreeSpace = previous })
			free := uint64(6 << 30)
			used := int64(0)
			if tc.support {
				free = 5<<30 + uint64(2*ceiling+ceiling/2)
			} else {
				used = 200<<20 - 2*ceiling - ceiling/2
			}
			ticketFreeSpace = func(string) (uint64, error) { return free, nil }
			ticket := &model.Ticket{UserID: 9, Title: "t", Category: "api", Priority: "normal"}
			_, err := model.CreateTicket(ticket, "c", 9, []model.TicketAttachment{{StorageKey: "seed.png", Size: used}})
			require.NoError(t, err)

			image := tinyPNG(t)
			uploads := make([]*streamedTicketUpload, 4)
			for i := range uploads {
				uploads[i] = startStreamedTicketUpload(t, "/api/tickets/"+strconv.Itoa(ticket.ID)+"/messages", image)
				if tc.support {
					uploads[i].serve(AddAdminTicketMessage, 100+i, ticket.ID)
				} else {
					uploads[i].serve(AddTicketMessage, 9, ticket.ID)
				}
			}
			for _, refused := range waitTicketUploads(t, uploads, 2) {
				requireTicketFailureCode(t, refused.recorder, tc.code)
			}
			users, global := ticketReservationSnapshot()
			require.Equal(t, 2*ceiling, global)
			if !tc.support {
				require.Equal(t, map[int]int64{9: 2 * ceiling}, users)
			}
			for _, upload := range uploads {
				select {
				case <-upload.done:
					continue
				default:
				}
				upload.resume <- nil
				<-upload.done
				requireTicketSuccess(t, upload.recorder)
			}
			require.Equal(t, 2, dirEntryCount(t, dir))
			requireNoTicketReservations(t)
		})
	}
}

// Every way a request ends returns its reservation and leaves no stray file:
// the client disconnecting mid-attachment, a field validation failure after the
// attachment was written, a database failure, and success.
func TestTicketAttachmentReservationsAreReleasedOnEveryOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	dir := t.TempDir()
	t.Setenv("TICKET_UPLOAD_DIR", dir)
	image := tinyPNG(t)
	ticket := &model.Ticket{UserID: 9, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", 9, nil)
	require.NoError(t, err)
	path := "/api/tickets/" + strconv.Itoa(ticket.ID) + "/messages"

	interrupted := startStreamedTicketUpload(t, path, image)
	interrupted.serve(AddTicketMessage, 9, ticket.ID)
	require.Eventually(t, func() bool { _, global := ticketReservationSnapshot(); return global > 0 }, 5*time.Second, 5*time.Millisecond)
	interrupted.resume <- io.ErrUnexpectedEOF
	<-interrupted.done
	requireTicketFailureCode(t, interrupted.recorder, "VALIDATION_ERROR")
	requireNoTicketReservations(t)
	requireEmptyDir(t, dir)

	// The attachment is staged before the missing title is noticed.
	body, contentType := ticketMultipart(t, map[string]string{"content": "c"}, image)
	ctx, recorder := ticketContext(bytes.NewReader(body), contentType)
	ctx.Set("id", 9)
	CreateTicket(ctx)
	requireTicketFailureCode(t, recorder, "VALIDATION_ERROR")
	requireNoTicketReservations(t)
	requireEmptyDir(t, dir)

	require.NoError(t, model.DB.Migrator().DropTable(&model.TicketAttachment{}))
	failed := userTicketReply(t, 9, ticket.ID, image)
	require.Contains(t, failed.Body.String(), `"success":false`)
	requireNoTicketReservations(t)
	requireEmptyDir(t, dir)
	require.NoError(t, model.DB.AutoMigrate(&model.TicketAttachment{}))

	requireTicketSuccess(t, userTicketReply(t, 9, ticket.ID, image))
	requireNoTicketReservations(t)
	require.Equal(t, 1, dirEntryCount(t, dir))
}
