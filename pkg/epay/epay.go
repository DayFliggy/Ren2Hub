// Package epay implements the bounded, signed EasyPay payment protocol.
package epay

import (
	"crypto/md5" // EasyPay's protocol requires MD5(message + merchant key).
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	StatusTradeSuccess   = "TRADE_SUCCESS"
	StatusPending        = "WAIT_BUYER_PAY"
	MaxNotificationBytes = 16 << 10
	maxFields            = 32
	maxKeyBytes          = 64
	maxValueBytes        = 4 << 10
	maxIdentifierBytes   = 128
)

var (
	ErrInvalidParameters = errors.New("epay: invalid parameters")
	ErrInvalidSignature  = errors.New("epay: invalid signature")
	ErrInvalidMoney      = errors.New("epay: invalid amount")
	ErrInvalidConfig     = errors.New("epay: invalid merchant configuration")
)

type Config struct {
	GatewayURL string
	MerchantID string
	Key        string `json:"-"`
}

type Payment struct {
	OrderNo     string
	TradeNo     string
	MerchantID  string
	Method      string
	Status      string
	AmountMinor int64
}

type Purchase struct {
	OrderNo     string
	Method      string
	Name        string
	NotifyURL   string
	ReturnURL   string
	AmountMinor int64
}

func NormalizeGatewayURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Opaque != "" || strings.HasSuffix(u.Host, ":") {
		return "", ErrInvalidConfig
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return "", ErrInvalidConfig
		}
	}
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.ToLower(u.Hostname())
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	// A canonical base is also used to bind transaction IDs across key rotations.
	u.Path = path.Clean("/" + u.Path)
	switch path.Base(u.Path) {
	case "submit.php", "mapi.php", "api.php":
		u.Path = path.Dir(u.Path)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// Sign preserves the EasyPay convention: sort nonempty values, omit sign and
// sign_type, and hash the unescaped concatenation followed by the merchant key.
func Sign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if name != "sign" && name != "sign_type" && value != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	var message strings.Builder
	for i, name := range keys {
		if i != 0 {
			message.WriteByte('&')
		}
		message.WriteString(name)
		message.WriteByte('=')
		message.WriteString(params[name])
	}
	message.WriteString(key)
	digest := md5.Sum([]byte(message.String()))
	return hex.EncodeToString(digest[:])
}

func ParseNotification(request *http.Request) (map[string]string, error) {
	if request == nil || request.URL == nil || len(request.URL.RawQuery) > MaxNotificationBytes {
		return nil, ErrInvalidParameters
	}
	raw := request.URL.RawQuery
	switch request.Method {
	case http.MethodGet:
	case http.MethodPost:
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" || request.Body == nil {
			return nil, ErrInvalidParameters
		}
		// Include query parameters in the same bounded parse so mixed-location
		// duplicates cannot acquire different meanings in separate consumers.
		remaining := MaxNotificationBytes - len(raw)
		if raw != "" {
			remaining--
		}
		if remaining < 0 || request.ContentLength > int64(remaining) {
			return nil, ErrInvalidParameters
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, int64(remaining)+1))
		if err != nil || len(body) > remaining {
			return nil, ErrInvalidParameters
		}
		if raw != "" && len(body) != 0 {
			raw += "&"
		}
		raw += string(body)
	default:
		return nil, ErrInvalidParameters
	}
	if raw == "" || strings.Count(raw, "&")+1 > maxFields {
		return nil, ErrInvalidParameters
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, ErrInvalidParameters
	}
	params := make(map[string]string, len(values))
	for name, list := range values {
		if len(list) != 1 {
			return nil, ErrInvalidParameters
		}
		params[name] = list[0]
	}
	if err := validateNotificationParameters(params); err != nil {
		return nil, err
	}
	return params, nil
}

