//go:build known_upstream_restored

package controller

import (
	"context"
	"github.com/QuantumNous/new-api/model"
)

func restoredControlFetchModels(ctx context.Context, ch *model.Channel) ([]string, error) {
	return fetchChannelUpstreamModelIDs(ch)
}
