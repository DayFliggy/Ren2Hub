package service_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const gatewayProcessRedisDB = 13

// Opt-in integration test. Every table has a run-specific prefix; Redis keys
// use distinct user/channel IDs in DB 13. It never flushes a Redis database.
func TestGatewayMultiProcessSuccessReservation(t *testing.T) {
	dsn, redisAddress := os.Getenv("GATEWAY_MULTIPROCESS_MYSQL_DSN"), os.Getenv("GATEWAY_MULTIPROCESS_REDIS_ADDR")
	if dsn == "" || redisAddress == "" {
		t.Skip("set GATEWAY_MULTIPROCESS_MYSQL_DSN and GATEWAY_MULTIPROCESS_REDIS_ADDR to isolated local services")
	}
	prefix := "gmi_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12] + "_"
	db := openGatewayProcessDB(t, dsn, prefix)
	previousDB, previousKind := model.DB, common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	client := redis.NewClient(&redis.Options{Addr: redisAddress, DB: gatewayProcessRedisDB, MaxRetries: -1})
	fixture := &gatewayProcessFixture{db: db, redis: client, nextID: 1_500_000_000 + int(uuid.New().ID()%100_000_000), prefix: prefix}
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousKind)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for _, userID := range fixture.users {
			key := service.GatewayLogicalRateKey(userID)
			assert.NoError(t, client.Del(cleanupCtx, key+":total", key+":success", key+":pending").Err())
		}
		assert.NoError(t, db.Migrator().DropTable(&model.Channel{}, &model.User{}, &model.Option{}))
		assert.NoError(t, client.Close())
		sqlDB, closeErr := db.DB()
		if assert.NoError(t, closeErr) {
			assert.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Channel{}))
	require.NoError(t, client.Ping(t.Context()).Err())
	config := model.DefaultGatewayConfig()
	config.UserConcurrency, config.WaitMS = 1, 0
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	proxy := startGatewayRedisProxy(t, redisAddress)
	first := startGatewayProcess(t, dsn, prefix, proxy.listener.Addr().String())
	second := startGatewayProcess(t, dsn, prefix, proxy.listener.Addr().String())
	require.NotEqual(t, first.command.Process.Pid, second.command.Process.Pid)
	var databaseVersion string
	require.NoError(t, db.Raw("SELECT VERSION()").Scan(&databaseVersion).Error)
	info, err := client.Info(t.Context(), "server").Result()
	require.NoError(t, err)
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "redis_version:") {
			t.Logf("real MySQL %s, %s, independent PIDs %d/%d", databaseVersion, line, first.command.Process.Pid, second.command.Process.Pid)
		}
	}

	t.Run("strict_success_reservation_across_processes", func(t *testing.T) {
		user, channel := fixture.account(t, 10, 0, "success"), fixture.channel(t, 10)
		left, right := first.begin(t, "/v1/chat/completions", user, channel), second.begin(t, "/v1/messages", user, channel)
		leftResult, rightResult := left.result(t), right.result(t)
		require.NoError(t, leftResult.err)
		require.NoError(t, rightResult.err)
		assert.ElementsMatch(t, []int{200, 429}, []int{leftResult.response.StatusCode, rightResult.response.StatusCode})
		winner, loser := left, right
		winnerResponse, loserResponse := leftResult.response, rightResult.response
		if leftResult.response.StatusCode != 200 {
			winner, loser = right, left
			winnerResponse, loserResponse = rightResult.response, leftResult.response
		}
		require.NoError(t, loserResponse.Body.Close())
		loser.cancel()
		key := service.GatewayLogicalRateKey(user)
		assert.Equal(t, int64(1), client.ZCard(t.Context(), key+":pending").Val())
		winner.cancel()
		require.NoError(t, winnerResponse.Body.Close())
		require.Eventually(t, func() bool { return client.ZCard(t.Context(), key+":pending").Val() == 0 }, 3*time.Second, 10*time.Millisecond)
		assert.Zero(t, client.ZCard(t.Context(), key+":success").Val())
		completed := second.hold(t, "/v1/responses", user, channel)
		completed.complete(t)
		require.Eventually(t, func() bool { return client.ZCard(t.Context(), key+":success").Val() == 1 }, 3*time.Second, 10*time.Millisecond)
		response := first.request(t, "/v1/chat/completions", user, channel)
		assert.Equal(t, http.StatusTooManyRequests, response.StatusCode)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, int64(4), client.ZCard(t.Context(), key+":total").Val(), "success-capacity rejections still count once as logical requests")
	})

}

