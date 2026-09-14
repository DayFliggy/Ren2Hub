package service

import (
	"crypto/hmac"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const MidjourneyImageURLLifetime = 24 * time.Hour

func BuildMidjourneyImageURL(baseURL, mjID string) string {
	expiresAt := time.Now().Add(MidjourneyImageURLLifetime).Unix()
	signature := MidjourneyImageURLSignature(mjID, expiresAt)
	return strings.TrimRight(baseURL, "/") + "/mj/image/" + url.PathEscape(mjID) +
		"?exp=" + strconv.FormatInt(expiresAt, 10) + "&sig=" + url.QueryEscape(signature)
}

// Row capabilities use a separate signature domain so existing provider-ID
// capabilities cannot be reinterpreted as access to another local task row.
func BuildMidjourneyTaskImageURL(baseURL string, taskID int) string {
	identifier := strconv.Itoa(taskID)
	expiresAt := time.Now().Add(MidjourneyImageURLLifetime).Unix()
	signature := midjourneyTaskImageSignature(identifier, expiresAt)
	return strings.TrimRight(baseURL, "/") + "/mj/image/" + identifier +
		"?v=2&exp=" + strconv.FormatInt(expiresAt, 10) + "&sig=" + url.QueryEscape(signature)
}

func midjourneyTaskImageSignature(identifier string, expiresAt int64) string {
	return common.GenerateHMACWithKey([]byte("midjourney-image-v2:"+common.SessionSecret), fmt.Sprintf("%s:%d", identifier, expiresAt))
}

func ValidateMidjourneyTaskImageURL(identifier string, expiresAt int64, signature string, now time.Time) bool {
	id, err := strconv.Atoi(identifier)
	if err != nil || id <= 0 || expiresAt <= now.Unix() || signature == "" {
		return false
	}
	return hmac.Equal([]byte(midjourneyTaskImageSignature(identifier, expiresAt)), []byte(signature))
}

func MidjourneyImageURLSignature(mjID string, expiresAt int64) string {
	payload := fmt.Sprintf("%s:%d", mjID, expiresAt)
	return common.GenerateHMACWithKey([]byte("midjourney-image-v1:"+common.SessionSecret), payload)
}

func ValidateMidjourneyImageURL(mjID string, expiresAt int64, signature string, now time.Time) bool {
	if strings.TrimSpace(mjID) == "" || expiresAt <= now.Unix() || strings.TrimSpace(signature) == "" {
		return false
	}
	expected := MidjourneyImageURLSignature(mjID, expiresAt)
	return hmac.Equal([]byte(expected), []byte(signature))
}
