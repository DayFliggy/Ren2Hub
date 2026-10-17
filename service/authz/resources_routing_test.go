package authz

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTokenRoutingUserPermissionDoesNotGrantAdministration(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	require.True(t, Can(7, common.RoleCommonUser, RoutingRead))
	require.True(t, Can(7, common.RoleCommonUser, RoutingWrite))
	for _, permission := range []Permission{ChannelRead, ChannelOperate, ChannelWrite, ChannelSensitiveWrite, ChannelSecretView, AuditRead, TaskPluginBind} {
		require.False(t, Can(7, common.RoleCommonUser, permission))
	}
	require.NoError(t, SetUserPermissions(7, PermissionsMap{ResourceRouting: {ActionRead: true, ActionWrite: false}}))
	require.True(t, Can(7, common.RoleCommonUser, RoutingRead))
	require.False(t, Can(7, common.RoleCommonUser, RoutingWrite))
	require.True(t, Can(1, common.RoleRootUser, RoutingWrite))
}
