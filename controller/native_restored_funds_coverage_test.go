//go:build known_upstream_restored

package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The original helper exists only locally. Both sources instead exercise its
// real caller with a valid password-change proof, then inject post-commit faults.
func TestRestoredNativeCommittedSecurityStepsContinue(t *testing.T) {
	for _, failedAction := range []string{"policy", "sessions", "cache"} {
		t.Run(failedAction, func(t *testing.T) {
			db := restoredFundsDB(t)
			require.NoError(t, db.AutoMigrate(&model.AuthFlow{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.UserAccessToken{}, &model.SystemTask{}))
			require.NoError(t, authz.Init(db))
			oldRDB, oldSecret := common.RDB, common.SessionSecret
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			common.RDB, common.RedisEnabled, common.SessionSecret = client, true, "restored-publication-proof"
			t.Cleanup(func() { client.Close(); common.RDB = oldRDB; common.SessionSecret = oldSecret })
			operator := model.User{Id: 9999, Username: "publication-operator", AffCode: "publication-operator", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, Password: "proof-password"}
			user := model.User{Id: 9998, Username: "publication-managed", AffCode: "publication-managed", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, Password: "old-password"}
			require.NoError(t, db.Create(&operator).Error)
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, model.PublishUserAuthCache(operator.Id))
			require.NoError(t, model.PublishUserAuthCache(user.Id))
			bundle, err := service.CreateLoginSession(operator.Id, "password", "127.0.0.1", "restored-publication")
			require.NoError(t, err)
			identity, err := service.ParseAccessToken(bundle.AccessToken)
			require.NoError(t, err)
			binding, err := service.BindVerificationOperation(service.VerificationOperation{Scope: service.VerificationScopeAdminUserUpdate, Context: []byte(fmt.Sprintf(`{"user_id":%d}`, user.Id))})
			require.NoError(t, err)
			proof, _, err := service.IssueSecurityProof(identity, service.VerificationMethodPassword, binding)
			require.NoError(t, err)
			session := model.UserSession{SID: "restored-security-session", UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "restored-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, db.Create(&session).Error)
			var committed atomic.Bool
			var mu sync.Mutex
			attempted := map[string]bool{}
			require.NoError(t, db.Callback().Update().After("gorm:update").Register("restored:committed-user", func(tx *gorm.DB) {
				if tx.Statement.Table == "users" {
					committed.Store(true)
				}
			}))
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("restored:security-fault", func(tx *gorm.DB) {
				if !committed.Load() || tx.Statement.ConnPool != db.Statement.ConnPool || tx.Statement.Schema == nil {
					return
				}
				action := ""
				switch tx.Statement.Schema.Name {
				case "CasbinRule":
					action = "policy"
				case "UserSession":
					action = "sessions"
				case "User":
					if len(tx.Statement.Selects) != 2 {
						action = "cache"
					}
				}
				if action == "" {
					return
				}
				mu.Lock()
				attempted[action] = true
				mu.Unlock()
				if action == failedAction {
					tx.AddError(errors.New(action + " publication unavailable"))
				}
			}))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", strings.NewReader(fmt.Sprintf(`{"id":%d,"username":%q,"password":"new-password-123"}`, user.Id, user.Username)))
			c.Request.Header.Set("X-Security-Proof", proof)
			c.Set("id", identity.UserID)
			c.Set("role", common.RoleRootUser)
			c.Set("username", operator.Username)
			c.Set("session_id", identity.SessionID)
			c.Set("auth_version", identity.UserAuthVersion)
			c.Set("session_version", identity.SessionVersion)
			UpdateUser(c)
			t.Logf("response=%s", w.Body.String())
			require.True(t, committed.Load(), "the password update must reach commit before fault injection")
			var body struct {
				Success, Committed bool
				PublicationPending bool `json:"publication_pending"`
				Message            string
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			securityFailure := failedAction != "cache"
			assert.Equal(t, !securityFailure, body.Success, "original success feedback")
			assert.Equal(t, !securityFailure, body.PublicationPending, "original recovery feedback")
			if securityFailure {
				assert.True(t, body.Committed, "original committed feedback")
				assert.Contains(t, body.Message, "unavailable")
			}
			for _, action := range []string{"policy", "sessions", "cache"} {
				mu.Lock()
				got := attempted[action]
				mu.Unlock()
				assert.True(t, got, action+" still runs after an earlier failure")
			}
			require.NoError(t, db.Callback().Query().Remove("restored:security-fault"))
			var updated model.User
			require.NoError(t, db.First(&updated, user.Id).Error)
			assert.EqualValues(t, 2, updated.AuthVersion)
			cached, err := model.GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, updated.AuthVersion, cached.AuthVersion, "committed cache state remains recoverable")
			var audits []model.AuditLog
			require.NoError(t, db.Where("action = ?", "user.update").Find(&audits).Error)
			assert.Len(t, audits, 1, "original committed action audit")
			if len(audits) > 0 {
				assert.True(t, audits[0].Success)
			}
			require.NoError(t, db.First(&session, "sid = ?", session.SID).Error)
			if failedAction != "sessions" {
				assert.Equal(t, model.UserSessionStatusRevoked, session.Status, "later session revocation still runs")
			} else {
				assert.Equal(t, model.UserSessionStatusActive, session.Status)
			}
			_, _, err = service.ValidateLoginSession(service.AuthIdentity{UserID: user.Id, SessionID: session.SID, SessionVersion: 1, UserAuthVersion: 1})
			assert.ErrorIs(t, err, service.ErrLoginSessionRevoked)
		})
	}
}

