package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
)

func LoadGmpayConfig(ctx context.Context) (setting.GmpayConfig, error) {
	var option model.Option
	config := setting.GmpayConfig{Assets: []setting.GmpayAsset{}}
	err := model.DB.WithContext(ctx).Where(&model.Option{Key: setting.GmpayConfigKey}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err := common.UnmarshalJsonStr(option.Value, &config); err != nil {
		return config, gmpay.ErrInvalidConfig
	}
	return config, config.Validate()
}

func SaveGmpayConfig(ctx context.Context, config setting.GmpayConfig) error {
	return model.SaveGmpayConfig(ctx, config)
}

func gmpayHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// The public catalog probe checks reachability, never the merchant secret and
// never creates a payment order.
func ProbeGmpayGateway(ctx context.Context, address string) ([]gmpay.SupportedAsset, error) {
	endpoint, err := gmpay.PublicConfigEndpoint(gmpay.Config{GatewayURL: address})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := gmpayHTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, gmpay.ErrGatewayRejected
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, gmpay.MaxPublicConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > gmpay.MaxPublicConfigBytes {
		return nil, gmpay.ErrInvalidParameters
	}
	return gmpay.ParsePublicConfig(body)
}
