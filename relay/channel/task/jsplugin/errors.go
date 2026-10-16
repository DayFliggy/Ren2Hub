package jsplugin

import (
	"github.com/QuantumNous/new-api/common"
	"golang.org/x/net/http/httpproxy"
)

// Polling and artifact hooks also run outside Gin. Keep a safe display message
// at this boundary without losing cancellation or engine-capacity classification.
type upstreamError struct {
	cause   error
	message string
}

func (e *upstreamError) Error() string { return e.message }
func (e *upstreamError) Unwrap() error { return e.cause }

func (a *TaskAdaptor) maskUpstreamError(err error, addresses ...string) error {
	if err == nil {
		return nil
	}
	_, baseURL, proxy := a.queryCredentials()
	environmentProxy := httpproxy.FromEnvironment()
	addresses = append(addresses, baseURL, proxy, environmentProxy.HTTPProxy, environmentProxy.HTTPSProxy)
	addresses = append(addresses, a.plugin.Meta.AllowedHosts...)
	message := common.MaskSensitiveInfo(err.Error(), common.FilterUpstreamAddresses(addresses...)...)
	if message == err.Error() {
		return err
	}
	return &upstreamError{cause: err, message: message}
}
