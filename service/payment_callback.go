package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// paymentCallbackURLs is shared by the EPay and GMPay order snapshots. A custom
// callback changes notification delivery only; the browser returns to the site.
func paymentCallbackURLs(serverAddress, customCallbackAddress, notifyPath string) (string, string, error) {
	serverOrigin, err := common.NormalizePaymentOrigin(serverAddress)
	if err != nil {
		return "", "", err
	}
	callbackOrigin := serverOrigin
	if strings.TrimSpace(customCallbackAddress) != "" {
		callbackOrigin, err = common.NormalizePaymentOrigin(customCallbackAddress)
		if err != nil {
			return "", "", err
		}
	}
	return callbackOrigin + notifyPath, serverOrigin + "/usage-logs", nil
}
