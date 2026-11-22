package model

// Verified entry points are used by signed payment callbacks; the native facades
// retain their internal compatibility contract. Both use the same settlement.

func RechargeVerified(referenceId string, customerId string, callerIp string, receipt TopUpReceipt) error {
	return rechargeStripe(referenceId, customerId, callerIp, &receipt)
}

func RechargeCreemVerified(referenceId string, customerEmail string, customerName string, callerIp string, receipt TopUpReceipt) error {
	return rechargeCreem(referenceId, customerEmail, customerName, callerIp, &receipt)
}

func RechargeWaffoVerified(tradeNo string, callerIp string, receipt TopUpReceipt) error {
	return rechargeWaffo(tradeNo, callerIp, &receipt)
}

func RechargeWaffoPancakeVerified(tradeNo string, receipt TopUpReceipt) error {
	return rechargeWaffoPancake(tradeNo, &receipt)
}
