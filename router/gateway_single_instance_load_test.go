package router

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

type gatewayLoadRedisHook struct{ commands, legacy atomic.Int64 }

func (h *gatewayLoadRedisHook) count(cmd redis.Cmder) {
	h.commands.Add(1)
	for _, arg := range cmd.Args() {
		if text, ok := arg.(string); ok && strings.Contains(text, "route:lease:") {
			h.legacy.Add(1)
		}
	}
}
func (h *gatewayLoadRedisHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	h.count(cmd)
	return ctx, nil
}
func (*gatewayLoadRedisHook) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (h *gatewayLoadRedisHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	for _, cmd := range cmds {
		h.count(cmd)
	}
	return ctx, nil
}
func (*gatewayLoadRedisHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

type gatewayLoadSQLLogger struct {
	logger.Interface
	queries atomic.Int64
}

func (l *gatewayLoadSQLLogger) Trace(context.Context, time.Time, func() (string, int64), error) {
	l.queries.Add(1)
}

type gatewayLoadResult struct {
	status                  int
	body                    string
	duration, timeToContent time.Duration
	err                     error
}
type gatewayLoadReport struct {
	Scenario                  string   `json:"scenario"`
	Redis                     string   `json:"redis"`
	Requests                  int      `json:"requests"`
	Successful                int      `json:"successful"`
	WaitTimeouts              int      `json:"wait_timeouts"`
	QueueRejected             int      `json:"queue_rejected"`
	Unavailable               int      `json:"unavailable"`
	UpstreamSends             int64    `json:"upstream_sends"`
	ExtraSends                uint64   `json:"extra_sends"`
	ExtraSendRatio            float64  `json:"extra_send_ratio"`
	SuccessRate               float64  `json:"success_rate"`
	TimeoutRate               float64  `json:"wait_timeout_rate"`
	DurationP50MS             float64  `json:"duration_p50_ms"`
	DurationP95MS             float64  `json:"duration_p95_ms"`
	ContentP50MS              float64  `json:"first_content_p50_ms"`
	ContentP95MS              float64  `json:"first_content_p95_ms"`
	QueueEntries              uint64   `json:"queue_entries"`
	AverageQueueMS            float64  `json:"average_queue_ms"`
	SQLQueries                int64    `json:"sql_queries"`
	RedisCommands             int64    `json:"redis_client_commands"`
	LegacyConcurrencyCommands int64    `json:"legacy_concurrency_commands"`
	ProcessCPUMS              *float64 `json:"test_process_cpu_ms"`
	AllocatedBytes            uint64   `json:"test_process_allocated_bytes"`
	HeapBeforeBytes           uint64   `json:"test_process_heap_before_bytes"`
	HeapAfterBytes            uint64   `json:"test_process_heap_after_bytes"`
	ElapsedMS                 float64  `json:"elapsed_ms"`
	Limitations               []string `json:"limitations"`
}

func gatewayLoadCPU() *float64 {
	if runtime.GOOS != "windows" {
		return nil
	}
	script := fmt.Sprintf("(Get-Process -Id %d).TotalProcessorTime.TotalMilliseconds.ToString([Globalization.CultureInfo]::InvariantCulture)", os.Getpid())
	data, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return nil
	}
	return &value
}

// A real local Redis database is leased only when explicitly requested. No
// FLUSH command is used; cleanup unlinks keys from the initially empty test DB.
func gatewayLoadRedis(t *testing.T) string {
	t.Helper()
	address := os.Getenv("GATEWAY_LOAD_REDIS_ADDR")
	if address == "" {
		return "miniredis (in-process test server)"
	}
	host, _, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.True(t, host == "localhost" || net.ParseIP(host).IsLoopback(), "load test Redis must be loopback")
	var client *redis.Client
	var dbIndex int
	for dbIndex = 15; dbIndex >= 8; dbIndex-- {
		candidate := redis.NewClient(&redis.Options{Addr: address, DB: dbIndex, MaxRetries: -1})
		count, err := candidate.DBSize(context.Background()).Result()
		require.NoError(t, err)
		if count == 0 {
			claimed, err := candidate.SetNX(context.Background(), "gateway:load:exclusive-test", true, time.Minute).Result()
			require.NoError(t, err)
			if claimed {
				client = candidate
				break
			}
		}
		require.NoError(t, candidate.Close())
	}
	require.NotNil(t, client, "no empty isolated Redis DB between 8 and 15")
	previous := common.RDB
	common.RDB = client
	t.Cleanup(func() {
		common.RDB = previous
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, "*", 100).Result()
			require.NoError(t, err)
			if len(keys) > 0 {
				require.NoError(t, client.Unlink(ctx, keys...).Err())
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		require.NoError(t, client.Close())
	})
	return fmt.Sprintf("Redis %s DB %d (initially empty, loopback only)", address, dbIndex)
}

