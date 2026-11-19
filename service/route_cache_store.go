package service

import (
	"container/list"
	"maps"
	"slices"
	"sync"
	"time"
)

const (
	cachePeriod         = 5 * time.Minute
	cachePeriods        = 12
	cacheRowsLimit      = 4096
	cacheCellsLimit     = 131072
	cacheVisitsLimit    = 65536
	cacheRequestMinimum = 50
	cacheSessionMinimum = 10
)

type cacheGroup struct {
	Model     string
	Operation int
	Stream    bool
	Mode      string
}
type cacheRowKey struct {
	group   cacheGroup
	channel int
	mapped  string
}
type cacheStratum struct {
	length uint8
	source string
	warm   bool
}
type cacheCohort struct {
	session [16]byte
	stratum cacheStratum
}
type cacheCounts struct {
	valid, count, hits    int
	read, input, maxInput float64
}

func (s *cacheCounts) add(o cacheCounts) {
	s.count += o.count
	s.valid += o.valid
	s.hits += o.hits
	s.read += o.read
	s.input += o.input
	s.maxInput = max(s.maxInput, o.maxInput)
}

type cacheBucket struct {
	tick       int64
	counts     cacheCounts
	cells      map[cacheCohort]cacheCounts
	incomplete bool
}

type cacheRow struct {
	buckets         [cachePeriods]cacheBucket
	last, qualified time.Time
}
type cacheVisitKey struct {
	group   cacheGroup
	session [16]byte
	channel int
}
type cacheVisit struct {
	key           cacheVisitKey
	last, expires time.Time
	exploring     int
	observed      bool
}
type cacheStore struct {
	mu        sync.Mutex
	rows      map[cacheRowKey]*cacheRow
	visits    map[cacheVisitKey]*list.Element
	lru       list.List
	cells     int
	evictions uint64
	revision  uint64
	result    map[cacheGroup]map[int]routeCacheBand
	state     map[cacheRowKey]cacheLevel
	at        time.Time
	duration  time.Duration
	budget    [cachePeriods]cacheBudget
}

type cacheLevel struct {
	band, pending     int
	previous, changed time.Time
	// Keep the sessions seen during the first qualifying round on both channels.
	witnesses     map[[16]byte]bool
	peerWitnesses map[[16]byte]bool
}

type routeCacheBand struct {
	ChannelID   int     `json:"channel_id"`
	Model       string  `json:"model"`
	MappedModel string  `json:"mapped_model"`
	Mode        string  `json:"mode"`
	Operation   int     `json:"operation"`
	Stream      bool    `json:"stream"`
	Known       bool    `json:"known"`
	Band        int     `json:"band"`
	Reason      string  `json:"reason"`
	Samples     int     `json:"samples"`
	Sessions    int     `json:"sessions"`
	Coverage    float64 `json:"coverage"`
	TokenRate   float64 `json:"token_rate"`
	HitRate     float64 `json:"hit_rate"`
	Reference   int     `json:"reference,omitempty"`
	Missing     int     `json:"missing_samples"`
}

type RouteCacheDiagnostics struct {
	Bands        []routeCacheBand `json:"bands"`
	EvaluatedAt  int64            `json:"evaluated_at"`
	EvaluationMS int64            `json:"evaluation_ms"`
	Cells        int              `json:"cells"`
	CellLimit    int              `json:"cell_limit"`
	VisitCount   int              `json:"visit_count"`
	VisitLimit   int              `json:"visit_limit"`
	Evictions    uint64           `json:"evictions"`
	FreeRequests int              `json:"free_requests"`
	Explorations int              `json:"explorations"`
}

var routeCaches = newCacheStore()

func newCacheStore() *cacheStore {
	return &cacheStore{rows: make(map[cacheRowKey]*cacheRow), visits: make(map[cacheVisitKey]*list.Element),
		result: make(map[cacheGroup]map[int]routeCacheBand), state: make(map[cacheRowKey]cacheLevel)}
}

func cacheTick(t time.Time) int64 { return t.Unix() / int64(cachePeriod/time.Second) }

