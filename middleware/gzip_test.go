package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

type trackedCompressedBody struct {
	*bytes.Reader
	closed bool
}

func (body *trackedCompressedBody) Close() error {
	body.closed = true
	return nil
}

func TestZstdRequestDecompressionContract(t *testing.T) {
	oldLimit := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = oldLimit })
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()

	for _, test := range []struct {
		name    string
		payload []byte
		encoded []byte
		status  int
	}{
		{name: "valid request", payload: []byte(`{"model":"example","messages":[]}`), status: http.StatusOK},
		{name: "invalid frame", encoded: []byte("invalid zstd frame"), status: http.StatusBadRequest},
		{name: "decoded size limit", payload: bytes.Repeat([]byte("x"), (1<<20)+1), status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded := test.encoded
			if encoded == nil {
				encoded = encoder.EncodeAll(test.payload, nil)
			}
			body := &trackedCompressedBody{Reader: bytes.NewReader(encoded)}
			engine := gin.New()
			engine.Use(DecompressRequestMiddleware())
			engine.POST("/request", func(c *gin.Context) {
				defer c.Request.Body.Close()
				decoded, readErr := io.ReadAll(c.Request.Body)
				if readErr != nil {
					var sizeError *http.MaxBytesError
					if errors.As(readErr, &sizeError) {
						c.Status(http.StatusRequestEntityTooLarge)
					} else {
						c.Status(http.StatusBadRequest)
					}
					return
				}
				if c.GetHeader("Content-Encoding") != "" {
					t.Error("decoded request retains Content-Encoding")
				}
				c.Data(http.StatusOK, "application/json", decoded)
			})
			request := httptest.NewRequest(http.MethodPost, "/request", body)
			request.Header.Set("Content-Encoding", "zstd")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if test.status == http.StatusOK && !bytes.Equal(response.Body.Bytes(), test.payload) {
				t.Fatal("decoded request differs from original payload")
			}
			if !body.closed {
				t.Fatal("original compressed request body was not closed")
			}
		})
	}
}
