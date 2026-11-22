package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// List readers need preferences, not notification destinations or credentials.
// An allowlisted settings type also keeps unknown future fields out of lists.
func projectUserList(users []*model.User) ([]*model.User, error) {
	projected := make([]*model.User, 0, len(users))
	for _, user := range users {
		copy := *user
		if copy.Setting != "" {
			var setting dto.UserSetting
			if err := common.Unmarshal([]byte(copy.Setting), &setting); err != nil {
				return nil, common.NewMessage("Failed to read user settings")
			}
			setting.WebhookUrl, setting.WebhookSecret = "", ""
			setting.BarkUrl, setting.GotifyUrl, setting.GotifyToken = "", "", ""
			encoded, err := common.Marshal(setting)
			if err != nil {
				return nil, err
			}
			copy.Setting = string(encoded)
		}
		projected = append(projected, &copy)
	}
	return projected, nil
}
