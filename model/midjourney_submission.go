package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// GetOwnedMidjourneyTask rejects ambiguous provider IDs instead of selecting a
// different account's job. Legacy rows without a Token owner stay console-only.
func GetOwnedMidjourneyTask(ctx context.Context, userID, tokenID int, providerID string) (*Midjourney, error) {
	if userID <= 0 || tokenID <= 0 || providerID == "" {
		return nil, ErrGatewayStateUnknown
	}
	var rows []Midjourney
	if err := DB.WithContext(ctx).Where("user_id = ? AND mj_id = ?", userID, providerID).Find(&rows).Error; err != nil {
		return nil, err
	}
	var owner *Midjourney
	for i := range rows {
		row := &rows[i]
		ownerToken := row.PrivateData.TokenId
		if ownerToken == 0 && row.PrivateData.CredentialVersion == 0 {
			ownerToken = row.TokenId
		}
		if ownerToken != tokenID {
			continue
		}
		if owner != nil && (owner.ChannelId != row.ChannelId || owner.PrivateData.CredentialID != row.PrivateData.CredentialID || owner.PrivateData.EndpointID != row.PrivateData.EndpointID) {
			return nil, ErrGatewayStateUnknown
		}
		owner = row
	}
	if owner == nil {
		return nil, ErrGatewayStateUnknown
	}
	return owner, nil
}

func ResolveMidjourneyCredential(ctx context.Context, channel *Channel, task *Midjourney) (string, int, error) {
	if task == nil || task.PrivateData.CredentialVersion != 1 {
		return "", 0, ErrGatewayStateUnavailable
	}
	return ResolveTaskCredential(ctx, channel, &Task{ChannelId: task.ChannelId, PrivateData: task.PrivateData})
}

func MidjourneySubmissionReady(ctx context.Context, task *Midjourney) (bool, error) {
	if task == nil {
		return false, errors.New("missing Midjourney task")
	}
	return TaskSubmissionReady(ctx, &Task{UserId: task.UserId, PrivateData: task.PrivateData})
}

// PersistAcceptedMidjourney commits accepted ownership and settlement intent
// together; a later accounting failure never causes another upstream submission.
func PersistAcceptedMidjourney(task *Midjourney, requestID string, consumption BillingConsumption) error {
	if DB == nil || task == nil || task.MjId == "" || requestID == "" || task.PrivateData.SubmissionRequestID != requestID ||
		task.PrivateData.CredentialVersion != 1 || task.PrivateData.CredentialID == "" || task.PrivateData.EndpointID == "" ||
		task.UserId != consumption.Log.UserId || task.ChannelId != consumption.Log.ChannelId ||
		task.PrivateData.TokenId != consumption.Log.TokenId || task.Quota != consumption.Log.Quota {
		return errors.New("invalid accepted Midjourney task")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := prepareTaskAcceptedBillingTx(tx, requestID, consumption); err != nil {
			return err
		}
		// Serializes concurrent bindings for the same account, including the
		// first row, where a provider-ID lookup cannot lock a nonexistent row.
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, task.UserId).Error; err != nil {
			return err
		}

		var rows []Midjourney
		if err := tx.Where("user_id = ? AND mj_id = ?", task.UserId, task.MjId).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.PrivateData.TokenId == task.PrivateData.TokenId && (row.ChannelId != task.ChannelId || row.PrivateData.CredentialID != task.PrivateData.CredentialID || row.PrivateData.EndpointID != task.PrivateData.EndpointID) {
				return ErrGatewayStateUnknown
			}
			if row.PrivateData.SubmissionRequestID != requestID {
				continue
			}
			if row.Quota != task.Quota || row.PrivateData.TokenId != task.PrivateData.TokenId || row.PrivateData.CredentialID != task.PrivateData.CredentialID || row.PrivateData.EndpointID != task.PrivateData.EndpointID {
				return ErrBillingRecoveryStateConflict
			}
			task.Id = row.Id
			return nil
		}
		return tx.Create(task).Error
	})
}
