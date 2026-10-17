// Package gmpay implements the bounded, signed GMPay merchant protocol used by
// the self-hosted epusdt multi-chain payment gateway.
//
// This is a sibling of pkg/epay rather than an extension of it: GMPay signs with
// HMAC-SHA256 over a "signature" field and returns a hosted checkout URL, while
// EasyPay signs with MD5 over a "sign" field and expects the browser to POST a
// form to submit.php. The two protocols share no wire format.
package gmpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

const (
	// StatusWaitPay and friends mirror the gateway order status values.
	StatusWaitPay    = 1
	StatusPaySuccess = 2
	StatusExpired    = 3
	StatusWaitSelect = 4

	// MaxNotificationBytes bounds the raw callback body. The callback is
	// unauthenticated input, so it is parsed under the same kind of ceiling the
	// EasyPay notification parser applies.
	MaxNotificationBytes = 16 << 10
	maxFields            = 32
	maxKeyBytes          = 64
	maxValueBytes        = 4 << 10
	maxIdentifierBytes   = 128
	// MaxTokenDecimals matches the gateway system.amount_precision ceiling.
	MaxTokenDecimals = 6

	createOrderPath  = "/payments/gmpay/v1/order/create-transaction"
	publicConfigPath = "/payments/gmpay/v1/config"
)

var (
	ErrInvalidParameters = errors.New("gmpay: invalid parameters")
	ErrInvalidSignature  = errors.New("gmpay: invalid signature")
	ErrInvalidConfig     = errors.New("gmpay: invalid merchant configuration")
	ErrGatewayRejected   = errors.New("gmpay: gateway rejected the request")
)

// Config carries the merchant credential. Key is never serialized.
type Config struct {
	GatewayURL string
	MerchantID string
	Key        string `json:"-"`
}

// CreateOrder is the outbound order request. Amount is a decimal string, never
// a float: the gateway re-formats JSON numbers through float64, which silently
// rewrites "100.00" as "100" and breaks the signature.
type CreateOrder struct {
	OrderNo   string
	Currency  string
	Token     string
	Network   string
	Amount    string
	NotifyURL string
	ReturnURL string
	Name      string
}

// CreatedOrder is the gateway order the gateway accepted.
type CreatedOrder struct {
	TradeNo        string
	OrderNo        string
	Amount         string
	Currency       string
	ActualAmount   string
	ReceiveAddress string
	Token          string
	Status         int
	ExpirationTime int64
	PaymentURL     string
}

// Notification is a verified payment callback. The gateway callback carries no
// network and no currency, so neither can be confirmed from it.
type Notification struct {
	PID                string
	TradeNo            string
	OrderNo            string
	Token              string
	ReceiveAddress     string
	BlockTransactionID string
	Amount             string
	ActualAmount       string
	Status             int
}

