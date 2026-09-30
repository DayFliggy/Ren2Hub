package billing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"golang.org/x/crypto/hkdf"
)

const evaluationEvidenceVersion = 1

var ErrEvaluationEvidence = errors.New("billing evaluation evidence is unavailable or invalid")

type EvidenceBinding struct {
	RequestID  string `json:"request_id"`
	AttemptID  string `json:"attempt_id"`
	ConfigHash string `json:"config_hash"`
	Phase      string `json:"phase"`
}

// EncryptedEvaluation never contains clear request parameters or headers.
// Callers must derive expected AAD from the recovery/attempt they are settling.
type EncryptedEvaluation struct {
	Version    int    `json:"version"`
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type evaluationEvidence struct {
	QuoteHash string                        `json:"quote_hash"`
	UsageHash string                        `json:"usage_hash"`
	Input     billingexpr.RequestInput      `json:"input"`
	Calls     []billingexpr.EnvironmentCall `json:"calls"`
}

type EvidenceCipher struct {
	aead  cipher.AEAD
	keyID string
}

// The host must ensure secret is explicitly configured and stable across
// restarts. It must not pass a randomly generated session-secret fallback.
func NewEvidenceCipher(secret []byte) (*EvidenceCipher, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("billing evidence requires a persistent crypto secret")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, nil, []byte("Ren2Hub/billing-evaluation-evidence/v1")), key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(key)
	return &EvidenceCipher{aead: aead, keyID: hex.EncodeToString(fingerprint[:16])}, nil
}

func (c *EvidenceCipher) KeyID() string {
	if c == nil {
		return ""
	}
	return c.keyID
}

func PricingConfigHash(quote *Quote) string {
	if quote == nil {
		return ""
	}
	hash, err := Hash(quote.PriceVersion)
	if err != nil {
		return ""
	}
	return hash
}

func evidenceAAD(quote *Quote, binding EvidenceBinding) ([]byte, error) {
	if quote == nil || binding.RequestID == "" || binding.AttemptID == "" || binding.ConfigHash != PricingConfigHash(quote) || strings.TrimSpace(binding.Phase) == "" {
		return nil, ErrEvaluationEvidence
	}
	return common.Marshal(struct {
		Version int             `json:"version"`
		Binding EvidenceBinding `json:"binding"`
	}{evaluationEvidenceVersion, binding})
}

func usageEvidenceHash(usage Usage) (string, error) {
	usage.Evidence = nil
	usage.requestInput = nil
	usage.environmentCalls = nil
	return Hash(usage)
}

// Evaluate computes once with real inputs and persists a replayable encrypted
// environment. The returned Usage must accompany the charge into the ledger.
func (c *EvidenceCipher) Evaluate(quote *Quote, usage Usage, input billingexpr.RequestInput, binding EvidenceBinding) (Usage, *ChargeResult, error) {
	if c == nil {
		return usage, nil, ErrEvaluationEvidence
	}
	aad, err := evidenceAAD(quote, binding)
	if err != nil {
		return usage, nil, err
	}
	if err = ValidateQuote(quote); err != nil {
		return usage, nil, err
	}
	frozen, err := billingexpr.CloneRequestInput(input)
	if err != nil {
		return usage, nil, ErrEvaluationEvidence
	}
	evaluation := billingexpr.NewEvaluation()
	if input.Evaluation != nil {
		evaluation.Now = input.Evaluation.Now
	}
	frozen.Evaluation = evaluation
	usage.Evidence = nil
	usage.requestInput = nil
	usage.environmentCalls = nil
	calculationUsage := usage
	calculationUsage.requestInput = &frozen
	charge, calculationErr := Calculate(quote, calculationUsage)
	hash, err := usageEvidenceHash(usage)
	if err != nil {
		return usage, nil, ErrEvaluationEvidence
	}
	evidence := evaluationEvidence{QuoteHash: quote.Hash, UsageHash: hash, Input: frozen, Calls: evaluation.Calls}
	plaintext, err := common.Marshal(evidence)
	if err != nil {
		return usage, nil, ErrEvaluationEvidence
	}
	defer clear(plaintext)
	nonce := make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return usage, nil, ErrEvaluationEvidence
	}
	usage.Evidence = &EncryptedEvaluation{Version: evaluationEvidenceVersion, KeyID: c.keyID, Nonce: nonce, Ciphertext: c.aead.Seal(nil, nonce, plaintext, aad)}
	return usage, charge, calculationErr
}

// Replay recalculates the entire expression and charge. Only environment reads
// are replayed; no stored amount is accepted as a substitute for computation.
func (c *EvidenceCipher) Replay(quote *Quote, usage Usage, binding EvidenceBinding) (*ChargeResult, error) {
	if c == nil || usage.Evidence == nil {
		return nil, ErrEvaluationEvidence
	}
	envelope := usage.Evidence
	if envelope.Version != evaluationEvidenceVersion || envelope.KeyID != c.keyID || len(envelope.Nonce) != c.aead.NonceSize() {
		return nil, ErrEvaluationEvidence
	}
	aad, err := evidenceAAD(quote, binding)
	if err != nil {
		return nil, ErrEvaluationEvidence
	}
	plaintext, err := c.aead.Open(nil, envelope.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return nil, ErrEvaluationEvidence
	}
	defer clear(plaintext)
	var evidence evaluationEvidence
	if err = common.Unmarshal(plaintext, &evidence); err != nil {
		return nil, ErrEvaluationEvidence
	}
	hash, err := usageEvidenceHash(usage)
	if err != nil || hash != evidence.UsageHash || evidence.QuoteHash != quote.Hash {
		return nil, ErrEvaluationEvidence
	}
	usage.requestInput = &evidence.Input
	usage.environmentCalls = evidence.Calls
	charge, err := Calculate(quote, usage)
	if err != nil {
		return nil, ErrEvaluationEvidence
	}
	return charge, nil
}
