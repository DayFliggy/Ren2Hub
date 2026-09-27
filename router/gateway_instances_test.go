package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGatewayApplicationSingleInstanceRestart(t *testing.T) {
	binary, adminDSN, redisAddress := os.Getenv("GATEWAY_APP_BINARY"), os.Getenv("GATEWAY_APP_MYSQL_DSN"), os.Getenv("GATEWAY_APP_REDIS_ADDR")
	if binary == "" || adminDSN == "" || redisAddress == "" {
		t.Skip("set GATEWAY_APP_BINARY, GATEWAY_APP_MYSQL_DSN and GATEWAY_APP_REDIS_ADDR for isolated application-process acceptance")
	}
	binary, err := filepath.Abs(binary)
	require.NoError(t, err)
	_, err = os.Stat(binary)
	require.NoError(t, err)
	dsn, db := gatewayInstancesDatabase(t, adminDSN)
	gatewayInstancesRedis(t, redisAddress)
	var upstreamCalls atomic.Int64
	upstreamCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		var request map[string]any
		if err := common.DecodeJson(r.Body, &request); err != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		encoded, err := common.Marshal(request)
		if err != nil {
			http.Error(w, "invalid fixture data", 500)
			return
		}
		if bytes.Contains(encoded, []byte("hold-instance-request")) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"chat_hold\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"held\"},\"finish_reason\":null}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"chat_hold\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"total_tokens\":11}}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(upstreamCancelled)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/v1/chat/completions":
			response = map[string]any{"id": "chat_complete", "object": "chat.completion", "created": 1, "model": request["model"], "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "completed"}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}}
		case "/v1/messages":
			response = map[string]any{"id": "msg_complete", "type": "message", "role": "assistant", "model": request["model"], "content": []any{map[string]any{"type": "text", "text": "completed"}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2}}
		default:
			http.Error(w, "unexpected fixture path", 400)
			return
		}
		data, err := common.Marshal(response)
		if err != nil {
			http.Error(w, "invalid fixture response", 500)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(upstream.Close)
	users, tokens, _ := gatewayInstancesSeed(t, db, upstream.URL)
	secret := uuid.NewString()
	secrets := []string{dsn, secret, tokens[0].Key, tokens[1].Key, tokens[2].Key}
	first := startGatewayInstance(t, binary, dsn, redisAddress, secret, "first", secrets)
	second := first

	unauthenticated := gatewayInstanceCall(t, second.url, "/v1/chat/completions", "", false, "complete")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.StatusCode)
	require.NoError(t, unauthenticated.Body.Close())
	assert.Zero(t, upstreamCalls.Load())

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	request := gatewayInstanceRequest(t, ctx, first.url, "/v1/chat/completions", tokens[0].Key, true, "hold-instance-request")
	held, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	if held.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(held.Body)
		_ = held.Body.Close()
		var snapshots []model.ChannelCapabilitySnapshot
		var health []model.ChannelHealth
		require.NoError(t, db.Find(&snapshots).Error)
		require.NoError(t, db.Find(&health).Error)
		t.Logf("runtime evidence: snapshots=%+v health=%+v", snapshots, health)
		t.Fatalf("holding request failed: status=%d body=%s\nfirst application: %s\nsecond application: %s", held.StatusCode, body, first.log.String(), second.log.String())
	}
	t.Cleanup(func() { _ = held.Body.Close() })
	require.Eventually(t, func() bool { return upstreamCalls.Load() == 1 }, 3*time.Second, 10*time.Millisecond)

	blockedUser := gatewayInstanceCall(t, second.url, "/v1/messages", tokens[1].Key, false, "complete")
	assert.Equal(t, http.StatusTooManyRequests, blockedUser.StatusCode, "different protocols and tokens share the user's in-flight limit")
	require.NoError(t, blockedUser.Body.Close())
	assert.Equal(t, int64(1), upstreamCalls.Load(), "user rejection must happen before upstream dispatch")

	blockedChannel := gatewayInstanceCall(t, second.url, "/v1/chat/completions", tokens[2].Key, false, "complete")
	assert.Equal(t, http.StatusTooManyRequests, blockedChannel.StatusCode, "channel capacity is shared by all users in the process")
	require.NoError(t, blockedChannel.Body.Close())
	assert.Equal(t, int64(1), upstreamCalls.Load(), "capacity rejection must not send another upstream request")
	var rejectedUser model.User
	require.NoError(t, db.First(&rejectedUser, users[1].Id).Error)
	assert.EqualValues(t, users[1].Quota, rejectedUser.Quota, "capacity rejection cannot consume balance")
	var rejectedConsumes int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ?", users[1].Id, model.LogTypeConsume).Count(&rejectedConsumes).Error)
	assert.Zero(t, rejectedConsumes)

	cancel()
	require.NoError(t, held.Body.Close())
	select {
	case <-upstreamCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("client cancellation did not reach the local upstream")
	}

	completed := gatewayInstanceCall(t, second.url, "/v1/chat/completions", tokens[2].Key, false, "complete")
	completedBody, err := io.ReadAll(completed.Body)
	require.NoError(t, err)
	require.NoError(t, completed.Body.Close())
	require.Equal(t, http.StatusOK, completed.StatusCode, "%s", completedBody)
	assert.Contains(t, string(completedBody), `"finish_reason":"stop"`)
	requestID := completed.Header.Get(common.RequestIdKey)
	require.NotEmpty(t, requestID)
	gatewayInstanceBilling(t, db, users[1], tokens[2], requestID)

	claude := gatewayInstanceCall(t, second.url, "/v1/messages", tokens[1].Key, false, "complete")
	claudeBody, err := io.ReadAll(claude.Body)
	require.NoError(t, err)
	require.NoError(t, claude.Body.Close())
	require.Equal(t, http.StatusOK, claude.StatusCode, "%s", claudeBody)
	assert.Contains(t, string(claudeBody), `"stop_reason":"end_turn"`)
	assert.Equal(t, int64(3), upstreamCalls.Load())
	require.NoError(t, first.command.Process.Kill())
	select {
	case <-first.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("old process did not exit")
	}
	restarted := startGatewayInstance(t, binary, dsn, redisAddress, secret, "restarted", secrets)
	resumed := gatewayInstanceCall(t, restarted.url, "/v1/chat/completions", tokens[2].Key, false, "complete")
	require.Equal(t, http.StatusOK, resumed.StatusCode)
	require.NoError(t, resumed.Body.Close())
	t.Log("TokenAuth, cross-protocol user rejection, capacity rejection without dispatch, cancellation propagation, readmission and database billing passed")
}

