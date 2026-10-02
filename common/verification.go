package common

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	digest         [sha256.Size]byte
	time           time.Time
	failedAttempts int
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
	verificationAttemptLimit = 5
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		digest: sha256.Sum256([]byte(code)),
		time:   time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	return verifyCodeWithKey(key, code, purpose, false)
}

// ConsumeVerificationCodeWithKey claims a reset token before any account write.
// A failed account operation must not restore an already consumed token.
func ConsumeVerificationCodeWithKey(key string, code string, purpose string) bool {
	return verifyCodeWithKey(key, code, purpose, true)
}

func verifyCodeWithKey(key string, code string, purpose string, consume bool) bool {
	digest := sha256.Sum256([]byte(code))
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	entryKey := purpose + key
	value, okay := verificationMap[entryKey]
	if !okay {
		return false
	}
	if time.Since(value.time) >= time.Duration(VerificationValidMinutes)*time.Minute || value.failedAttempts >= verificationAttemptLimit {
		delete(verificationMap, entryKey)
		return false
	}
	if subtle.ConstantTimeCompare(digest[:], value.digest[:]) != 1 {
		value.failedAttempts++
		if value.failedAttempts >= verificationAttemptLimit {
			delete(verificationMap, entryKey)
		} else {
			verificationMap[entryKey] = value
		}
		return false
	}
	if consume {
		delete(verificationMap, entryKey)
	}
	return true
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
