package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentOriginPreservesHTTPIPAndPort(t *testing.T) {
	for _, test := range []struct{ input, expected string }{
		{"http://37.114.34.132:13000/", "http://37.114.34.132:13000"},
		{"https://37.114.34.132/", "https://37.114.34.132"},
		{" https://Wallet.Example.test:443/ ", "https://wallet.example.test"},
		{"http://localhost:80/", "http://localhost"},
		{"http://[::1]:13000/", "http://[::1]:13000"},
	} {
		t.Run(test.input, func(t *testing.T) {
			origin, err := NormalizePaymentOrigin(test.input)
			require.NoError(t, err)
			assert.Equal(t, test.expected, origin)
		})
	}
}

func TestPaymentOriginRejectsComponentsAndInvalidPorts(t *testing.T) {
	for _, input := range []string{
		"", "wallet.example.test", "ftp://wallet.example.test", "https://wallet.example.test/checkout",
		"https://wallet.example.test/?a=1", "https://wallet.example.test/?", "https://wallet.example.test/#fragment", "https://wallet.example.test/#",
		"https://operator:secret@wallet.example.test", "https://operator:secret@[broken", "https://wallet.example.test:0", "https://wallet.example.test:65536",
		"https://wallet.example.test:", "https://[not-an-ip]:13000", "https://wallet.example.test\r\n",
	} {
		t.Run(input, func(t *testing.T) {
			origin, err := NormalizePaymentOrigin(input)
			require.Error(t, err)
			assert.Empty(t, origin)
			assert.EqualError(t, err, "invalid payment origin")
		})
	}
}
