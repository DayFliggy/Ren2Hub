package epay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const maxQueryResponseBytes = 1 << 20

var (
	ErrQueryFailed     = errors.New("epay: query request failed")
	ErrInvalidResponse = errors.New("epay: invalid query response")
	ErrGatewayRejected = errors.New("epay: gateway rejected query")
)

type Client struct{ httpClient *http.Client }

// NewClient copies the client so TLS test transports can be injected without
// weakening the timeout or redirect boundary, or mutating a shared client.
func NewClient(client *http.Client) *Client {
	var bounded http.Client
	if client != nil {
		bounded = *client
	}
	bounded.Timeout = 10 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: &bounded}
}

var defaultClient = NewClient(nil)

func QueryOrder(ctx context.Context, config Config, orderNo string) (*Payment, error) {
	return defaultClient.QueryOrder(ctx, config, orderNo)
}

func (client *Client) QueryOrder(ctx context.Context, config Config, orderNo string) (*Payment, error) {
	base, err := NormalizeGatewayURL(config.GatewayURL)
	if err != nil || !validIdentifier(config.MerchantID) || config.Key == "" {
		return nil, ErrInvalidConfig
	}
	if !validIdentifier(orderNo) {
		return nil, ErrInvalidParameters
	}
	params := url.Values{"act": {"order"}, "pid": {config.MerchantID}, "key": {config.Key}, "out_trade_no": {orderNo}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api.php", strings.NewReader(params.Encode()))
	if err != nil {
		return nil, ErrQueryFailed
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	transport := defaultClient.httpClient
	if client != nil && client.httpClient != nil {
		transport = client.httpClient
	}
	response, err := transport.Do(request)
	if err != nil {
		// Transport errors can embed request URLs, headers or credential-bearing
		// provider messages. Only retain safe cancellation classifications.
		if errors.Is(err, context.Canceled) {
			return nil, errors.Join(ErrQueryFailed, context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.Join(ErrQueryFailed, context.DeadlineExceeded)
		}
		return nil, ErrQueryFailed
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrQueryFailed
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxQueryResponseBytes+1))
	if err != nil {
		return nil, ErrQueryFailed
	}
	if len(body) > maxQueryResponseBytes {
		return nil, ErrInvalidResponse
	}
	return parseQueryResponse(body, config.MerchantID, orderNo)
}

func parseQueryResponse(body []byte, merchantID, orderNo string) (*Payment, error) {
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, ErrInvalidResponse
	}
	code, err := queryScalar(envelope["code"])
	if err != nil {
		return nil, ErrInvalidResponse
	}
	if code != "1" {
		return nil, ErrGatewayRejected
	}
	// api.php uses top-level fields and status 0/1. Alternate wrappers and
	// callback-style status fields require a separate explicit protocol adapter.
	if _, exists := envelope["data"]; exists {
		return nil, ErrInvalidResponse
	}
	if _, exists := envelope["trade_status"]; exists {
		return nil, ErrInvalidResponse
	}
	fields := make(map[string]string)
	for _, name := range []string{"pid", "out_trade_no", "trade_no", "type", "money", "status"} {
		raw, exists := envelope[name]
		if !exists {
			return nil, ErrInvalidResponse
		}
		value, err := queryScalar(raw)
		if err != nil || value == "" || len(value) > maxValueBytes || value != strings.TrimSpace(value) || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
			return nil, ErrInvalidResponse
		}
		fields[name] = value
	}
	for _, name := range []string{"pid", "out_trade_no", "trade_no", "type"} {
		if !validIdentifier(fields[name]) {
			return nil, ErrInvalidResponse
		}
	}
	if fields["pid"] != merchantID || fields["out_trade_no"] != orderNo {
		return nil, ErrInvalidResponse
	}
	amount, err := ParseMoney(fields["money"])
	if err != nil {
		return nil, ErrInvalidResponse
	}
	status := StatusPending
	switch fields["status"] {
	case "0":
	case "1":
		status = StatusTradeSuccess
	default:
		return nil, ErrInvalidResponse
	}
	return &Payment{OrderNo: fields["out_trade_no"], TradeNo: fields["trade_no"], MerchantID: fields["pid"], Method: fields["type"], Status: status, AmountMinor: amount}, nil
}

func queryScalar(raw json.RawMessage) (string, error) {
	switch common.GetJsonType(raw) {
	case "string":
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return "", ErrInvalidResponse
		}
		return value, nil
	case "number":
		var value json.Number
		if err := common.Unmarshal(raw, &value); err != nil {
			return "", ErrInvalidResponse
		}
		return value.String(), nil
	default:
		return "", ErrInvalidResponse
	}
}
