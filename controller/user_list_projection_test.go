package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestUserListProjectionRejectsMalformedSettingsWithoutSecret(t *testing.T) {
	user := &model.User{Setting: `{"webhook_secret":"fixture-secret",broken`}
	list, err := projectUserList([]*model.User{user})
	require.Error(t, err)
	require.Nil(t, list)
	require.NotContains(t, err.Error(), "fixture-secret")
	require.Contains(t, user.Setting, "fixture-secret")
	user.Setting = `{"language":"en","future_token":"fixture-secret"}`
	list, err = projectUserList([]*model.User{user})
	require.NoError(t, err)
	require.NotContains(t, list[0].Setting, "fixture-secret")
	require.Contains(t, list[0].Setting, `"language":"en"`)
}