func TestRestoredNativeUserDirectoryScopeBeforePaging(t *testing.T) {
	db := restoredFundsDB(t)
	for i, role := range []int{common.RoleRootUser, common.RoleAdminUser, common.RoleCommonUser, common.RoleAdminUser, common.RoleCommonUser, common.RoleCommonUser} {
		require.NoError(t, db.Create(&model.User{Id: i + 1, Username: fmt.Sprintf("account%d", i+1), Role: role, Password: "not-returned", AffCode: fmt.Sprint(i), Status: common.UserStatusEnabled, Email: fmt.Sprintf("account%d@example.test", i+1)}).Error)
	}
	require.NoError(t, db.Delete(&model.User{}, 6).Error)
	check := func(name string, handler gin.HandlerFunc, role int, query string, total int, ids ...int) {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/user/?"+query, nil)
			c.Set("id", 2)
			c.Set("role", role)
			handler(c)
			var r struct {
				Success bool
				Data    struct {
					Total int
					Items []model.User
				}
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
			require.True(t, r.Success)
			assert.Equal(t, total, r.Data.Total, "role restriction precedes count")
			var got []int
			for _, u := range r.Data.Items {
				got = append(got, u.Id)
				assert.NotEqual(t, "not-returned", u.Password)
			}
			assert.Equal(t, ids, got, "role restriction precedes paging and filters")
		})
	}
	for i, h := range []gin.HandlerFunc{GetAllUsers, SearchUsers} {
		check(fmt.Sprintf("handler_%d_page_1", i), h, common.RoleAdminUser, "sort_by=id&sort_order=asc&page_size=1&p=1", 3, 3)
		check(fmt.Sprintf("handler_%d_page_2", i), h, common.RoleAdminUser, "sort_by=id&sort_order=asc&page_size=1&p=2", 3, 5)
		check(fmt.Sprintf("handler_%d_root", i), h, common.RoleRootUser, "sort_by=id&sort_order=asc&page_size=20", 6, 1, 2, 3, 4, 5, 6)
	}
	check("keyword", SearchUsers, common.RoleAdminUser, "keyword=account&sort_by=id&sort_order=asc", 3, 3, 5, 6)
	check("root_id", SearchUsers, common.RoleAdminUser, "keyword=1", 0)
	check("root_email", SearchUsers, common.RoleAdminUser, "keyword=account1@example.test", 0)
	check("admin_role", SearchUsers, common.RoleAdminUser, "role=10", 0)
	check("root_role", SearchUsers, common.RoleAdminUser, "role=100", 0)
	check("deleted", SearchUsers, common.RoleAdminUser, "role=1&status=-1", 1, 6)
	check("root_reads_admin", SearchUsers, common.RoleRootUser, "role=10&sort_by=id&sort_order=asc", 2, 2, 4)
	check("root_reads_root", SearchUsers, common.RoleRootUser, "role=100", 1, 1)
}