// Canonical renders the signed parameter string: non-empty name/value pairs
// sorted as complete pairs, joined with "&". Excluded: the signature field itself and any
// empty value. Names are case-sensitive. Callers supply already-formatted
// value strings, because the signature covers their exact spelling.
func Canonical(params map[string]string) string {
	pairs := make([]string, 0, len(params))
	for name, value := range params {
		if name == "signature" || value == "" {
			continue
		}
		pairs = append(pairs, name+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// Sign returns the lowercase hexadecimal HMAC-SHA256 of the canonical string.
func Sign(params map[string]string, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidConfig
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(Canonical(params)))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// NormalizeGatewayURL validates an HTTPS base address and strips any known
// endpoint suffix so the base can be rebuilt deterministically.
func NormalizeGatewayURL(raw string) (string, error) {
	u, err := parseHTTPSURL(raw)
	if err != nil || u.RawQuery != "" || u.ForceQuery {
		return "", ErrInvalidConfig
	}
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.ToLower(u.Hostname())
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{createOrderPath, publicConfigPath} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
		}
	}
	u.Path = strings.TrimRight(path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// CreateOrderEndpoint returns the absolute create-transaction endpoint.
func CreateOrderEndpoint(config Config) (string, error) {
	base, err := NormalizeGatewayURL(config.GatewayURL)
	if err != nil {
		return "", err
	}
	return base + createOrderPath, nil
}

// PublicConfigEndpoint returns the absolute public-config endpoint, used by the
// admin credential probe.
func PublicConfigEndpoint(config Config) (string, error) {
	base, err := NormalizeGatewayURL(config.GatewayURL)
	if err != nil {
		return "", err
	}
	return base + publicConfigPath, nil
}

// ValidatePaymentURL accepts only a well-formed HTTPS checkout URL. The value is
// handed to the browser, so anything malformed must never be stored or returned.
func ValidatePaymentURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if _, err := parseHTTPSURL(value); err != nil {
		return "", ErrInvalidParameters
	}
	return value, nil
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") || u.Opaque != "" || strings.HasSuffix(u.Host, ":") {
		return nil, ErrInvalidParameters
	}
	if strings.Contains(u.Hostname(), ":") || strings.ContainsAny(u.Host, "[]") {
		if !strings.HasPrefix(u.Host, "[") || net.ParseIP(u.Hostname()) == nil {
			return nil, ErrInvalidParameters
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return nil, ErrInvalidParameters
		}
	}
	return u, nil
}

// ParseTokenAmount parses a positive crypto quantity with at most
// MaxTokenDecimals fractional digits. Never use it for fiat cents: the two
// sides have different precision contracts.
func ParseTokenAmount(raw string) (decimal.Decimal, error) {
	value := raw
	if value == "" || len(value) > maxValueBytes || value != strings.TrimSpace(value) {
		return decimal.Zero, ErrInvalidParameters
	}
	whole, fraction, hasDot := strings.Cut(value, ".")
	if whole == "" || (hasDot && fraction == "") {
		return decimal.Zero, ErrInvalidParameters
	}
	for _, part := range []string{whole, fraction} {
		for _, char := range part {
			if char < '0' || char > '9' {
				return decimal.Zero, ErrInvalidParameters
			}
		}
	}
	if len(fraction) > MaxTokenDecimals {
		return decimal.Zero, ErrInvalidParameters
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || !amount.IsPositive() {
		return decimal.Zero, ErrInvalidParameters
	}
	return amount, nil
}

// FormatFiatAmount renders a positive fiat amount with exactly two decimals from
// an integer number of cents.
func FormatFiatAmount(cents int64) string {
	return decimal.NewFromInt(cents).Div(decimal.NewFromInt(100)).StringFixed(2)
}

// ParseFiatAmount parses a positive fiat amount into whole cents. Trailing
// fractional zeros are harmless; a sub-cent value is rejected rather than
// rounded, because the settlement comparison must be exact. This is the fiat
// counterpart to ParseTokenAmount and the two must not be interchanged.
func ParseFiatAmount(raw string) (int64, error) {
	value := raw
	if value == "" || len(value) > maxValueBytes || value != strings.TrimSpace(value) {
		return 0, ErrInvalidParameters
	}
	whole, fraction, hasDot := strings.Cut(value, ".")
	if whole == "" || (hasDot && fraction == "") {
		return 0, ErrInvalidParameters
	}
	for _, part := range []string{whole, fraction} {
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, ErrInvalidParameters
			}
		}
	}
	if len(fraction) > 2 {
		if strings.Trim(fraction[2:], "0") != "" {
			return 0, ErrInvalidParameters
		}
		fraction = fraction[:2]
	}
	var cents int64
	if len(fraction) > 0 {
		cents = int64(fraction[0]-'0') * 10
	}
	if len(fraction) > 1 {
		cents += int64(fraction[1] - '0')
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || units > (math.MaxInt64-cents)/100 {
		return 0, ErrInvalidParameters
	}
	amount := units*100 + cents
	if amount <= 0 {
		return 0, ErrInvalidParameters
	}
	return amount, nil
}

// BuildCreateOrder renders the signed request body as a form. Form encoding is
// deliberate: the gateway parses JSON numbers through float64 and would
// re-format the amount before signing, so a literal such as "100.00" would not
// verify.
func BuildCreateOrder(config Config, order CreateOrder) (string, url.Values, error) {
	endpoint, err := CreateOrderEndpoint(config)
	if err != nil {
		return "", nil, err
	}
	if config.Key == "" || !validIdentifier(config.MerchantID) || !validIdentifier(order.OrderNo) || len(order.OrderNo) > 32 {
		return "", nil, ErrInvalidConfig
	}
	if order.Amount == "" || len(order.Amount) > 64 || order.Name == "" || len(order.Name) > maxValueBytes {
		return "", nil, ErrInvalidParameters
	}
	amountMinor, err := ParseFiatAmount(order.Amount)
	if err != nil || amountMinor <= 1 {
		return "", nil, ErrInvalidParameters
	}
	// The gateway stores fiat amounts as float64. Reject a quote whose cents
	// would change before the gateway signs its response or notification.
	gatewayAmount, err := strconv.ParseFloat(order.Amount, 64)
	if err != nil {
		return "", nil, ErrInvalidParameters
	}
	roundTripMinor, err := ParseFiatAmount(strconv.FormatFloat(gatewayAmount, 'f', -1, 64))
	if err != nil || roundTripMinor != amountMinor {
		return "", nil, ErrInvalidParameters
	}
	if !validIdentifier(order.Token) || !validIdentifier(order.Network) || !validIdentifier(order.Currency) {
		return "", nil, ErrInvalidParameters
	}
	if order.NotifyURL == "" {
		return "", nil, ErrInvalidParameters
	}
	for _, raw := range []string{order.NotifyURL, order.ReturnURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return "", nil, ErrInvalidParameters
		}
	}
	params := map[string]string{
		"pid": config.MerchantID, "order_id": order.OrderNo, "currency": order.Currency,
		"token": order.Token, "network": order.Network, "amount": order.Amount,
		"notify_url": order.NotifyURL, "name": order.Name,
	}
	if order.ReturnURL != "" {
		params["redirect_url"] = order.ReturnURL
	}
	signature, err := Sign(params, config.Key)
	if err != nil {
		return "", nil, err
	}
	params["signature"] = signature
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, value)
	}
	return endpoint, values, nil
}

