package media

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeTextRejectsPartialOrNonUTF8Content(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		ok                bool
	}{
		{name: "utf8", input: " 5L2g5aW9\n", want: "你好", ok: true},
		{name: "line breaks", input: "aGVs\r\nbG8=", want: "hello", ok: true},
		{name: "bad trailing data", input: "aGVsbG8=!"},
		{name: "binary", input: "/w=="},
		{name: "truncated", input: "aGVsbG8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DecodeText(tc.input)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func BenchmarkDecodeText(b *testing.B) {
	input := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("plain text\n", 6400)))
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := DecodeText(input); !ok {
			b.Fatal("valid text was rejected")
		}
	}
}