func gatewayInstancesDatabase(t *testing.T, adminDSN string) (string, *gorm.DB) {
	t.Helper()
	parsed, err := mysqldriver.ParseDSN(adminDSN)
	require.NoError(t, err)
	admin, err := gorm.Open(mysql.Open(adminDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, adminSQL.Close()) })
	name := "gateway_instances_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	quoted := "`" + name + "`"
	require.NoError(t, admin.Exec("CREATE DATABASE "+quoted+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error)
	t.Cleanup(func() { assert.NoError(t, admin.Exec("DROP DATABASE "+quoted).Error) })
	parsed.DBName = name
	dsn := parsed.FormatDSN()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	return dsn, db
}

func gatewayInstancesRedis(t *testing.T, address string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: address, DB: 15, MaxRetries: -1})
	require.NoError(t, client.Ping(t.Context()).Err())
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	owner := uuid.NewString()
	locked, err := client.SetNX(t.Context(), "gateway:application-test:lock", owner, 2*time.Minute).Result()
	require.NoError(t, err)
	require.True(t, locked, "Redis DB 15 is already owned by another application acceptance test")
	existing := map[string]bool{}
	iterator := client.Scan(t.Context(), 0, "*", 1000).Iterator()
	for iterator.Next(t.Context()) {
		existing[iterator.Val()] = true
	}
	require.NoError(t, iterator.Err())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		iterator := client.Scan(ctx, 0, "*", 1000).Iterator()
		var created []string
		for iterator.Next(ctx) {
			if !existing[iterator.Val()] {
				created = append(created, iterator.Val())
			}
		}
		assert.NoError(t, iterator.Err())
		if len(created) > 0 {
			assert.NoError(t, client.Del(ctx, created...).Err())
		}
		current, err := client.Get(ctx, "gateway:application-test:lock").Result()
		if assert.NoError(t, err) && assert.Equal(t, owner, current) {
			assert.NoError(t, client.Del(ctx, "gateway:application-test:lock").Err())
		}
	})
	return client
}

