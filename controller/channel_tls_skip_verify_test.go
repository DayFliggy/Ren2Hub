package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func newTLSFixtureChannel(channelType int, baseURL string, skipVerify bool) *model.Channel {
	channel := &model.Channel{Type: channelType, Key: "tls-fixture-key", BaseURL: &baseURL}
	channel.SetSetting(dto.ChannelSettings{TLSInsecureSkipVerify: skipVerify})
	if channelType == constant.ChannelTypeAdvancedCustom {
		channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: dto.AdvancedCustomBalancePath, UpstreamPath: "/balance", Converter: "none",
		}}}})
	}
	return channel
}

func TestChannelUpstreamQueriesHonorTLSInsecureSkipVerify(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"fixture-model"}]}`))
		case "/v1beta/models":
			_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-fixture"}]}`))
		case "/balance":
			// Not a credit summary, so the advanced balance read returns the raw
			// body without writing the balance to the database.
			_, _ = w.Write([]byte(`{"credits":"1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)

	queries := []struct {
		name        string
		channelType int
		run         func(*model.Channel) error
	}{
		{name: "model list", channelType: constant.ChannelTypeOpenAI, run: func(channel *model.Channel) error {
			_, err := fetchChannelUpstreamModelIDs(channel)
			return err
		}},
		{name: "gemini model list", channelType: constant.ChannelTypeGemini, run: func(channel *model.Channel) error {
			_, err := fetchChannelUpstreamModelIDs(channel)
			return err
		}},
		{name: "balance", channelType: constant.ChannelTypeOpenAI, run: func(channel *model.Channel) error {
			_, err := GetResponseBody(http.MethodGet, upstream.URL+"/balance", channel, http.Header{})
			return err
		}},
		{name: "advanced custom balance", channelType: constant.ChannelTypeAdvancedCustom, run: func(channel *model.Channel) error {
			_, err := fetchAdvancedCustomBalance(context.Background(), channel)
			return err
		}},
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			err := query.run(newTLSFixtureChannel(query.channelType, upstream.URL, false))
			require.ErrorContains(t, err, "certificate", "a verifying channel must reject the self-signed upstream")
			require.NoError(t, query.run(newTLSFixtureChannel(query.channelType, upstream.URL, true)))
		})
	}
}
