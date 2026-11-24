package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func upstreamAEAD() (cipher.AEAD, error) {
	if os.Getenv("CRYPTO_SECRET") == "" && os.Getenv("SESSION_SECRET") == "" {
		return nil, errors.New("persistent_secret_required")
	}
	key, err := hkdf.Key(sha256.New, []byte(common.CryptoSecret), nil, "ren2hub/upstream-account/v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func credentialAAD(a *model.UpstreamAccount) []byte {
	return []byte(a.Platform + "\x00" + a.CredentialUID)
}
func sealUpstreamCredential(a *model.UpstreamAccount, c UpstreamCredential) (string, error) {
	box, err := upstreamAEAD()
	if err != nil {
		return "", err
	}
	plain, err := common.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(box.Seal(nil, nil, plain, credentialAAD(a))), nil
}
func openUpstreamCredential(a *model.UpstreamAccount) (c UpstreamCredential, err error) {
	box, err := upstreamAEAD()
	if err != nil {
		return c, err
	}
	blob, err := base64.RawStdEncoding.DecodeString(a.CredentialCiphertext)
	if err != nil {
		return c, err
	}
	plain, err := box.Open(nil, nil, blob, credentialAAD(a))
	if err != nil {
		return c, err
	}
	err = common.Unmarshal(plain, &c)
	return c, err
}
func credentialForAccount(a *model.UpstreamAccount) (UpstreamCredential, error) {
	c, err := openUpstreamCredential(a)
	if err == nil {
		return c, nil
	}
	if writeErr := model.UpdateUpstreamAccountCAS(a.ID, a.Version, map[string]any{"status": "credential_undecryptable"}); writeErr != nil {
		return c, writeErr
	}
	return c, errors.New("credential_undecryptable")
}