func gatewayInstancesSeed(t *testing.T, db *gorm.DB, upstream string) ([]model.User, []model.Token, []model.Channel) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Setup{}))
	config := model.DefaultGatewayConfig()
	config.UserConcurrency, config.WaitMS, config.UserQueueSize, config.ChannelQueueSize = 1, 0, 0, 0
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	options := map[string]string{model.GatewayConfigKey: string(encoded), "auto_pricing.enabled": "false", "ModelPrice": `{"gpt-4o-mini":0.001,"claude-3-5-sonnet":0.001}`, "ModelRequestRateLimitEnabled": "false", "AutomaticDisableChannelEnabled": "false", "AutomaticEnableChannelEnabled": "false", "monitor_setting.auto_test_channel_enabled": "false", "fetch_setting.allow_private_ip": "true"}
	for key, value := range options {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	require.NoError(t, db.Create(&model.Setup{Version: "isolated-acceptance", InitializedAt: time.Now().Unix()}).Error)
	base := 1_700_000_000 + int(uuid.New().ID()%100_000_000)
	users := []model.User{{Id: base, Username: "instance-user-a", Password: "fixture-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 1000000, Group: "default", AffCode: "instance-a"}, {Id: base + 1, Username: "instance-user-b", Password: "fixture-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 1000000, Group: "default", AffCode: "instance-b"}}
	for i := range users {
		require.NoError(t, db.Create(&users[i]).Error)
	}
	tokens := []model.Token{{Id: base + 2, UserId: users[0].Id}, {Id: base + 3, UserId: users[0].Id}, {Id: base + 4, UserId: users[1].Id}}
	for i := range tokens {
		tokens[i].Key = "gi" + strings.ReplaceAll(uuid.NewString(), "-", "")
		tokens[i].Name, tokens[i].Status, tokens[i].ExpiredTime, tokens[i].RemainQuota, tokens[i].ConversionPolicy = "local acceptance", common.TokenStatusEnabled, -1, 1000000, "safe"
		require.NoError(t, db.Create(&tokens[i]).Error)
	}
	channels := []model.Channel{{Id: base + 5, Type: constant.ChannelTypeOpenAI, Models: "gpt-4o-mini"}, {Id: base + 6, Type: constant.ChannelTypeAnthropic, Models: "claude-3-5-sonnet"}}
	for i := range channels {
		channels[i].Name, channels[i].Key, channels[i].Status, channels[i].BaseURL, channels[i].Group, channels[i].CapacityTotal, channels[i].ChannelRatio = "local acceptance", "local-upstream-fixture", common.ChannelStatusEnabled, &upstream, "default", 1, "1"
		require.NoError(t, db.Create(&channels[i]).Error)
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: channels[i].Models, ChannelId: channels[i].Id, Enabled: true, Weight: 1}).Error)
	}
	return users, tokens, channels
}

type gatewayApplicationLog struct {
	mu      sync.Mutex
	bytes   []byte
	secrets []string
}

func (log *gatewayApplicationLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.bytes = append(log.bytes, data...)
	if len(log.bytes) > 32000 {
		log.bytes = append([]byte(nil), log.bytes[len(log.bytes)-32000:]...)
	}
	return len(data), nil
}
func (log *gatewayApplicationLog) String() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	value := string(log.bytes)
	for _, secret := range log.secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

type gatewayApplicationInstance struct {
	command *exec.Cmd
	url     string
	exited  chan struct{}
	log     *gatewayApplicationLog
}

