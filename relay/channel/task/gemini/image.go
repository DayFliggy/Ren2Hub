package gemini

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxVeoImageSize = 20 * 1024 * 1024 // 20 MB

// ExtractMultipartImage reads the first `input_reference` file from a multipart
// form upload and returns a VeoImageInput. Returns nil if no file is present.
func ExtractMultipartImage(c *gin.Context) (*VeoImageInput, error) {
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		return nil, nil
	}
	mf, err := c.MultipartForm()
	if err != nil {
		return nil, fmt.Errorf("read Veo multipart image: %w", err)
	}
	files, exists := mf.File["input_reference"]
	if !exists || len(files) == 0 {
		return nil, nil
	}
	if len(files) != 1 {
		return nil, fmt.Errorf("Veo task submission supports one input image")
	}
	fh := files[0]
	if fh.Size > maxVeoImageSize {
		return nil, fmt.Errorf("Veo input image exceeds %d bytes", maxVeoImageSize)
	}
	file, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("open Veo input image: %w", err)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(file, maxVeoImageSize+1))
	if err != nil {
		return nil, fmt.Errorf("read Veo input image: %w", err)
	}
	return validateVeoImage(fileBytes, fh.Header.Get("Content-Type"))
}

// ParseImageInput parses an image string (data URI or raw base64) into a
// VeoImageInput. Invalid or unsupported inputs cannot become text-only requests.
func ParseImageInput(imageStr string) (*VeoImageInput, error) {
	imageStr = strings.TrimSpace(imageStr)
	if imageStr == "" {
		return nil, fmt.Errorf("Veo input image is empty")
	}
	mimeType := ""
	if strings.HasPrefix(imageStr, "data:") {
		metadata, payload, found := strings.Cut(imageStr[len("data:"):], ",")
		media, encoded := strings.CutSuffix(metadata, ";base64")
		if !found || !encoded {
			return nil, fmt.Errorf("Veo input image must use a base64 data URI")
		}
		var err error
		mimeType, _, err = mime.ParseMediaType(media)
		if err != nil {
			return nil, fmt.Errorf("invalid Veo image media type")
		}
		imageStr = payload
	}
	if len(imageStr) > base64.StdEncoding.EncodedLen(maxVeoImageSize) {
		return nil, fmt.Errorf("Veo input image exceeds %d bytes", maxVeoImageSize)
	}
	raw, err := base64.StdEncoding.DecodeString(imageStr)
	if err != nil {
		return nil, fmt.Errorf("Veo input image must be valid base64 or a data URI")
	}
	return validateVeoImage(raw, mimeType)
}

func validateVeoImage(data []byte, declaredType string) (*VeoImageInput, error) {
	if len(data) == 0 || len(data) > maxVeoImageSize {
		return nil, fmt.Errorf("Veo input image must contain 1 to %d bytes", maxVeoImageSize)
	}
	detectedType := http.DetectContentType(data)
	if !strings.HasPrefix(detectedType, "image/") {
		return nil, fmt.Errorf("Veo input does not contain a recognized image")
	}
	if declaredType != "" && declaredType != "application/octet-stream" && declaredType != detectedType {
		return nil, fmt.Errorf("Veo image content does not match its media type")
	}
	return &VeoImageInput{
		BytesBase64Encoded: base64.StdEncoding.EncodeToString(data),
		MimeType:           detectedType,
	}, nil
}
