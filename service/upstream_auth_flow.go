package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"sync"
	"time"
)

type UpstreamAuthRequest struct {
	AccountID    int    `json:"account_id"`
	Platform     string `json:"platform"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	ProxyURL     string `json:"proxy_url"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	PAT          string `json:"pat"`
	RefreshToken string `json:"refresh_token"`
}
type UpstreamAuthResult struct {
	AccountID int    `json:"account_id,omitempty"`
	Challenge string `json:"challenge,omitempty"`
}

// The challenge contains only the remote flow token and the account revision
// that started the login. Credentials stay on the stack of the HTTP request.
type upstreamChallenge struct {
	admin, id, oldPAT                       int
	version                                 int64
	until                                   time.Time
	token, platform, name, base, proxy, uid string
}

var upstreamChallenges = struct {
	sync.Mutex
	entries map[string]upstreamChallenge
}{entries: make(map[string]upstreamChallenge)}

func storeUpstreamChallenge(c upstreamChallenge) (UpstreamAuthResult, error) {
	upstreamChallenges.Lock()
	defer upstreamChallenges.Unlock()
	now := time.Now()
	n := 0
	for k, v := range upstreamChallenges.entries {
		if !v.until.After(now) {
			delete(upstreamChallenges.entries, k)
		} else if v.admin == c.admin {
			n++
		}
	}
	if n >= 4 || len(upstreamChallenges.entries) >= 64 {
		return UpstreamAuthResult{}, errors.New("challenge_limit")
	}
	id := uuid.NewString()
	c.until = now.Add(5 * time.Minute)
	upstreamChallenges.entries[id] = c
	return UpstreamAuthResult{Challenge: id}, nil
}
func validateUpstreamEndpoint(platform, base, proxy string) error {
	if platform != "sub2api" && platform != "newapi" {
		return errors.New("unsupported_platform")
	}
	u, e := url.Parse(base)
	if e != nil || u.User != nil || u.Hostname() == "" || strings.Contains(u.Hostname(), "%") || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid_base_url")
	}
	if proxy != "" {
		if _, _, e := common.ParseProxyURLRuntime(proxy); e != nil {
			return errors.New("invalid_proxy_url")
		}
	}
	return nil
}
func ProbeUpstream(ctx context.Context, platform, base, proxy string) (UpstreamCapabilities, error) {
	if err := validateUpstreamEndpoint(platform, base, proxy); err != nil {
		return UpstreamCapabilities{}, err
	}
	ad, err := newUpstreamAdapter(platform, base, proxy, UpstreamCredential{})
	if err != nil {
		return UpstreamCapabilities{}, err
	}
	return ad.Probe(ctx)
}
func StartUpstreamAuth(ctx context.Context, admin int, in UpstreamAuthRequest) (out UpstreamAuthResult, err error) {
	if _, err = upstreamAEAD(); err != nil {
		return
	}
	c := upstreamChallenge{admin: admin, id: in.AccountID, name: strings.TrimSpace(in.Name), platform: in.Platform, base: strings.TrimRight(in.BaseURL, "/"), proxy: in.ProxyURL, uid: uuid.NewString()}
	if c.id != 0 {
		defer lockManagedAccount(c.id)()
		a, e := model.GetUpstreamAccount(c.id)
		if e != nil {
			return out, e
		}
		c.name, c.platform, c.base, c.proxy, c.uid, c.version = a.Name, a.Platform, a.BaseURL, a.ProxyURL, a.CredentialUID, a.Version
		if prev, e := openUpstreamCredential(a); e == nil {
			c.oldPAT = prev.RemotePATID
		}
	}
	if c.name == "" || len(c.name) > 128 {
		return out, errors.New("invalid_account_name")
	}
	if err = validateUpstreamEndpoint(c.platform, c.base, c.proxy); err != nil {
		return
	}
	if (in.PAT != "" && (c.platform != "newapi" || in.Password != "" || in.RefreshToken != "")) || (in.RefreshToken != "" && (c.platform != "sub2api" || in.Password != "")) {
		return out, errors.New("invalid_credentials")
	}
	cred := UpstreamCredential{PAT: in.PAT, RefreshToken: in.RefreshToken}
	ad, err := newUpstreamAdapter(c.platform, c.base, c.proxy, cred)
	if err != nil {
		return
	}
	switch {
	case in.PAT != "":
		_, err = ad.Snapshot(ctx)
	case in.RefreshToken != "":
		cred, err = ad.Refresh(ctx)
	case in.Username != "" && in.Password != "":
		c.token, err = ad.Login(ctx, in.Username, in.Password, c.uid, c.oldPAT, c.persist(&out))
		if err == nil && c.token != "" {
			return storeUpstreamChallenge(c)
		}
		return
	default:
		return out, errors.New("invalid_credentials")
	}
	if err != nil {
		return
	}
	return saveAuthorizedUpstream(c, cred)
}
func consumeUpstreamChallenge(admin int, id, code string) (upstreamChallenge, error) {
	upstreamChallenges.Lock()
	defer upstreamChallenges.Unlock()
	c, ok := upstreamChallenges.entries[id]
	if !ok || c.admin != admin {
		return upstreamChallenge{}, errors.New("challenge_invalid")
	}
	if !c.until.After(time.Now()) {
		delete(upstreamChallenges.entries, id)
		return upstreamChallenge{}, errors.New("challenge_invalid")
	}
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return upstreamChallenge{}, errors.New("invalid_verification_code")
	}
	delete(upstreamChallenges.entries, id)
	return c, nil
}

func CompleteUpstreamAuth(ctx context.Context, admin int, id, code string) (out UpstreamAuthResult, err error) {
	c, err := consumeUpstreamChallenge(admin, id, code)
	if err != nil {
		return out, err
	}
	if c.id != 0 {
		defer lockManagedAccount(c.id)()
		a, e := model.GetUpstreamAccount(c.id)
		if e != nil {
			return out, e
		}
		if a.Version != c.version {
			return out, errors.New("challenge_invalid")
		}
	}
	ad, err := newUpstreamAdapter(c.platform, c.base, c.proxy, UpstreamCredential{})
	if err != nil {
		return
	}
	err = ad.Complete(ctx, c.token, code, c.uid, c.oldPAT, c.persist(&out))
	return
}

// persist completes the local commit inside the remote login session. Only
// after it succeeds may the adapter retire the previous credential.
func (c upstreamChallenge) persist(out *UpstreamAuthResult) func(UpstreamCredential) error {
	return func(cred UpstreamCredential) error {
		result, err := saveAuthorizedUpstream(c, cred)
		if err == nil {
			*out = result
		}
		return err
	}
}
func saveAuthorizedUpstream(c upstreamChallenge, cred UpstreamCredential) (out UpstreamAuthResult, err error) {
	a := &model.UpstreamAccount{ID: c.id, Name: c.name, Platform: c.platform, BaseURL: c.base, ProxyURL: c.proxy, CredentialUID: c.uid, Status: "active"}
	a.CredentialCiphertext, err = sealUpstreamCredential(a, cred)
	if err != nil {
		return
	}
	if a.ID == 0 {
		err = model.CreateUpstreamAccount(a)
	} else {
		err = model.UpdateUpstreamAccountCAS(a.ID, c.version, map[string]any{"credential_ciphertext": a.CredentialCiphertext, "status": "active", "next_sync_at": 0, "last_error": ""})
	}
	if err == nil {
		pendingRotations.Delete(a.ID)
		out.AccountID = a.ID
	}
	return
}