type gatewayProcessFixture struct {
	db     *gorm.DB
	redis  *redis.Client
	nextID int
	prefix string
	users  []int
}

func (fixture *gatewayProcessFixture) account(t *testing.T, concurrency, wait int, group string) int {
	t.Helper()
	fixture.nextID++
	user := model.User{Id: fixture.nextID, Username: fmt.Sprint("u", fixture.nextID), AffCode: fmt.Sprint(fixture.nextID), Status: common.UserStatusEnabled, Group: group, GatewayConcurrency: &concurrency, GatewayWaitMS: &wait}
	require.NoError(t, fixture.db.Create(&user).Error)
	fixture.users = append(fixture.users, user.Id)
	return user.Id
}

func (fixture *gatewayProcessFixture) channel(t *testing.T, capacity int) int {
	t.Helper()
	fixture.nextID++
	channel := model.Channel{Id: fixture.nextID, Key: "local-fixture-only", Name: fixture.prefix, Status: common.ChannelStatusEnabled, CapacityTotal: capacity, ChannelRatio: "1"}
	require.NoError(t, fixture.db.Create(&channel).Error)
	return channel.Id
}

func openGatewayProcessDB(t *testing.T, dsn, prefix string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	return db
}

type gatewayWorkerProcess struct {
	command *exec.Cmd
	url     string
	exited  chan struct{}
	logsMu  sync.Mutex
	logs    []string
}

func startGatewayProcess(t *testing.T, dsn, prefix, redisAddress string) *gatewayWorkerProcess {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	worker := &gatewayWorkerProcess{command: exec.Command(binary, "-test.run=^TestGatewayMultiProcessWorker$", "-test.timeout=55s"), exited: make(chan struct{})}
	worker.command.Env = append(os.Environ(), "GATEWAY_PROCESS_WORKER=1", "GATEWAY_PROCESS_DSN="+dsn, "GATEWAY_PROCESS_PREFIX="+prefix, "GATEWAY_PROCESS_REDIS="+redisAddress)
	stdout, err := worker.command.StdoutPipe()
	require.NoError(t, err)
	worker.command.Stderr = worker.command.Stdout
	require.NoError(t, worker.command.Start())
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "GATEWAY_PROCESS_READY ") {
				ready <- strings.TrimPrefix(line, "GATEWAY_PROCESS_READY ")
			}
			worker.logsMu.Lock()
			worker.logs = append(worker.logs, line)
			worker.logsMu.Unlock()
		}
	}()
	go func() { _ = worker.command.Wait(); close(worker.exited) }()
	t.Cleanup(func() {
		select {
		case <-worker.exited:
			return
		default:
		}
		client := &http.Client{Timeout: time.Second}
		if response, stopErr := client.Post(worker.url+"/control/shutdown", "text/plain", nil); stopErr == nil {
			_ = response.Body.Close()
		}
		select {
		case <-worker.exited:
		case <-time.After(2 * time.Second):
			_ = worker.command.Process.Kill()
			<-worker.exited
		}
	})
	select {
	case worker.url = <-ready:
	case <-worker.exited:
		worker.logsMu.Lock()
		output := strings.Join(worker.logs, "\n")
		worker.logsMu.Unlock()
		t.Fatalf("worker exited before serving: %s", output)
	case <-time.After(8 * time.Second):
		t.Fatal("worker startup timed out")
	}
	return worker
}

type gatewayHTTPResult struct {
	response *http.Response
	err      error
}
type gatewayPendingRequest struct {
	worker *gatewayWorkerProcess
	id     string
	cancel context.CancelFunc
	done   chan gatewayHTTPResult
}
type gatewayHeldRequest struct {
	pending  *gatewayPendingRequest
	response *http.Response
}

