package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type routeCacheRequest struct {
	mode     string
	resource bool
	ttl      time.Duration
	session  [16]byte
}

// A restart forgets session identities and evidence together. Neither the original key
// nor this digest is included in a routing diagnostic or an upstream request.
var routeCacheSecret = func() [32]byte {
	var s [32]byte
	rand.Read(s[:])
	return s
}()

func routeCacheContext(c *gin.Context) routeCacheRequest {
	if c == nil || c.Request == nil {
		return routeCacheRequest{}
	}
	const key = "route_cache_request"
	if v, ok := c.Get(key); ok {
		return v.(routeCacheRequest)
	}
	ctx := readRouteCacheContext(c)
	c.Set(key, ctx)
	return ctx
}

func readRouteCacheContext(c *gin.Context) routeCacheRequest {
	ctx := routeCacheRequest{mode: "automatic"}
	if c.Request.Body == nil || !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		return ctx
	}
	body, err := common.GetBodyStorage(c)
	if err != nil {
		return routeCacheRequest{}
	}
	fields, err := common.ReadBodyJSONFields(body, "prompt_cache_key", "prompt_cache_retention", "cachedContent", "previous_response_id", "metadata.user_id", "cache_control", "system.#.cache_control", "tools.#.cache_control", "messages.#.content.#.cache_control")
	if err != nil {
		return routeCacheRequest{}
	}
	ctx.resource = fields[2].String() != "" || fields[3].String() != ""
	if ctx.resource {
		return ctx
	}
	session := strings.TrimSpace(fields[0].String())
	if session != "" {
		ctx.mode = "explicit"
	}
	if fields[1].String() == "24h" {
		ctx.ttl = 24 * time.Hour
	}
	for _, v := range fields[5:] {
		if ttl := ephemeralCacheTTL(v); ttl > 0 {
			ctx.mode = "explicit"
			// A shorter breakpoint lifetime is the conservative opportunity bound.
			if ctx.ttl == 0 || ttl < ctx.ttl {
				ctx.ttl = ttl
			}
		}
	}
	if session == "" {
		for _, header := range []string{"Session_id", "Session-Id", "X-Session-Id"} {
			if v := strings.TrimSpace(c.GetHeader(header)); v != "" {
				session = v
				break
			}
		}
	}
	if session == "" {
		if _, tail, ok := strings.Cut(fields[4].String(), "_session_"); ok && tail != "" {
			session = tail
		}
	}
	tenant := c.GetInt("id")
	if session != "" && tenant > 0 {
		mac := hmac.New(sha256.New, routeCacheSecret[:])
		mac.Write([]byte(strconv.Itoa(tenant)))
		mac.Write([]byte{0})
		mac.Write([]byte(session))
		copy(ctx.session[:], mac.Sum(nil))
	}
	return ctx
}

func ephemeralCacheTTL(v ojson.Result) time.Duration {
	if v.IsArray() {
		var shortest time.Duration
		v.ForEach(func(_, child ojson.Result) bool {
			if ttl := ephemeralCacheTTL(child); ttl > 0 && (shortest == 0 || ttl < shortest) {
				shortest = ttl
			}
			return true
		})
		return shortest
	}
	if v.Get("type").String() != "ephemeral" {
		return 0
	}
	switch v.Get("ttl").String() {
	case "", "5m":
		return 5 * time.Minute
	case "1h":
		return time.Hour
	}
	return 0
}

func (r routeCacheRequest) identifiable() bool {
	return !r.resource && r.ttl > 0 && r.session != [16]byte{}
}
