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
