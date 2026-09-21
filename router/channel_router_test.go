package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelDefaultBaseURLsRequireReadPermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/default_base_urls", authz.ChannelRead, controller.GetChannelDefaultBaseURLs)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelRoutes(engine.Group("/api"))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestChannelStatusRoutesUseExpectedPermissions(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/:id/vllm/status", authz.ChannelRead, controller.GetVLLMChannelStatus)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/sglang/status", authz.ChannelRead, controller.GetSGLangChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/:id/status", authz.ChannelOperate, controller.UpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/status/batch", authz.ChannelOperate, controller.BatchUpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
}

func TestChannelDeleteRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodDelete, "/:id", authz.ChannelSensitiveWrite, controller.DeleteChannel)
	assertChannelRoutePermission(t, http.MethodPost, "/batch", authz.ChannelSensitiveWrite, controller.DeleteChannelBatch)
	assertChannelRoutePermission(t, http.MethodDelete, "/disabled", authz.ChannelSensitiveWrite, controller.DeleteDisabledChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/tag", authz.ChannelWrite, controller.EditTagChannels)
	assertChannelRoutePermission(t, http.MethodPost, "/batch/tag", authz.ChannelWrite, controller.BatchSetChannelTag)
}

func TestChannelStatusRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")

	require.NotPanics(t, func() {
		registerChannelRoutes(api)
	})
}

func assertChannelRoutePermission(t *testing.T, method string, path string, permission authz.Permission, handler any) {
	t.Helper()
	for _, route := range channelPermissionRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, permission, route.permission)
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}

func TestAdministratorManagementAndChannelSecretAccess(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMaster, previousRedis := common.IsMasterNode, common.RedisEnabled
	previousLimit, previousCritical := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuthFlow{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.UserOAuthBinding{}, &model.AuditLog{}, &model.Channel{}, &model.Option{}, &model.CasbinRule{}, &model.AuthzRole{}, &model.CustomOAuthProvider{}, &model.SystemInstance{}, &model.SystemTask{}, &model.TaskPlugin{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.IsMasterNode, common.RedisEnabled = true, false
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.IsMasterNode, common.RedisEnabled = previousMaster, previousRedis
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousLimit, previousCritical
		require.NoError(t, sqlDB.Close())
	})
	channel := model.Channel{Name: "private channel", Type: 1, Key: "admin-channel-secret", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	router := gin.New()
	SetApiRouter(router)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(fmt.Sprint(role), func(t *testing.T) {
			pat := fmt.Sprintf("management-pat-%d", role)
			user := model.User{Username: fmt.Sprintf("management-%d", role), AffCode: fmt.Sprintf("management-%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, Secret: "test-factor", IsEnabled: true}).Error)
			bundle, err := service.CreateLoginSession(user.Id, "2fa", "127.0.0.1", "management-test")
			require.NoError(t, err)
			for _, credential := range []string{pat, bundle.AccessToken} {
				for _, path := range []string{"/api/option/", "/api/option/request_policy", "/api/custom-oauth-provider/", "/api/performance/stats", "/api/ratio_sync/channels", "/api/plugin/task/runtime/status", "/api/system-info/instances", "/api/system-task/current?type=log_cleanup", "/api/task_plugin_options", "/api/audit"} {
					request := httptest.NewRequest(http.MethodGet, path, nil)
					request.Header.Set("Authorization", "Bearer "+credential)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					wantStatus := http.StatusOK
					if role < common.RoleAdminUser {
						wantStatus = http.StatusForbidden
					}
					require.Equal(t, wantStatus, response.Code, "%s: %s", path, response.Body.String())
					var result struct{ Success bool }
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.Equal(t, role >= common.RoleAdminUser, result.Success, "%s: %s", path, response.Body.String())
				}
			}
			identity, err := service.ParseAccessToken(bundle.AccessToken)
			require.NoError(t, err)
			_, err = service.GetVerificationRequirements(identity, service.VerificationScopeChannelKeyRead)
			if role < common.RoleAdminUser {
				assert.ErrorIs(t, err, service.ErrVerificationForbidden)
				return
			}
			require.NoError(t, err)
			operation := service.VerificationOperation{Scope: service.VerificationScopeChannelKeyRead, Context: []byte(fmt.Sprintf(`{"channel_id":%d}`, channel.Id))}
			binding, err := service.BindVerificationOperation(operation)
			require.NoError(t, err)
			expired, _, err := service.IssueSecurityProof(identity, "2fa", binding)
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.AuthFlow{}).Where("user_id = ? AND purpose = ?", user.Id, model.AuthFlowPurposeSecurityProof).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			proof, _, err := service.IssueSecurityProof(identity, "2fa", binding)
			require.NoError(t, err)
			for _, test := range []struct {
				name, credential, proof, code string
				status                        int
			}{
				{"missing proof", bundle.AccessToken, "", "SECURITY_PROOF_REQUIRED", http.StatusForbidden},
				{"expired proof", bundle.AccessToken, expired, "SECURITY_PROOF_EXPIRED", http.StatusForbidden},
				{"PAT cannot use session proof", pat, proof, "SECURITY_PROOF_INVALID", http.StatusForbidden},
				{"authorized", bundle.AccessToken, proof, "", http.StatusOK},
				{"replay", bundle.AccessToken, proof, "SECURITY_PROOF_CONSUMED", http.StatusForbidden},
			} {
				t.Run(test.name, func(t *testing.T) {
					request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/channel/%d/key", channel.Id), nil)
					request.Header.Set("Authorization", "Bearer "+test.credential)
					request.Header.Set("X-Security-Proof", test.proof)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					require.Equal(t, test.status, response.Code, response.Body.String())
					var result struct {
						Code string
						Data struct{ Key string }
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.Equal(t, test.code, result.Code)
					if test.status == http.StatusOK {
						assert.Equal(t, channel.Key, result.Data.Key)
					} else {
						assert.NotContains(t, response.Body.String(), channel.Key)
					}
					assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
				})
			}
			if role != common.RoleAdminUser {
				return
			}
			proof, _, err = service.IssueSecurityProof(identity, "2fa", binding)
			require.NoError(t, err)
			require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionSecretView: false}}))
			_, err = service.GetVerificationRequirements(identity, service.VerificationScopeChannelKeyRead)
			assert.ErrorIs(t, err, service.ErrVerificationForbidden)
			_, err = service.ConsumeOperationProof(proof, identity, operation)
			assert.ErrorIs(t, err, service.ErrVerificationForbidden)
			request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/channel/%d/key", channel.Id), nil)
			request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
			request.Header.Set("X-Security-Proof", proof)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusForbidden, response.Code)
			assert.NotContains(t, response.Body.String(), channel.Key)

			for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
				target := model.User{Username: fmt.Sprintf("protected-%d", role), AffCode: fmt.Sprintf("protected-%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1}
				require.NoError(t, db.Create(&target).Error)
				request := httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(fmt.Sprintf(`{"id":%d,"action":"disable"}`, target.Id)))
				request.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				var result struct{ Success bool }
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				assert.False(t, result.Success)
				require.NoError(t, db.First(&target, target.Id).Error)
				assert.Equal(t, common.UserStatusEnabled, target.Status)
			}
		})
	}
	var logs []model.AuditLog
	require.NoError(t, db.Find(&logs).Error)
	encoded, err := common.Marshal(logs)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), channel.Key)
	assert.NotContains(t, string(encoded), "management-pat-")
}
