package service

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/model"
)

type channelRequestBody struct {
	io.ReadCloser
	permit *ConcurrencyPermit
}

func (b *channelRequestBody) Close() error {
	defer b.permit.Release()
	return b.ReadCloser.Close()
}

// DoChannelRequest gives a background or auxiliary HTTP request one immediate
// channel permit. The caller must close the response body, including on errors.
func DoChannelRequest(ctx context.Context, channel *model.Channel, send func() (*http.Response, error)) (*http.Response, error) {
	permit, err := AcquireChannelConcurrency(ctx, channel)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			permit.Release()
		}
	}()
	response, err := send()
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("upstream returned an empty response")
	}
	response.Body = &channelRequestBody{ReadCloser: response.Body, permit: permit}
	transferred = true
	return response, nil
}
