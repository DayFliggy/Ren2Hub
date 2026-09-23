package setting

// GMPay multi-chain crypto checkout configuration. The gateway is enabled once
// the base address, merchant id and secret are populated, mirroring Stripe and
// Creem: there is no separate Enabled flag. Assets bind a display name to one
// concrete token and network so an order can record the requested asset.
var (
	GmpayPayAddress string
	GmpayId         string
	GmpayKey        string
	GmpayAssets     string  = "[]"
	GmpayPrice      float64 = 1.0
	GmpayMinTopUp   int     = 1
	GmpayGroupRatio string  = `{"default":1}`
)
