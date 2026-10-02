package common

import (
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerificationCodeStoresOnlyDigestAndPreservesPurposeIsolation(t *testing.T) {
	const email, raw = "digest-verification@example.com", "private-reset-token"
	RegisterVerificationCodeWithKey(email, raw, PasswordResetPurpose)
	t.Cleanup(func() { DeleteKey(email, PasswordResetPurpose) })
	verificationMutex.Lock()
	stored := verificationMap[PasswordResetPurpose+email]
	verificationMutex.Unlock()
	assert.Equal(t, sha256.Sum256([]byte(raw)), stored.digest)
	assert.NotContains(t, string(stored.digest[:]), raw)
	assert.False(t, VerifyCodeWithKey(email, raw, EmailVerificationPurpose))
	assert.True(t, VerifyCodeWithKey(email, raw, PasswordResetPurpose))
	assert.True(t, ConsumeVerificationCodeWithKey(email, raw, PasswordResetPurpose))
	assert.False(t, ConsumeVerificationCodeWithKey(email, raw, PasswordResetPurpose))
}

func TestVerificationCodeFailedAttemptsAreBoundedAndResetOnlyByNewCode(t *testing.T) {
	const email, original = "bounded-verification@example.com", "first-code"
	RegisterVerificationCodeWithKey(email, original, EmailVerificationPurpose)
	t.Cleanup(func() { DeleteKey(email, EmailVerificationPurpose) })
	for range 4 {
		require.False(t, VerifyCodeWithKey(email, "wrong", EmailVerificationPurpose))
	}
	require.True(t, VerifyCodeWithKey(email, original, EmailVerificationPurpose), "four failures do not invalidate a correct code")
	require.False(t, VerifyCodeWithKey(email, "wrong", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey(email, original, EmailVerificationPurpose), "the fifth failure invalidates the code")
	RegisterVerificationCodeWithKey(email, "replacement-code", EmailVerificationPurpose)
	assert.True(t, VerifyCodeWithKey(email, "replacement-code", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey(email, original, EmailVerificationPurpose))
}

func TestVerificationCodeConcurrentFailuresCannotLoseAttemptCount(t *testing.T) {
	const email, raw = "concurrent-failure@example.com", "correct-code"
	RegisterVerificationCodeWithKey(email, raw, EmailVerificationPurpose)
	t.Cleanup(func() { DeleteKey(email, EmailVerificationPurpose) })
	start := make(chan struct{})
	results := make(chan bool, 5)
	var group sync.WaitGroup
	for range 5 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- VerifyCodeWithKey(email, "incorrect", EmailVerificationPurpose)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for accepted := range results {
		assert.False(t, accepted)
	}
	assert.False(t, VerifyCodeWithKey(email, raw, EmailVerificationPurpose))
}

func TestVerificationCodeConcurrentConsumptionHasOneWinner(t *testing.T) {
	const email, raw = "concurrent-consume@example.com", "one-reset-token"
	RegisterVerificationCodeWithKey(email, raw, PasswordResetPurpose)
	t.Cleanup(func() { DeleteKey(email, PasswordResetPurpose) })
	start := make(chan struct{})
	var group sync.WaitGroup
	var consumed atomic.Int32
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			if ConsumeVerificationCodeWithKey(email, raw, PasswordResetPurpose) {
				consumed.Add(1)
			}
		}()
	}
	close(start)
	group.Wait()
	assert.EqualValues(t, 1, consumed.Load())
	assert.False(t, VerifyCodeWithKey(email, raw, PasswordResetPurpose))
}

func TestVerificationCodeExpiredTokenCannotBeConsumed(t *testing.T) {
	const email, raw = "expired-verification@example.com", "expired-reset-token"
	RegisterVerificationCodeWithKey(email, raw, PasswordResetPurpose)
	t.Cleanup(func() { DeleteKey(email, PasswordResetPurpose) })
	verificationMutex.Lock()
	stored := verificationMap[PasswordResetPurpose+email]
	stored.time = time.Now().Add(-time.Duration(VerificationValidMinutes+1) * time.Minute)
	verificationMap[PasswordResetPurpose+email] = stored
	verificationMutex.Unlock()
	assert.False(t, ConsumeVerificationCodeWithKey(email, raw, PasswordResetPurpose))
	assert.False(t, VerifyCodeWithKey(email, raw, PasswordResetPurpose))
}