func (s *cacheStore) record(ctx routeCacheRequest, model string, key routeObservationKey, started, finished time.Time, read, input int64, valid bool, source string) {
	if ctx.resource || ctx.mode == "" || model == "" || key.model == "" || key.id <= 0 {
		return
	}
	group := cacheGroup{model, key.operation, key.stream, ctx.mode}
	rowKey := cacheRowKey{group, key.id, key.model}
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.rows[rowKey]
	if row == nil {
		if len(s.rows) >= cacheRowsLimit {
			s.evictions++
			return
		}
		row = &cacheRow{}
		s.rows[rowKey] = row
	}
	row.last = finished
	tick := cacheTick(finished)
	bucket := &row.buckets[tick%cachePeriods]
	if bucket.tick != tick {
		s.cells -= len(bucket.cells)
		*bucket = cacheBucket{tick: tick}
	}
	count := cacheCounts{count: 1}
	if valid && input > 0 && read >= 0 && read <= input {
		count.valid = 1
		if read > 0 {
			count.hits = 1
		}
		count.input = float64(input)
		count.read = float64(read)
		count.maxInput = count.input
	}
	bucket.counts.add(count)
	if !ctx.identifiable() {
		return
	}
	visitKey := cacheVisitKey{group, ctx.session, key.id}
	visit := s.lookup(visitKey, started)
	warm := visit != nil && !visit.last.After(started)
	s.remember(visitKey, finished, ctx.ttl, 0).observed = true
	// The channel-zero entry recognizes free repeat visits before a channel is selected.
	groupVisit := cacheVisitKey{group, ctx.session, 0}
	exploring := 0
	if existing := s.lookup(groupVisit, finished); existing != nil {
		exploring = existing.exploring
	}
	s.remember(groupVisit, finished, ctx.ttl, exploring).observed = true
	if input <= 0 {
		return
	}
	if count.valid > 0 {
		row.qualified = finished
	}
	stratum := cacheStratum{source: source, warm: warm}
	switch {
	case input >= 32768:
		stratum.length = 3
	case input >= 8192:
		stratum.length = 2
	case input >= 1024:
		stratum.length = 1
	}
	if source == "explore" || source == "automatic" || source == "" {
		stratum.source = "auto"
	}
	cohort := cacheCohort{ctx.session, stratum}
	existing, ok := bucket.cells[cohort]
	if !ok {
		if s.cells >= cacheCellsLimit {
			s.evictions++
			bucket.incomplete = true
			return
		}
		s.cells++
	}
	if bucket.cells == nil {
		bucket.cells = make(map[cacheCohort]cacheCounts)
	}
	existing.add(count)
	bucket.cells[cohort] = existing
}

func (s *cacheStore) lookup(key cacheVisitKey, now time.Time) *cacheVisit {
	e := s.visits[key]
	if e == nil {
		return nil
	}
	v := e.Value.(*cacheVisit)
	if !now.Before(v.expires) {
		s.lru.Remove(e)
		delete(s.visits, key)
		return nil
	}
	return v
}
func (s *cacheStore) remember(key cacheVisitKey, now time.Time, ttl time.Duration, exploring int) *cacheVisit {
	if e := s.visits[key]; e != nil {
		visit := e.Value.(*cacheVisit)
		if now.Before(visit.last) {
			return visit
		}
		visit.last = now
		visit.expires = now.Add(ttl)
		visit.exploring = exploring
		s.lru.MoveToFront(e)
		return visit
	}
	if len(s.visits) >= cacheVisitsLimit {
		oldest := s.lru.Back()
		s.lru.Remove(oldest)
		delete(s.visits, oldest.Value.(*cacheVisit).key)
		s.evictions++
	}
	visit := &cacheVisit{key: key, last: now, expires: now.Add(ttl), exploring: exploring}
	s.visits[key] = s.lru.PushFront(visit)
	return visit
}
func (s *cacheStore) bands(ctx routeCacheRequest, model string, op int, stream bool, now time.Time) map[int]routeCacheBand {
	if !ctx.identifiable() {
		return nil
	}
	g := cacheGroup{model, op, stream, ctx.mode}
	s.mu.Lock()
	defer s.mu.Unlock()
	// This is a read-only lookup, including for previews.
	v := s.visits[cacheVisitKey{g, ctx.session, 0}]
	if v == nil || !v.Value.(*cacheVisit).observed || !now.Before(v.Value.(*cacheVisit).expires) || now.Sub(s.at) > 2*cachePeriod {
		return nil
	}
	return s.result[g]
}

func (s *cacheStore) invalidate(channel int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revision++
	for key, row := range s.rows {
		if key.channel == channel {
			for i := range row.buckets {
				s.cells -= len(row.buckets[i].cells)
			}
			delete(s.rows, key)
			delete(s.state, key)
			// Results are immutable after publication, including references to this channel.
			delete(s.result, key.group)
		}
	}
	for key, e := range s.visits {
		if key.channel == channel || e.Value.(*cacheVisit).exploring == channel {
			s.lru.Remove(e)
			delete(s.visits, key)
		}
	}
}

func (s *cacheStore) diagnostics() RouteCacheDiagnostics {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := RouteCacheDiagnostics{Bands: []routeCacheBand{}, EvaluationMS: s.duration.Milliseconds(), Cells: s.cells, CellLimit: cacheCellsLimit, VisitCount: len(s.visits), VisitLimit: cacheVisitsLimit, Evictions: s.evictions}
	if !s.at.IsZero() {
		d.EvaluatedAt = s.at.UnixMilli()
	}
	for _, rows := range s.result {
		d.Bands = append(d.Bands, slices.Collect(maps.Values(rows))...)
	}
	slices.SortFunc(d.Bands, compareCacheBands)
	d.FreeRequests, d.Explorations = s.budgetTotals(cacheTick(time.Now()))
	return d
}
