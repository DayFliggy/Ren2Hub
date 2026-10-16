package controller

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"golang.org/x/net/http/httpproxy"
)

// Balance queries are display projections, never a source of wallet funds.
// Keep upstream quota exact until the existing channel balance float boundary.
func updateChannelNewAPIBalance(channel *model.Channel) (balance float64, retErr error) {
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	defer func() {
		if retErr == nil {
			return
		}
		proxy := httpproxy.FromEnvironment()
		safe := sanitizeFetchModelsError(retErr, channel.Key)
		message := common.MaskSensitiveInfo(safe.Error(), baseURL, channel.GetSetting().Proxy, proxy.HTTPProxy, proxy.HTTPSProxy)
		// Preserve typed local messages when no private content was removed.
		if message != retErr.Error() {
			retErr = errors.New(message)
		}
	}()
	if baseURL == "" {
		return 0, errors.New("New API channel is missing base_url")
	}
	body, err := GetResponseBody(http.MethodGet, baseURL+"/api/usage/token/", channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	var usage struct {
		Code    bool   `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			TotalAvailable *decimal.Decimal `json:"total_available"`
			UnlimitedQuota *bool            `json:"unlimited_quota"`
			UserQuota      *struct {
				TotalAvailable *decimal.Decimal `json:"total_available"`
			} `json:"user_quota"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &usage); err != nil {
		return 0, err
	}
	if !usage.Code {
		return 0, fmt.Errorf("New API usage query failed: %s", usage.Message)
	}
	if usage.Data == nil || usage.Data.UnlimitedQuota == nil {
		return 0, errors.New("New API usage response is missing quota information")
	}
	remaining := usage.Data.TotalAvailable
	if *usage.Data.UnlimitedQuota {
		remaining = nil
	} else if err := validateChannelQuotaDecimal(remaining); err != nil {
		return 0, fmt.Errorf("New API key quota: %w", err)
	}
	if owner := usage.Data.UserQuota; owner != nil {
		if err := validateChannelQuotaDecimal(owner.TotalAvailable); err != nil {
			return 0, fmt.Errorf("New API user quota: %w", err)
		}
		if remaining == nil || owner.TotalAvailable.LessThan(*remaining) {
			remaining = owner.TotalAvailable
		}
	}
	if remaining == nil {
		return 0, common.NewMessage(`The upstream key has unlimited quota. Turn on "Allow querying user quota" for this key on the upstream to see the real balance.`)
	}
	body, err = GetResponseBody(http.MethodGet, baseURL+"/api/status", channel, nil)
	if err != nil {
		return 0, err
	}
	var status struct {
		Success *bool `json:"success"`
		Data    struct {
			QuotaPerUnit *decimal.Decimal `json:"quota_per_unit"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &status); err != nil {
		return 0, err
	}
	if status.Success != nil && !*status.Success {
		return 0, errors.New("New API status query failed")
	}
	unit := status.Data.QuotaPerUnit
	if err := validateChannelQuotaDecimal(unit); err != nil {
		return 0, fmt.Errorf("New API quota_per_unit: %w", err)
	}
	if !unit.IsPositive() {
		return 0, errors.New("New API quota_per_unit must be positive")
	}
	// Rational division avoids an intermediate Decimal division precision or
	// float conversion changing which balance is smaller.
	balance, _ = new(big.Rat).Quo(remaining.Rat(), unit.Rat()).Float64()
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return 0, errors.New("New API balance must be finite")
	}
	return balance, channel.UpdateBalance(balance)
}

// Bound hostile decimal exponents before comparison or rational expansion.
func validateChannelQuotaDecimal(value *decimal.Decimal) error {
	if value == nil {
		return errors.New("missing decimal value")
	}
	if value.Exponent() < -18 || value.Exponent() > 18 || len(value.Coefficient().String()) > 80 {
		return errors.New("decimal value is out of range")
	}
	return nil
}
