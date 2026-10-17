package service

import (
	"bufio"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allowLocalTicketNotifications(t *testing.T) {
	t.Helper()
	oldRedis, oldLimit := common.RedisEnabled, constant.NotifyLimitCount
	common.RedisEnabled, constant.NotifyLimitCount = false, 10000
	t.Cleanup(func() { common.RedisEnabled, constant.NotifyLimitCount = oldRedis, oldLimit })
}

// Receive one message through the real SMTP protocol. No production settings or
// recipient are used, and a deadline turns a broken handshake into a test error.
func receiveTicketTestMail(listener net.Listener) (string, error) {
	conn, err := listener.Accept()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}
	reader := textproto.NewReader(bufio.NewReader(conn))
	if _, err := fmt.Fprint(conn, "220 localhost test SMTP\r\n"); err != nil {
		return "", err
	}
	var message string
	for {
		line, err := reader.ReadLine()
		if err != nil {
			return "", err
		}
		response := "250 OK\r\n"
		switch {
		case strings.HasPrefix(line, "EHLO "), strings.HasPrefix(line, "HELO "), strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
		case line == "DATA":
			if _, err := fmt.Fprint(conn, "354 Send message\r\n"); err != nil {
				return "", err
			}
			body, err := reader.ReadDotBytes()
			if err != nil {
				return "", err
			}
			message = string(body)
		case line == "QUIT":
			_, err := fmt.Fprint(conn, "221 Bye\r\n")
			return message, err
		default:
			return "", fmt.Errorf("unexpected SMTP command: %q", line)
		}
		if _, err := fmt.Fprint(conn, response); err != nil {
			return "", err
		}
	}
}

func TestTicketNotificationSMTPOutputBoundary(t *testing.T) {
	allowLocalTicketNotifications(t)
	oldServer, oldPort, oldFrom := common.SMTPServer, common.SMTPPort, common.SMTPFrom
	oldAccount, oldToken := common.SMTPAccount, common.SMTPToken
	oldSSL, oldStartTLS := common.SMTPSSLEnabled, common.SMTPStartTLSEnabled
	t.Cleanup(func() {
		common.SMTPServer, common.SMTPPort, common.SMTPFrom = oldServer, oldPort, oldFrom
		common.SMTPAccount, common.SMTPToken = oldAccount, oldToken
		common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = oldSSL, oldStartTLS
	})
	common.SMTPServer, common.SMTPFrom = "127.0.0.1", "sender@example.invalid"
	common.SMTPAccount, common.SMTPToken = "", ""
	common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = false, false
	for _, tc := range []struct {
		name, kind, content, want string
		values                    []any
	}{
		{"ticket title", ticketNotifyType, `<a href="https://example.invalid">工单</a> & review`, `&lt;a href=&#34;https://example.invalid&#34;&gt;工单&lt;/a&gt; &amp; review`, nil},
		{"ticket interpolated value", ticketNotifyType, "Request: {{value}}", "Request: &lt;b&gt;unsafe&lt;/b&gt; &amp; text", []any{"<b>unsafe</b> & text"}},
		{"ticket literal entity", ticketNotifyType, "Literal &lt;b&gt;\n第二行", "Literal &amp;lt;b&amp;gt;\n第二行", nil},
		{"native HTML notification", dto.NotifyTypeQuotaExceed, "Remaining: {{value}}<br/>", "Remaining: 10<br/>", []any{10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			common.SMTPPort = listener.Addr().(*net.TCPAddr).Port
			type result struct {
				message string
				err     error
			}
			captured := make(chan result, 1)
			go func() { message, err := receiveTicketTestMail(listener); captured <- result{message, err} }()
			subject := "工单 <literal> & subject"
			data := dto.NewNotify(tc.kind, subject, tc.content, tc.values)
			require.NoError(t, NotifyUser(918201, "recipient@example.invalid", dto.UserSetting{NotifyType: dto.NotifyTypeEmail}, data))
			var received result
			select {
			case received = <-captured:
			case <-time.After(5 * time.Second):
				t.Fatal("SMTP capture did not complete")
			}
			require.NoError(t, received.err)
			message, err := mail.ReadMessage(strings.NewReader(received.message))
			require.NoError(t, err)
			assert.Equal(t, "text/html; charset=UTF-8", message.Header.Get("Content-Type"))
			decodedSubject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
			require.NoError(t, err)
			assert.Equal(t, subject, decodedSubject)
			body, err := io.ReadAll(message.Body)
			require.NoError(t, err)
			assert.Equal(t, tc.want, strings.TrimSuffix(string(body), "\n"))
			assert.Equal(t, tc.content, data.Content, "sending must not mutate notification data")
		})
	}
}

func TestTicketNotificationNonEmailChannelsPreserveText(t *testing.T) {
	allowLocalTicketNotifications(t)
	oldClient, oldWorker := httpClient, system_setting.WorkerUrl
	fetch := system_setting.GetFetchSetting()
	oldProtection := fetch.EnableSSRFProtection
	fetch.EnableSSRFProtection, system_setting.WorkerUrl = false, ""
	t.Cleanup(func() {
		httpClient, system_setting.WorkerUrl = oldClient, oldWorker
		fetch.EnableSSRFProtection = oldProtection
	})
	type receivedNotice struct{ title, content string }
	received := make(chan receivedNotice, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bark" {
			received <- receivedNotice{r.URL.Query().Get("title"), r.URL.Query().Get("content")}
		} else {
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var payload struct {
				Title   string `json:"title"`
				Content string `json:"content"`
				Message string `json:"message"`
			}
			if !assert.NoError(t, common.Unmarshal(body, &payload)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.URL.Path == "/gotify/message" {
				payload.Content = payload.Message
			}
			received <- receivedNotice{payload.Title, payload.Content}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	httpClient = server.Client()
	for _, kind := range []string{dto.NotifyTypeWebhook, dto.NotifyTypeBark, dto.NotifyTypeGotify} {
		t.Run(kind, func(t *testing.T) {
			setting := dto.UserSetting{NotifyType: kind, WebhookUrl: server.URL + "/webhook", BarkUrl: server.URL + "/bark?title={{title}}&content={{content}}", GotifyUrl: server.URL + "/gotify", GotifyToken: "test-only-token"}
			data := dto.NewNotify(ticketNotifyType, "Ticket <title>", "Request: {{value}}", []any{`<a href="https://example.invalid">plain</a> & 中文`})
			require.NoError(t, NotifyUser(918202, "", setting, data))
			select {
			case notice := <-received:
				assert.Equal(t, data.Title, notice.title)
				assert.Equal(t, `Request: <a href="https://example.invalid">plain</a> & 中文`, notice.content)
			case <-time.After(5 * time.Second):
				t.Fatal("local notification was not received")
			}
		})
	}
}
