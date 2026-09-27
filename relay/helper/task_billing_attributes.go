package helper

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// FreezeTaskBillingAttributes reads only the final adapter payload. JSON
// parameters and multipart scalar fields are projected through the same allowlist.
func FreezeTaskBillingAttributes(info *relaycommon.RelayInfo, contentType string, body []byte, inherited ...map[string]interface{}) error {
	var fields map[string]interface{}
	if strings.HasPrefix(contentType, "multipart/") && !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		_, parameters, err := mime.ParseMediaType(contentType)
		if err != nil || parameters["boundary"] == "" {
			return fmt.Errorf("invalid task multipart billing attributes")
		}
		fields = map[string]interface{}{}
		reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("invalid task multipart billing attributes")
			}
			if part.FileName() != "" {
				_ = part.Close()
				continue
			}
			switch part.FormName() {
			case "service_tier", "quality", "size", "resolution", "n", "quantity", "seconds", "duration":
				value, err := io.ReadAll(io.LimitReader(part, 129))
				if err != nil || len(value) > 128 {
					return fmt.Errorf("invalid task billing attribute")
				}
				if _, exists := fields[part.FormName()]; exists {
					return fmt.Errorf("duplicate task billing attribute")
				}
				fields[part.FormName()] = string(value)
			}
			_ = part.Close()
		}
	} else {
		if err := common.DecodeJsonNumbers(bytes.NewReader(body), &fields); err != nil {
			return fmt.Errorf("invalid task JSON billing attributes")
		}
		if parameters, ok := fields["parameters"].(map[string]interface{}); ok {
			// These aliases are the final Veo and DashScope protocol fields.
			for _, pair := range [][2]string{{"durationSeconds", "duration"}, {"sampleCount", "quantity"}, {"duration", "duration"}, {"size", "resolution"}, {"resolution", "resolution"}, {"n", "quantity"}} {
				if value, exists := parameters[pair[0]]; exists {
					fields[pair[1]] = value
				}
			}
		}
	}
	data, err := common.Marshal(fields)
	if err != nil {
		return err
	}
	if err := FreezeOutboundBillingAttributes(info, data); err != nil {
		return err
	}
	// Remix providers inherit omitted media properties from the accepted source
	// job. Final explicit parameters always win, and only media facts inherit.
	if len(inherited) != 0 {
		for _, name := range []string{"duration", "resolution", "quantity", "quality"} {
			if _, exists := info.BillingRequestInput.Attributes[name]; exists {
				continue
			}
			if value, exists := inherited[0][name]; exists {
				info.BillingRequestInput.Attributes[name] = value
			}
		}
	}
	return billingexpr.ValidateAttributes(info.BillingRequestInput.Attributes)
}