// ParseCreatedOrder decodes the creation envelope and its order payload.
func ParseCreatedOrder(body []byte) (*CreatedOrder, error) {
	if len(body) == 0 || len(body) > MaxNotificationBytes {
		return nil, ErrInvalidParameters
	}
	var env struct {
		StatusCode int               `json:"status_code"`
		Message    string            `json:"message"`
		Data       *createdOrderWire `json:"data"`
	}
	if err := common.Unmarshal(body, &env); err != nil || !hasUniqueJSONFields(gjson.ParseBytes(body), 0) {
		return nil, ErrInvalidParameters
	}
	if env.StatusCode != http.StatusOK {
		// Only known pre-creation rejections establish that no order was made.
		// A system error (400), an existing order (10002), or a new unknown
		// status requires reconciliation of this non-idempotent request.
		switch env.StatusCode {
		case 401, 10003, 10004, 10005, 10006, 10009, 10014, 10016, 10041:
			return nil, ErrGatewayRejected
		default:
			return nil, ErrInvalidParameters
		}
	}
	if env.Data == nil {
		return nil, ErrInvalidParameters
	}
	wire := env.Data
	if !validIdentifier(wire.TradeNo) || !validIdentifier(wire.OrderNo) {
		return nil, ErrInvalidParameters
	}
	if wire.Status != StatusWaitPay && wire.Status != StatusPaySuccess && wire.Status != StatusWaitSelect {
		return nil, ErrInvalidParameters
	}
	paymentURL, err := ValidatePaymentURL(wire.PaymentURL)
	if err != nil {
		return nil, err
	}
	return &CreatedOrder{
		TradeNo: wire.TradeNo, OrderNo: wire.OrderNo, Amount: string(wire.Amount),
		Currency: wire.Currency, ActualAmount: string(wire.ActualAmount),
		ReceiveAddress: wire.ReceiveAddress, Token: wire.Token,
		Status: wire.Status, ExpirationTime: wire.ExpirationTime, PaymentURL: paymentURL,
	}, nil
}

// createdOrderWire keeps every gateway amount as exact decimal text. The
// gateway emits JSON numbers, including exponent notation for large values;
// decoding must not lose precision through float64.
type createdOrderWire struct {
	TradeNo        string        `json:"trade_id"`
	OrderNo        string        `json:"order_id"`
	Amount         literalString `json:"amount"`
	Currency       string        `json:"currency"`
	ActualAmount   literalString `json:"actual_amount"`
	ReceiveAddress string        `json:"receive_address"`
	Token          string        `json:"token"`
	Status         int           `json:"status"`
	ExpirationTime int64         `json:"expiration_time"`
	PaymentURL     string        `json:"payment_url"`
}

// literalString accepts a quoted string or a bare number. Numeric exponents
// expand to decimal notation without rounding; strings retain their spelling.
type literalString string

