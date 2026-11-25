package authz

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementResourcePermissions(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	assert.True(t, Can(1, common.RoleRootUser, AnnouncementRead))
	assert.True(t, Can(1, common.RoleRootUser, AnnouncementWrite))
	assert.True(t, Can(2, common.RoleAdminUser, AnnouncementRead))
	assert.True(t, Can(2, common.RoleAdminUser, AnnouncementWrite))
	assert.False(t, Can(3, common.RoleCommonUser, AnnouncementRead))
	assert.False(t, Can(3, common.RoleCommonUser, AnnouncementWrite))
}