func gatewayLoadRequest(ctx context.Context, url, key string) gatewayLoadResult {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"load fixture"}],"stream":true}`))
	if err != nil {
		return gatewayLoadResult{err: err}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return gatewayLoadResult{err: err, duration: time.Since(start)}
	}
	defer response.Body.Close()
	result := gatewayLoadResult{status: response.StatusCode}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		result.body += line + "\n"
		if result.timeToContent == 0 && strings.Contains(line, `"content":"hello"`) {
			result.timeToContent = time.Since(start)
		}
	}
	result.err = scanner.Err()
	result.duration = time.Since(start)
	return result
}

func gatewayLoadPercentile(values []time.Duration, percent int) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := (len(values)*percent+99)/100 - 1
	return float64(values[max(index, 0)]) / float64(time.Millisecond)
}

func TestGatewayProviderFullChannelRoutesToIndependentChannel(t *testing.T) {
	var a, b atomic.Int64
	url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer local-test-key" {
			a.Add(1)
		} else {
			b.Add(1)
		}
		gatewaySDKUpstream(w, r)
	})
	config := model.DefaultGatewayConfig()
	config.ChannelRates[1] = model.GatewayProviderRateLimit{RPM: 1}
	initialConfig, err := common.Marshal(model.DefaultGatewayConfig())
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(initialConfig)}).Error)
	require.NoError(t, model.SaveGatewayConfig(config))
	reservation, err := service.ReserveGatewayProviderRate(context.Background(), common.RDB, []service.GatewayProviderRateScope{{ID: service.GatewayChannelRateScopeID(1), RPM: 1}}, 0)
	require.NoError(t, err)
	require.NoError(t, service.FinishGatewayProviderRate(context.Background(), common.RDB, reservation, 0, true, false, 0))
	result := gatewayLoadRequest(context.Background(), url, key)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.status, result.body)
	assert.Positive(t, result.timeToContent)
	assert.Zero(t, a.Load(), "a confirmed full channel must be skipped before provider I/O")
	assert.EqualValues(t, 1, b.Load(), "independent B retains its send quota")
	var user model.User
	require.Eventually(t, func() bool { return model.DB.First(&user, 1).Error == nil && user.RequestCount == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, 1, user.RequestCount)
}

func TestGatewaySingleInstanceControlledLoad(t *testing.T) {
	if os.Getenv("GATEWAY_RUN_LOAD") != "1" {
		t.Skip("set GATEWAY_RUN_LOAD=1 for controlled HTTP load")
	}
	for _, scenario := range []string{"low", "burst", "congestion", "partial_failure", "all_unavailable"} {
		t.Run(scenario, func(t *testing.T) { runGatewayControlledLoad(t, scenario) })
	}
}

func runGatewayControlledLoad(t *testing.T, scenario string) {
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	defer release()
	var sends, aSends, bSends atomic.Int64
	entered := make(chan struct{}, 32)
	url, key, otherKey := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		isA := r.Header.Get("Authorization") == "Bearer local-test-key"
		if isA {
			aSends.Add(1)
		} else {
			bSends.Add(1)
		}
		if scenario == "partial_failure" && isA {
			w.Header().Set("Retry-After", "60")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"temporary test quota exhausted","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
			return
		}
		entered <- struct{}{}
		if scenario == "burst" || scenario == "congestion" {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		gatewaySDKUpstream(w, r)
	})
	redisDescription := gatewayLoadRedis(t)
	// Shared-cache SQLite does not serialize concurrent write transactions itself.
	// Keep database writes serialized in this fixture; gateway/upstream work remains concurrent.
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	config := model.DefaultGatewayConfig()
	config.UserConcurrency, config.UserQueueSize, config.TotalQueueSize = 2, 20, 32
	if scenario == "congestion" {
		config.UserConcurrency, config.UserQueueSize, config.TotalQueueSize, config.WaitMS = 1, 4, 4, 500
	}
	initialConfig, err := common.Marshal(model.DefaultGatewayConfig())
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(initialConfig)}).Error)
	require.NoError(t, model.SaveGatewayConfig(config))
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{1, 4}).Update("capacity_total", 1).Error)
	if scenario == "all_unavailable" {
		for _, id := range []int{1, 4} {
			require.NoError(t, model.DB.Create(&model.ChannelHealth{ChannelID: id, Model: "gpt-4o-mini", State: model.RouteHealthStateOpen, HealthEpoch: 2, CooldownUntil: time.Now().Add(time.Minute).Unix()}).Error)
		}
	}
	hooks := &gatewayLoadRedisHook{}
	common.RDB.AddHook(hooks)
	sqlLogger := &gatewayLoadSQLLogger{Interface: model.DB.Config.Logger}
	model.DB.Config.Logger = sqlLogger
	before, err := service.ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	cpuBefore := gatewayLoadCPU()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	sqlBefore, redisBefore := sqlLogger.queries.Load(), hooks.commands.Load()
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*8)
	defer cancel()
	var results []gatewayLoadResult
	switch scenario {
	case "low", "partial_failure", "all_unavailable":
		for index := 0; index < 12; index++ {
			results = append(results, gatewayLoadRequest(ctx, url, key))
		}
	case "burst":
		done := make(chan gatewayLoadResult, 18)
		for index := 0; index < 18; index++ {
			token := key
			if index%2 == 1 {
				token = otherKey
			}
			go func() { done <- gatewayLoadRequest(ctx, url, token) }()
		}
		require.Eventually(t, func() bool { return service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(1)).Queued == 16 }, time.Second*2, time.Millisecond)
		release()
		for range 18 {
			results = append(results, <-done)
		}
	case "congestion":
		done := make(chan gatewayLoadResult, 7)
		go func() { done <- gatewayLoadRequest(ctx, url, key) }()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("first request did not reach upstream")
		}
		for range 4 {
			go func() { done <- gatewayLoadRequest(ctx, url, otherKey) }()
		}
		require.Eventually(t, func() bool { return service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(1)).Queued == 4 }, time.Millisecond*300, time.Millisecond)
		for range 2 {
			go func() { done <- gatewayLoadRequest(ctx, url, key) }()
		}
		for range 6 {
			results = append(results, <-done)
		}
		release()
		results = append(results, <-done)
	}
	require.Eventually(t, func() bool {
		u := service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(1))
		a := service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1))
		b := service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(4))
		return u.InFlight+u.Queued+a.InFlight+a.Queued+b.InFlight+b.Queued == 0
	}, time.Second, time.Millisecond)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&memAfter)
	sqlCount, redisCount := sqlLogger.queries.Load()-sqlBefore, hooks.commands.Load()-redisBefore
	cpuAfter := gatewayLoadCPU()
	after, err := service.ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	report := gatewayLoadReport{Scenario: scenario, Redis: redisDescription, Requests: len(results), UpstreamSends: sends.Load(), ExtraSends: after.RetryBudget.RetrySends - before.RetryBudget.RetrySends, SQLQueries: sqlCount, RedisCommands: redisCount, LegacyConcurrencyCommands: hooks.legacy.Load(), AllocatedBytes: memAfter.TotalAlloc - memBefore.TotalAlloc, HeapBeforeBytes: memBefore.HeapAlloc, HeapAfterBytes: memAfter.HeapAlloc, ElapsedMS: float64(elapsed) / float64(time.Millisecond), Limitations: []string{"new implementation only; no equivalent dirty-worktree old application baseline", "SQLite in-memory with one database connection; test-process CPU and heap include gateway, mock upstream and HTTP clients", "Redis counts are client commands including pipelines and scripts, not Lua-internal operations", "TTFT measures the first non-empty content event; admission latency is only observed through queue metrics", "SQL counts exclude setup and final verification but may include asynchronous billing/log work; not a production throughput benchmark"}}
	if cpuBefore != nil && cpuAfter != nil {
		cpuDelta := *cpuAfter - *cpuBefore
		report.ProcessCPUMS = &cpuDelta
	}
	report.QueueEntries = after.UserQueue.Entered - before.UserQueue.Entered + after.ChannelQueue.Entered - before.ChannelQueue.Entered
	waitMS := after.UserQueue.WaitMS - before.UserQueue.WaitMS + after.ChannelQueue.WaitMS - before.ChannelQueue.WaitMS
	if report.QueueEntries > 0 {
		report.AverageQueueMS = float64(waitMS) / float64(report.QueueEntries)
	}
	var durations, ttft []time.Duration
	for _, result := range results {
		require.NoError(t, result.err)
		durations = append(durations, result.duration)
		switch result.status {
		case http.StatusOK:
			report.Successful++
			require.Contains(t, result.body, "[DONE]")
			require.Positive(t, result.timeToContent)
			ttft = append(ttft, result.timeToContent)
		case http.StatusTooManyRequests:
			if strings.Contains(result.body, "wait_timeout") {
				report.WaitTimeouts++
			} else {
				report.QueueRejected++
			}
		case http.StatusServiceUnavailable:
			report.Unavailable++
		default:
			t.Fatalf("unexpected HTTP status %d: %s", result.status, result.body)
		}
	}
	report.SuccessRate = float64(report.Successful) / float64(report.Requests)
	report.TimeoutRate = float64(report.WaitTimeouts) / float64(report.Requests)
	report.ExtraSendRatio = float64(report.ExtraSends) / float64(report.Requests)
	report.DurationP50MS, report.DurationP95MS = gatewayLoadPercentile(durations, 50), gatewayLoadPercentile(durations, 95)
	report.ContentP50MS, report.ContentP95MS = gatewayLoadPercentile(ttft, 50), gatewayLoadPercentile(ttft, 95)
	assert.Zero(t, report.LegacyConcurrencyCommands)
	// Validation reads are outside the command measurement window.
	var legacyCursor uint64
	for {
		keys, next, err := common.RDB.Scan(context.Background(), legacyCursor, "route:lease:*", 100).Result()
		require.NoError(t, err)
		require.Empty(t, keys, "HTTP traffic must not create legacy concurrency keys")
		legacyCursor = next
		if legacyCursor == 0 {
			break
		}
	}
	assert.Equal(t, after.Admission.Granted-after.Admission.Released, uint64(after.Admission.InFlight), "grant/release conservation")
	assert.Zero(t, after.Admission.InFlight)
	assert.Zero(t, after.Admission.Queued)
	switch scenario {
	case "low", "burst":
		assert.Equal(t, report.Requests, report.Successful)
		assert.EqualValues(t, report.Requests, report.UpstreamSends)
		assert.Zero(t, report.ExtraSends)
		if scenario == "burst" {
			assert.GreaterOrEqual(t, report.QueueEntries, uint64(16))
		}
	case "congestion":
		assert.Equal(t, 1, report.Successful)
		assert.Equal(t, 4, report.WaitTimeouts)
		assert.Equal(t, 2, report.QueueRejected)
		assert.EqualValues(t, 1, report.UpstreamSends)
	case "partial_failure":
		assert.Equal(t, report.Requests, report.Successful)
		assert.EqualValues(t, 1, aSends.Load())
		assert.EqualValues(t, report.Requests, bSends.Load())
		assert.EqualValues(t, 1, report.ExtraSends)
	case "all_unavailable":
		assert.Equal(t, report.Requests, report.Unavailable)
		assert.Zero(t, report.UpstreamSends)
		assert.Zero(t, report.ExtraSends)
	}
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, report.Successful, user.RequestCount, "only accepted logical requests are billed")
	encoded, err := common.Marshal(report)
	require.NoError(t, err)
	t.Log(string(encoded))
	directory := filepath.Join("..", ".local-tests", "single-instance")
	require.NoError(t, os.MkdirAll(directory, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "http-load-"+scenario+".json"), encoded, 0600))
}