func TestRestoredNativeProviderTypedErrors(t *testing.T) {
	for _, provider := range []string{model.PaymentProviderEpay, model.PaymentProviderStripe, model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake} {
		t.Run(provider, func(t *testing.T) {
			db := restoredFundsDB(t)
			u := model.User{Username: "typed-" + provider, AffCode: "typed-" + provider, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&u).Error)
			settle := func(n string) error {
				switch provider {
				case model.PaymentProviderEpay:
					_, err := model.RechargeEpay(n, "", "", "1")
					return err
				case model.PaymentProviderStripe:
					return model.Recharge(n, "", "")
				case model.PaymentProviderCreem:
					return model.RechargeCreem(n, "", "", "")
				case model.PaymentProviderWaffo:
					return model.RechargeWaffo(n, "")
				default:
					return model.RechargeWaffoPancake(n)
				}
			}
			foreign := model.TopUp{UserId: u.Id, TradeNo: "typed-foreign-" + provider, PaymentProvider: model.PaymentProviderBalance, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
			restoredOrder(t, &foreign)
			expired := model.TopUp{UserId: u.Id, TradeNo: "typed-expired-" + provider, PaymentProvider: provider, Status: common.TopUpStatusExpired, Amount: 1, Money: 1}
			restoredOrder(t, &expired)
			if provider == model.PaymentProviderEpay {
				require.NoError(t, db.Model(&expired).Update("currency", "CNY").Error)
			}
			t.Run("missing", func(t *testing.T) { assert.ErrorIs(t, settle("missing-"+provider), model.ErrTopUpNotFound) })
			t.Run("empty", func(t *testing.T) { assert.ErrorIs(t, settle(""), model.ErrTopUpNotFound) })
			t.Run("foreign", func(t *testing.T) { assert.ErrorIs(t, settle(foreign.TradeNo), model.ErrPaymentMethodMismatch) })
			t.Run("expired", func(t *testing.T) { assert.ErrorIs(t, settle(expired.TradeNo), model.ErrTopUpStatusInvalid) })
			t.Run("query_outage", func(t *testing.T) {
				outage := errors.New("database unavailable")
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("restored:typed-outage", func(tx *gorm.DB) {
					if tx.Statement.Table == "top_ups" {
						tx.AddError(outage)
					}
				}))
				err := settle("outage-" + provider)
				require.NoError(t, db.Callback().Query().Remove("restored:typed-outage"))
				assert.ErrorIs(t, err, outage)
				assert.NotErrorIs(t, err, model.ErrTopUpNotFound, "a lookup failure must stay retryable")
			})
			require.NoError(t, db.First(&u, u.Id).Error)
			assert.Zero(t, u.Quota)
			assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(foreign.TradeNo).Status)
		})
	}
}

func TestRestoredNativeStripeReceiptCasesKeepWalletUnchanged(t *testing.T) {
	for _, scenario := range []struct{ name, fields string }{{"currency", `"amount_subtotal":100,"currency":"eur"`}, {"amount", `"amount_subtotal":99,"currency":"usd"`}, {"missing", `"amount_subtotal":0,"currency":""`}} {
		t.Run(scenario.name, func(t *testing.T) {
			db := restoredFundsDB(t)
			deliver := restoredStripeWebhook(t)
			u := model.User{Username: "receipt-" + scenario.name, AffCode: "receipt-" + scenario.name}
			require.NoError(t, db.Create(&u).Error)
			order := model.TopUp{UserId: u.Id, TradeNo: "receipt-" + scenario.name, PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
			restoredOrder(t, &order)
			assert.Equal(t, http.StatusOK, deliver(restoredStripeEvent(`"mode":"payment","client_reference_id":"`+order.TradeNo+`",`+scenario.fields)))
			require.NoError(t, db.First(&u, u.Id).Error)
			assert.Zero(t, u.Quota, "signature alone cannot authorize an amount or currency different from the created order")
			assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
			assert.EqualValues(t, 1, restoredRejections(t, db), "every rejected callback is audited")
		})
	}
}

func TestRestoredNativeEpayRejectedReceiptHasAudit(t *testing.T) {
	db := restoredFundsDB(t)
	u := model.User{Username: "epay-audit", AffCode: "epay-audit"}
	require.NoError(t, db.Create(&u).Error)
	order := model.TopUp{UserId: u.Id, TradeNo: "epay-audit", PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, Amount: 1, Money: 1}
	restoredOrder(t, &order)
	_, _ = model.RechargeEpay(order.TradeNo, "", "", "0.99")
	var audits []model.AuditLog
	require.NoError(t, db.Where("action = ?", "payment.callback_rejected").Find(&audits).Error)
	assert.Len(t, audits, 1, "every rejected callback is audited; administrator completion is not a callback")
	if len(audits) > 0 {
		assert.Equal(t, u.Id, audits[0].UserId)
		require.NotNil(t, audits[0].Other.Op)
		params, err := common.Marshal(audits[0].Other.Op.Params)
		require.NoError(t, err)
		assert.Contains(t, string(params), `"trade_no":"`+order.TradeNo+`"`)
		assert.Contains(t, string(params), `"provider":"epay"`)
	}
}