func validateNotificationParameters(params map[string]string) error {
	if len(params) > maxFields {
		return ErrInvalidParameters
	}
	total := 0
	for name, value := range params {
		if name == "" || len(name) > maxKeyBytes || len(value) > maxValueBytes || !utf8.ValidString(name) || !utf8.ValidString(value) || strings.ContainsAny(name+value, "\x00\r\n") {
			return ErrInvalidParameters
		}
		total += len(name) + len(value) + 2
	}
	if total-1 > MaxNotificationBytes {
		return ErrInvalidParameters
	}
	for _, name := range []string{"pid", "trade_no", "out_trade_no", "type", "money", "trade_status", "sign", "sign_type"} {
		value := params[name]
		if value == "" || value != strings.TrimSpace(value) {
			return ErrInvalidParameters
		}
	}
	for _, name := range []string{"pid", "trade_no", "out_trade_no", "type"} {
		if !validIdentifier(params[name]) {
			return ErrInvalidParameters
		}
	}
	if params["sign_type"] != "MD5" || (params["trade_status"] != StatusTradeSuccess && params["trade_status"] != StatusPending && params["trade_status"] != "TRADE_CLOSED") {
		return ErrInvalidParameters
	}
	return nil
}

func VerifyNotification(params map[string]string, config Config) (*Payment, error) {
	if !validIdentifier(config.MerchantID) || config.Key == "" {
		return nil, ErrInvalidConfig
	}
	if err := validateNotificationParameters(params); err != nil {
		return nil, err
	}
	signature, err := hex.DecodeString(params["sign"])
	if err != nil || len(signature) != md5.Size {
		return nil, ErrInvalidSignature
	}
	expected, _ := hex.DecodeString(Sign(params, config.Key))
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return nil, ErrInvalidSignature
	}
	if params["pid"] != config.MerchantID {
		return nil, ErrInvalidParameters
	}
	amount, err := ParseMoney(params["money"])
	if err != nil {
		return nil, err
	}
	return &Payment{
		OrderNo: params["out_trade_no"], TradeNo: params["trade_no"], MerchantID: params["pid"],
		Method: params["type"], Status: params["trade_status"], AmountMinor: amount,
	}, nil
}

// ParseMoney accepts positive fixed-point yuan amounts with exact cent precision.
// Extra fractional zeros are harmless; sub-cent values must never be rounded.
func ParseMoney(raw string) (int64, error) {
	whole, fraction, hasDot := strings.Cut(raw, ".")
	if whole == "" || len(raw) > maxValueBytes || (hasDot && fraction == "") {
		return 0, ErrInvalidMoney
	}
	for _, part := range []string{whole, fraction} {
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, ErrInvalidMoney
			}
		}
	}
	if len(fraction) > 2 {
		if strings.Trim(fraction[2:], "0") != "" {
			return 0, ErrInvalidMoney
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
	yuan, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || yuan > (math.MaxInt64-cents)/100 {
		return 0, ErrInvalidMoney
	}
	amount := yuan*100 + cents
	if amount <= 0 {
		return 0, ErrInvalidMoney
	}
	return amount, nil
}

func FormatMoney(amount int64) string {
	// String splitting handles MinInt64 without overflowing its absolute value.
	raw := strconv.FormatInt(amount, 10)
	prefix := ""
	if raw[0] == '-' {
		prefix, raw = "-", raw[1:]
	}
	for len(raw) < 3 {
		raw = "0" + raw
	}
	return prefix + raw[:len(raw)-2] + "." + raw[len(raw)-2:]
}

func BuildPurchase(config Config, purchase Purchase) (string, map[string]string, error) {
	base, err := NormalizeGatewayURL(config.GatewayURL)
	if err != nil || !validIdentifier(config.MerchantID) || config.Key == "" {
		return "", nil, ErrInvalidConfig
	}
	if purchase.AmountMinor <= 0 || !validIdentifier(purchase.OrderNo) || !validIdentifier(purchase.Method) || purchase.Name == "" || len(purchase.Name) > maxValueBytes {
		return "", nil, ErrInvalidParameters
	}
	for _, raw := range []string{purchase.NotifyURL, purchase.ReturnURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return "", nil, ErrInvalidParameters
		}
	}
	params := map[string]string{
		"pid": config.MerchantID, "type": purchase.Method, "out_trade_no": purchase.OrderNo,
		"notify_url": purchase.NotifyURL, "return_url": purchase.ReturnURL, "name": purchase.Name,
		"money": FormatMoney(purchase.AmountMinor), "device": "pc", "sign_type": "MD5",
	}
	params["sign"] = Sign(params, config.Key)
	return base + "/submit.php", params, nil
}

// Identifiers must fit the payment record on every supported database.
func validIdentifier(value string) bool {
	return value != "" && len(value) <= maxIdentifierBytes && value == strings.TrimSpace(value) && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}