// UnmarshalJSON copies the raw token text, stripping quotes when the value is a
// JSON string. An empty or null value becomes the empty string.
func (value *literalString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch {
	case trimmed == "" || trimmed == "null":
		*value = ""
		return nil
	case len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"':
		var decoded string
		if err := common.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*value = literalString(decoded)
		return nil
	default:
		var number json.Number
		if err := common.Unmarshal(data, &number); err != nil {
			return ErrInvalidParameters
		}
		if strings.ContainsAny(trimmed, "eE") {
			amount, err := decimal.NewFromString(trimmed)
			// Bound expansion before allocating a decimal string from remote input.
			if err != nil || amount.Exponent() > maxValueBytes || amount.Exponent() < -maxValueBytes {
				return ErrInvalidParameters
			}
			trimmed = amount.String()
			if len(trimmed) > maxValueBytes {
				return ErrInvalidParameters
			}
		}
		*value = literalString(trimmed)
		return nil
	}
}

// ParseNotification reads the bounded JSON callback body exactly once.
func ParseNotification(request *http.Request) (map[string]string, error) {
	if request == nil || request.Method != http.MethodPost || request.Body == nil {
		return nil, ErrInvalidParameters
	}
	mediaType := request.Header.Get("Content-Type")
	if mediaType != "" {
		parsedType, _, err := mime.ParseMediaType(mediaType)
		if err != nil || parsedType != "application/json" {
			return nil, ErrInvalidParameters
		}
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, MaxNotificationBytes+1))
	if err != nil || len(body) == 0 || len(body) > MaxNotificationBytes {
		return nil, ErrInvalidParameters
	}
	return parseNotificationBody(body)
}

// parseNotificationBody decodes the callback into flat strings. Unknown fields
// are retained so an extension field still participates in the signature.
func parseNotificationBody(body []byte) (map[string]string, error) {
	var raw map[string]any
	if err := common.Unmarshal(body, &raw); err != nil || raw == nil || !hasUniqueJSONFields(gjson.ParseBytes(body), 0) {
		return nil, ErrInvalidParameters
	}
	if len(raw) > maxFields {
		return nil, ErrInvalidParameters
	}
	params := make(map[string]string, len(raw))
	for name, value := range raw {
		if name == "" || len(name) > maxKeyBytes || !utf8.ValidString(name) {
			return nil, ErrInvalidParameters
		}
		text, err := scalarToString(value)
		if err != nil {
			return nil, err
		}
		if len(text) > maxValueBytes || !utf8.ValidString(text) || strings.ContainsAny(name+text, "\x00\r\n") {
			return nil, ErrInvalidParameters
		}
		params[name] = text
	}
	return params, nil
}

// hasUniqueJSONFields rejects ambiguous objects before signed values or gateway
// outcomes are consumed. Decode uses case-insensitive struct matching, so case
// variants and escaped spellings must not supply a second value for one field.
func hasUniqueJSONFields(value gjson.Result, depth int) bool {
	if depth > 32 {
		return false
	}
	if !value.IsObject() && !value.IsArray() {
		return true
	}
	seen := make(map[string]bool)
	valid := true
	value.ForEach(func(key, child gjson.Result) bool {
		if value.IsObject() {
			name := strings.ToLower(key.Str)
			if seen[name] {
				valid = false
				return false
			}
			seen[name] = true
		}
		valid = hasUniqueJSONFields(child, depth+1)
		return valid
	})
	return valid
}

// scalarToString mirrors the gateway value rendering so both sides sign the
// same bytes: JSON strings keep their spelling, numbers are formatted the way
// Go writes float64, and null becomes empty (and is therefore excluded).
func scalarToString(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case nil:
		return "", nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", ErrInvalidParameters
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	default:
		return "", ErrInvalidParameters
	}
}

