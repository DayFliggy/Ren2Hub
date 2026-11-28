package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// channelUpstreamAccountFilterIDs resolves the optional `upstream_account` query
// parameter into matching channel IDs. Returns (nil, 0, nil) when omitted,
// 400 on invalid IDs, and 403 when the caller lacks upstream:read permission.
func channelUpstreamAccountFilterIDs(c *gin.Context) ([]int, int, error) {
	raw := c.Query("upstream_account")
	if raw == "" {
		return nil, 0, nil
	}
	accountID, err := strconv.Atoi(raw)
	if err != nil || accountID <= 0 {
		return nil, http.StatusBadRequest, errors.New("invalid upstream_account")
	}
	if !middleware.CanWithAccessToken(c, authz.UpstreamRead) {
		return nil, http.StatusForbidden, errors.New("upstream read permission required")
	}
	ids, err := model.ListUpstreamChannelIDsForAccount(accountID)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return ids, 0, nil
}

// channelFilterIDs combines optional `lab` and `upstream_account` filters into
// a single channel-ID whitelist compatible with applyChannelLabFilter.
func channelFilterIDs(c *gin.Context) ([]int, int, error) {
	labIDs, err := channelLabFilterIDs(c)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	upstreamIDs, status, err := channelUpstreamAccountFilterIDs(c)
	if err != nil {
		return nil, status, err
	}
	if labIDs == nil {
		return upstreamIDs, 0, nil
	}
	if upstreamIDs == nil {
		return labIDs, 0, nil
	}
	allowed := make(map[int]struct{}, len(upstreamIDs))
	for _, id := range upstreamIDs {
		allowed[id] = struct{}{}
	}
	merged := make([]int, 0, min(len(labIDs), len(upstreamIDs)))
	for _, id := range labIDs {
		if _, ok := allowed[id]; ok {
			merged = append(merged, id)
		}
	}
	return merged, 0, nil
}

func filterChannelsByIDs(channels []*model.Channel, ids []int) []*model.Channel {
	if ids == nil {
		return channels
	}
	if len(ids) == 0 {
		return make([]*model.Channel, 0)
	}
	allowed := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	filtered := make([]*model.Channel, 0, len(channels))
	for _, ch := range channels {
		if ch != nil {
			if _, ok := allowed[ch.Id]; ok {
				filtered = append(filtered, ch)
			}
		}
	}
	return filtered
}