func (worker *gatewayWorkerProcess) begin(t *testing.T, path string, userID, channelID int) *gatewayPendingRequest {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	pending := &gatewayPendingRequest{worker: worker, id: uuid.NewString(), cancel: cancel, done: make(chan gatewayHTTPResult, 1)}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, worker.url+path, strings.NewReader(`{"model":"gateway-model","messages":[{"role":"user","content":"local admission fixture"}],"max_tokens":2,"contents":[{"parts":[{"text":"local admission fixture"}]}]}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Fixture-User", strconv.Itoa(userID))
	request.Header.Set("X-Fixture-Channel", strconv.Itoa(channelID))
	request.Header.Set("X-Fixture-Request", pending.id)
	go func() {
		response, callErr := http.DefaultClient.Do(request)
		pending.done <- gatewayHTTPResult{response: response, err: callErr}
	}()
	t.Cleanup(cancel)
	return pending
}

func (pending *gatewayPendingRequest) result(t *testing.T) gatewayHTTPResult {
	t.Helper()
	select {
	case result := <-pending.done:
		return result
	case <-time.After(4 * time.Second):
		pending.cancel()
		t.Fatal("request did not reach an admission result")
		return gatewayHTTPResult{}
	}
}

func (worker *gatewayWorkerProcess) request(t *testing.T, path string, userID, channelID int) *http.Response {
	t.Helper()
	result := worker.begin(t, path, userID, channelID).result(t)
	require.NoError(t, result.err)
	return result.response
}

func (worker *gatewayWorkerProcess) hold(t *testing.T, path string, userID, channelID int) *gatewayHeldRequest {
	t.Helper()
	pending := worker.begin(t, path, userID, channelID)
	result := pending.result(t)
	require.NoError(t, result.err)
	if result.response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(result.response.Body)
		_ = result.response.Body.Close()
		t.Fatalf("admission returned %d: %s", result.response.StatusCode, body)
	}
	return &gatewayHeldRequest{pending: pending, response: result.response}
}

func (held *gatewayHeldRequest) complete(t *testing.T) {
	t.Helper()
	response, err := (&http.Client{Timeout: time.Second}).Post(held.pending.worker.url+"/control/release/"+held.pending.id, "text/plain", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())
	_, err = io.Copy(io.Discard, held.response.Body)
	require.NoError(t, err)
	require.NoError(t, held.response.Body.Close())
	held.pending.cancel()
}

// This helper is a separate application process. Identity is supplied by the
// test fixture, and the final handler is local; the admission middleware and
// Redis/database implementations are the same ones used by relay requests.
func TestGatewayMultiProcessWorker(t *testing.T) {
	if os.Getenv("GATEWAY_PROCESS_WORKER") != "1" {
		t.Skip("subprocess helper")
	}
	model.DB = openGatewayProcessDB(t, os.Getenv("GATEWAY_PROCESS_DSN"), os.Getenv("GATEWAY_PROCESS_PREFIX"))
	common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	common.RDB = redis.NewClient(&redis.Options{Addr: os.Getenv("GATEWAY_PROCESS_REDIS"), DB: gatewayProcessRedisDB, MaxRetries: -1, DialTimeout: 150 * time.Millisecond, ReadTimeout: 150 * time.Millisecond, WriteTimeout: 150 * time.Millisecond})
	common.RedisEnabled = true
	require.NoError(t, common.RDB.Ping(t.Context()).Err())
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = true, 1000, 1, 1
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	var gates sync.Map
	shutdown := make(chan struct{})
	engine.POST("/control/shutdown", func(c *gin.Context) { c.Status(204); close(shutdown) })
	engine.POST("/control/release/:id", func(c *gin.Context) {
		if value, found := gates.LoadAndDelete(c.Param("id")); found {
			close(value.(chan struct{}))
			c.Status(204)
		} else {
			c.Status(404)
		}
	})
	requests := engine.Group("", func(c *gin.Context) {
		userID, _ := strconv.Atoi(c.GetHeader("X-Fixture-User"))
		var user model.User
		if err := model.DB.WithContext(c.Request.Context()).Select("id", "group").First(&user, userID).Error; err != nil {
			c.AbortWithStatus(401)
			return
		}
		c.Set("id", userID)
		common.SetContextKey(c, constant.ContextKeyUserId, userID)
		common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
		c.Set(common.RequestIdKey, c.GetHeader("X-Fixture-Request"))
		c.Next()
	})
	requests.Use(func(c *gin.Context) {
		if common.GetContextKeyString(c, constant.ContextKeyUserGroup) == "success" {
			middleware.ModelRequestRateLimit()(c)
		} else {
			c.Next()
		}
	}, middleware.GatewayAdmission())
	handler := func(c *gin.Context) {
		channelID, _ := strconv.Atoi(c.GetHeader("X-Fixture-Channel"))
		_, budget, ok := service.GatewayAdmissionFromContext(c.Request.Context())
		if !ok {
			c.AbortWithStatus(500)
			return
		}
		lease, _, err := service.AcquireGatewayChannelPermit(c.Request.Context(), c.GetString(common.RequestIdKey), channelID, true, budget)
		if err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, service.ErrAdmissionCapacity) || errors.Is(err, service.ErrGatewayWaitExpired) || errors.Is(err, service.ErrGatewayQueueFull) {
				status = http.StatusTooManyRequests
			}
			c.AbortWithStatusJSON(status, gin.H{"error": service.GatewayAdmissionErrorCode(err)})
			return
		}
		defer lease.Release()
		gate := make(chan struct{})
		id := c.GetString(common.RequestIdKey)
		gates.Store(id, gate)
		defer gates.Delete(id)
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Fixture-Pid", strconv.Itoa(os.Getpid()))
		c.Status(200)
		c.Writer.Flush()
		select {
		case <-gate:
			_, _ = c.Writer.WriteString("data: completed\n\n")
		case <-c.Request.Context().Done():
			middleware.MarkRelayRequestFailed(c)
		}
	}
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses", "/v1beta/models/*model"} {
		requests.POST(path, handler)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: engine, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	fmt.Println("GATEWAY_PROCESS_READY http://" + listener.Addr().String())
	<-shutdown
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	_ = common.RDB.Close()
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

type gatewayRedisProxy struct {
	listener    net.Listener
	upstream    string
	mu          sync.Mutex
	available   bool
	connections map[net.Conn]struct{}
}

func startGatewayRedisProxy(t *testing.T, upstream string) *gatewayRedisProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	proxy := &gatewayRedisProxy{listener: listener, upstream: upstream, available: true, connections: map[net.Conn]struct{}{}}
	go func() {
		for {
			incoming, err := listener.Accept()
			if err != nil {
				return
			}
			go proxy.forward(incoming)
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); proxy.setAvailable(false) })
	return proxy
}

func (proxy *gatewayRedisProxy) setAvailable(available bool) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.available = available
	if !available {
		for connection := range proxy.connections {
			_ = connection.Close()
		}
	}
}

func (proxy *gatewayRedisProxy) forward(incoming net.Conn) {
	outgoing, err := net.DialTimeout("tcp", proxy.upstream, 150*time.Millisecond)
	if err != nil {
		_ = incoming.Close()
		return
	}
	proxy.mu.Lock()
	if !proxy.available {
		proxy.mu.Unlock()
		_ = incoming.Close()
		_ = outgoing.Close()
		return
	}
	proxy.connections[incoming], proxy.connections[outgoing] = struct{}{}, struct{}{}
	proxy.mu.Unlock()
	defer func() {
		_ = incoming.Close()
		_ = outgoing.Close()
		proxy.mu.Lock()
		delete(proxy.connections, incoming)
		delete(proxy.connections, outgoing)
		proxy.mu.Unlock()
	}()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(outgoing, incoming); _ = outgoing.Close(); close(done) }()
	_, _ = io.Copy(incoming, outgoing)
	_ = incoming.Close()
	<-done
}