// VerifyNotification validates the signature and the required claims, returning
// only after every check passes.
func VerifyNotification(params map[string]string, config Config) (*Notification, error) {
	if !validIdentifier(config.MerchantID) || config.Key == "" {
		return nil, ErrInvalidConfig
	}
	if len(params) > maxFields {
		return nil, ErrInvalidParameters
	}
	signature, err := hex.DecodeString(params["signature"])
	if err != nil || len(signature) != sha256.Size {
		return nil, ErrInvalidSignature
	}
	expected, err := Sign(params, config.Key)
	if err != nil {
		return nil, err
	}
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil || subtle.ConstantTimeCompare(signature, expectedBytes) != 1 {
		return nil, ErrInvalidSignature
	}
	for _, name := range []string{"pid", "trade_id", "order_id", "token"} {
		if !validIdentifier(params[name]) {
			return nil, ErrInvalidParameters
		}
	}
	if params["pid"] != config.MerchantID {
		return nil, ErrInvalidParameters
	}
	transaction := params["block_transaction_id"]
	if transaction == "" || len(transaction) > maxIdentifierBytes {
		return nil, ErrInvalidParameters
	}
	status, err := strconv.Atoi(params["status"])
	if err != nil || (status != StatusWaitPay && status != StatusPaySuccess && status != StatusExpired) {
		return nil, ErrInvalidParameters
	}
	if status == StatusPaySuccess {
		if _, err := ParseFiatAmount(params["amount"]); err != nil {
			return nil, ErrInvalidParameters
		}
		if _, err := ParseTokenAmount(params["actual_amount"]); err != nil {
			return nil, ErrInvalidParameters
		}
		if !validIdentifier(params["receive_address"]) {
			return nil, ErrInvalidParameters
		}
	}
	return &Notification{
		PID: params["pid"], TradeNo: params["trade_id"], OrderNo: params["order_id"],
		Token: params["token"], ReceiveAddress: params["receive_address"], BlockTransactionID: transaction,
		Amount: params["amount"], ActualAmount: params["actual_amount"], Status: status,
	}, nil
}

// validIdentifier rejects empty, over-long, padded, or control-bearing values.
func validIdentifier(value string) bool {
	return value != "" && len(value) <= maxIdentifierBytes && value == strings.TrimSpace(value) && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

// maxSupportedNetworks and maxSupportedTokens bound the public-config catalog.
// The probe response is remote input, so it is parsed under the same kind of
// ceiling as every other gateway payload.
const (
	maxSupportedNetworks = 64
	maxSupportedTokens   = 64
)

// SupportedAsset is one enabled network and the token symbols the gateway can
// collect on it. The admin asset catalog is built by picking pairs from here.
type SupportedAsset struct {
	Network     string   `json:"network"`
	DisplayName string   `json:"display_name"`
	Tokens      []string `json:"tokens"`
}

// ParsePublicConfig reads the supported_assets list from the gateway's public
// configuration. The endpoint is unauthenticated, so a success here proves
// reachability and the asset catalog, not the merchant credential.
func ParsePublicConfig(body []byte) ([]SupportedAsset, error) {
	if len(body) == 0 || len(body) > MaxNotificationBytes {
		return nil, ErrInvalidParameters
	}
	var env struct {
		StatusCode int `json:"status_code"`
		Data       *struct {
			SupportedAssets []struct {
				Network     string   `json:"network"`
				DisplayName string   `json:"display_name"`
				Tokens      []string `json:"tokens"`
			} `json:"supported_assets"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &env); err != nil || !hasUniqueJSONFields(gjson.ParseBytes(body), 0) {
		return nil, ErrInvalidParameters
	}
	if env.StatusCode != http.StatusOK || env.Data == nil {
		return nil, ErrGatewayRejected
	}
	if env.Data.SupportedAssets == nil || len(env.Data.SupportedAssets) > maxSupportedNetworks {
		return nil, ErrInvalidParameters
	}
	assets := make([]SupportedAsset, 0, len(env.Data.SupportedAssets))
	for _, raw := range env.Data.SupportedAssets {
		network := strings.ToLower(strings.TrimSpace(raw.Network))
		if !validIdentifier(network) || len(raw.Tokens) > maxSupportedTokens {
			return nil, ErrInvalidParameters
		}
		tokens := make([]string, 0, len(raw.Tokens))
		for _, token := range raw.Tokens {
			symbol := strings.ToLower(strings.TrimSpace(token))
			if !validIdentifier(symbol) {
				return nil, ErrInvalidParameters
			}
			tokens = append(tokens, symbol)
		}
		if len(tokens) == 0 {
			continue
		}
		display := strings.TrimSpace(raw.DisplayName)
		if display == "" || len(display) > maxIdentifierBytes {
			display = network
		}
		assets = append(assets, SupportedAsset{Network: network, DisplayName: display, Tokens: tokens})
	}
	return assets, nil
}
