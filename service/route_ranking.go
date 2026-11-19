package service

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

type routeRank struct {
	channel                                        *model.Channel
	health, band, cacheBand, load, active, samples int
	cacheKnown                                     bool
	healthFactor, ratio, price, draw, rank         float64
}
type rankGroup struct {
	health, band int
	priority     int64
	positive     bool
}

func (r routeRank) group() rankGroup {
	return rankGroup{r.health, r.band, r.channel.GetPriority(), r.channel.GetWeight() > 0}
}
func rankGroupLess(a, b rankGroup) bool {
	if a.health != b.health {
		return a.health < b.health
	}
	if a.band != b.band {
		return a.band < b.band
	}
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	return a.positive && !b.positive
}
func rankRouteGroup(rows []routeRank) {
	limited := 0
	for _, r := range rows {
		if r.channel.MaxConcurrency > 0 {
			limited++
		}
	}
	cheapest := map[int]float64{}
	for i := range rows {
		r := &rows[i]
		switch limited {
		case len(rows):
			r.load = min(3, 4*r.active/r.channel.MaxConcurrency)
		case 0:
			r.load = routeLoadBucket(r.active)
		default:
			r.load = 0
		}
		if r.ratio > 0 && (cheapest[r.load] == 0 || r.ratio < cheapest[r.load]) {
			cheapest[r.load] = r.ratio
		}
	}
	for i := range rows {
		r := &rows[i]
		r.price = 1
		if r.ratio > 0 && r.healthFactor == 1 {
			r.price = max(.85, math.Sqrt(cheapest[r.load]/r.ratio))
		}
		r.rank = r.draw / (float64(max(1, r.channel.GetWeight())) * r.healthFactor * r.price)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.load != b.load {
			return a.load < b.load
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.channel.Id < b.channel.Id
	})
}
func sortRouteRanks(rows []routeRank) {
	sort.SliceStable(rows, func(i, j int) bool { return rankGroupLess(rows[i].group(), rows[j].group()) })
	for begin := 0; begin < len(rows); {
		end := begin + 1
		for end < len(rows) && rows[end].group() == rows[begin].group() {
			end++
		}
		rankRouteGroup(rows[begin:end])
		begin = end
	}
}
func rankAutomatic(rows []routeRank) {
	sortRouteRanks(rows)
	known := make([]routeRank, 0, len(rows))
	changed := false
	for _, r := range rows {
		if r.cacheKnown {
			changed = changed || r.cacheBand > r.band
			r.band = max(r.band, r.cacheBand)
			known = append(known, r)
		}
	}
	if !changed {
		return
	}
	sortRouteRanks(known)
	j := 0
	for i := range rows {
		if rows[i].cacheKnown {
			rows[i] = known[j]
			j++
		}
	}
}
func orderAutomaticRoutes(rows []*model.Channel, requestID, modelName string, operation int, stream bool) {
	orderRouteRows(nil, rows, requestID, modelName, operation, stream)
}
func orderRuntimeRoutes(c *gin.Context, rows []*model.Channel, modelName string, operation int, stream bool) {
	orderRouteRows(c, rows, c.GetString(common.RequestIdKey), modelName, operation, stream)
}
func orderRouteRows(c *gin.Context, rows []*model.Channel, requestID, modelName string, operation int, stream bool) {
	now := time.Now()
	quality := routeQualities.snapshot()
	var ctx routeCacheRequest
	if c != nil {
		ctx = routeCacheContext(c)
	}
	cache := routeCaches.bands(ctx, modelName, operation, stream, now)
	ranked := make([]routeRank, 0, len(rows))
	for _, ch := range rows {
		mapped, _, err := relaycommon.ResolveMappedModelName(modelName, ch.GetModelMapping())
		if err != nil {
			mapped = ""
		}
		stat := routeQualityOf(quality, now, modelName, ch.Id, mapped, operation, stream)
		health, factor := routeObservations.routeHealth(ch.Id, mapped)
		ratio, _ := strconv.ParseFloat(ch.ChannelRatio, 64) // Eligibility has already validated the configured ratio.
		hash := sha256.Sum256([]byte(requestID + ":" + strconv.Itoa(ch.Id)))
		u := (float64(binary.BigEndian.Uint64(hash[:8])>>11) + 1) / (float64(uint64(1)<<53) + 1)
		r := routeRank{channel: ch, health: health, healthFactor: factor, band: stat.Band, samples: stat.Samples, active: ChannelInFlight(ch.Id), ratio: ratio, draw: -math.Log(u)}
		if sample := cache[ch.Id]; sample.Known && !ctx.resource {
			r.cacheKnown = true
			r.cacheBand = sample.Band
		}
		ranked = append(ranked, r)
	}
	rankAutomatic(ranked)
	for i, r := range ranked {
		rows[i] = r.channel
	}
	if c == nil {
		return
	}
	c.Set("route_rank_candidates", ranked)
	c.Set("route_explore_channel", 0)
	_, pinned, _ := GetChannelConstraints(c).ResolvedPin()
	if pinned || routePreselecting(c) || operation == relayconstant.RelayModeNativeCount || ctx.resource || len(c.GetStringSlice("use_channel")) != 0 {
		return
	}
	eligible, deficient := []int{}, []int{}
	for _, r := range ranked {
		if r.health != 0 || r.healthFactor != 1 || r.channel.GetWeight() == 0 || r.channel.MaxConcurrency > 0 && r.active >= r.channel.MaxConcurrency {
			continue
		}
		eligible = append(eligible, r.channel.Id)
		if r.samples < 50 || r.band > 0 || ctx.identifiable() && !r.cacheKnown {
			deficient = append(deficient, r.channel.Id)
		}
	}
	if len(eligible) < 2 {
		return
	}
	chosen := routeCaches.explore(ctx, modelName, operation, stream, eligible, deficient, now)
	index := slices.IndexFunc(rows, func(ch *model.Channel) bool { return ch.Id == chosen })
	if index >= 0 {
		ch := rows[index]
		copy(rows[1:index+1], rows[:index])
		rows[0] = ch
		c.Set("route_explore_channel", chosen)
	}
}

// routeQualityOf is a channel's quality on the request's model, operation and
// stream type; a row not updated for two hours counts as unrated.
func routeQualityOf(quality qualitySnapshot, now time.Time, modelName string, channelID int, mapped string, operation int, stream bool) RouteQualitySnapshot {
	stat := quality[routeQualityKey{modelName, routeObservationKey{channelID, mapped, operation, stream}}]
	if now.UnixMilli()-stat.UpdatedAt > 2*time.Hour.Milliseconds() {
		return RouteQualitySnapshot{}
	}
	return stat
}

func setRouteDecision(c *gin.Context, id int, source string) {
	band, price := 0, 1.0
	if source == "auto" {
		if ranks, ok := common.GetContextKeyType[[]routeRank](c, "route_rank_candidates"); ok {
			for _, r := range ranks {
				if r.channel.Id == id {
					band, price = r.band, r.price
					break
				}
			}
		}
		if id == c.GetInt("route_explore_channel") {
			source, price = "explore", 1
		}
	}
	c.Set("route_admission_source", source)
	c.Set("route_selected_quality", band)
	c.Set("route_price_factor", price)
}