func startGatewayInstance(t *testing.T, binary, dsn, redisAddress, secret, name string, secrets []string) *gatewayApplicationInstance {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	instance := &gatewayApplicationInstance{url: "http://127.0.0.1:" + strconv.Itoa(port), exited: make(chan struct{}), log: &gatewayApplicationLog{secrets: secrets}}
	instance.command = exec.CommandContext(t.Context(), binary)
	instance.command.Dir = t.TempDir()
	instance.command.Env = append(os.Environ(), "PORT="+strconv.Itoa(port), "SQL_DSN="+dsn, "LOG_SQL_DSN=", "REDIS_CONN_STRING=redis://"+redisAddress+"/15", "SESSION_SECRET="+secret, "CRYPTO_SECRET="+secret, "NODE_NAME=gateway-acceptance-"+name+"-"+uuid.NewString(), "NODE_TYPE=master", "DEBUG=false", "GIN_MODE=release", "SYNC_FREQUENCY=60", "BATCH_UPDATE_ENABLED=false", "CHANNEL_UPDATE_FREQUENCY=", "UPDATE_TASK=false", "ENABLE_PPROF=false", "PYROSCOPE_SERVER_ADDRESS=", "SQL_MAX_OPEN_CONNS=10", "SQL_MAX_IDLE_CONNS=2", "HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=")
	instance.command.Stdout, instance.command.Stderr = instance.log, instance.log
	require.NoError(t, instance.command.Start())
	go func() { _ = instance.command.Wait(); close(instance.exited) }()
	t.Cleanup(func() {
		select {
		case <-instance.exited:
			return
		default:
		}
		_ = instance.command.Process.Kill()
		select {
		case <-instance.exited:
		case <-time.After(3 * time.Second):
			t.Errorf("test application %d did not exit", instance.command.Process.Pid)
		}
	})
	client := &http.Client{Timeout: 300 * time.Millisecond}
	ready := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-instance.exited:
			t.Fatalf("application exited before ready: %s", instance.log.String())
		default:
		}
		response, err := client.Get(instance.url + "/api/status")
		if err == nil {
			ready = response.StatusCode == 200
			_ = response.Body.Close()
			if ready {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, ready, "application did not become ready: %s", instance.log.String())
	return instance
}

func gatewayInstanceRequest(t *testing.T, ctx context.Context, baseURL, path, key string, stream bool, content string) *http.Request {
	t.Helper()
	modelName := "gpt-4o-mini"
	if path == "/v1/messages" {
		modelName = "claude-3-5-sonnet"
	}
	body, err := common.Marshal(map[string]any{"model": modelName, "stream": stream, "max_tokens": 16, "messages": []any{map[string]any{"role": "user", "content": content}}})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer sk-"+key)
	}
	if path == "/v1/messages" {
		request.Header.Set("anthropic-version", "2023-06-01")
	}
	return request
}

func gatewayInstanceCall(t *testing.T, baseURL, path, key string, stream bool, content string) *http.Response {
	t.Helper()
	request := gatewayInstanceRequest(t, t.Context(), baseURL, path, key, stream, content)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	return response
}

func gatewayInstanceBilling(t *testing.T, db *gorm.DB, initialUser model.User, initialToken model.Token, requestID string) {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var logs []model.Log
		if !assert.NoError(collect, db.Where("request_id = ? AND type = ?", requestID, model.LogTypeConsume).Find(&logs).Error) || !assert.Len(collect, logs, 1) {
			return
		}
		assert.Positive(collect, logs[0].Quota)
		var user model.User
		var token model.Token
		assert.NoError(collect, db.First(&user, initialUser.Id).Error)
		assert.NoError(collect, db.First(&token, initialToken.Id).Error)
		assert.EqualValues(collect, initialUser.Quota-logs[0].Quota, user.Quota)
		assert.EqualValues(collect, initialToken.RemainQuota-logs[0].Quota, token.RemainQuota)
		assert.EqualValues(collect, logs[0].Quota, token.UsedQuota)
		var recovery model.BillingRecovery
		if assert.NoError(collect, db.Where("request_id = ?", requestID).First(&recovery).Error) {
			assert.Equal(collect, model.BillingRecoveryStatusSettled, recovery.Status)
		}
	}, 3*time.Second, 20*time.Millisecond)
}
