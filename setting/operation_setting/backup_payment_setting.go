package operation_setting

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// BackupPaymentLink is an external page the wallet lists beside the online
// payment methods, such as a shop that sells redemption codes.
type BackupPaymentLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Icon is an optional https image or built-in icon identifier, as for GMPay assets.
	Icon string `json:"icon"`
}

type BackupPaymentSetting struct {
	Links []BackupPaymentLink `json:"links"`
}

const (
	BackupPaymentLinksOptionKey = "backup_payment_setting.links"
	MaxBackupPaymentLinks       = 10
	maxBackupPaymentNameRunes   = 32
	maxBackupPaymentIconBytes   = 256
)

var backupPaymentSetting = BackupPaymentSetting{Links: []BackupPaymentLink{}}

func init() {
	config.GlobalConfig.Register("backup_payment_setting", &backupPaymentSetting)
}

// ValidateBackupPaymentLinks checks an administrator's list before it is saved.
func ValidateBackupPaymentLinks(value string) error {
	var links []BackupPaymentLink
	if err := common.UnmarshalJsonStr(value, &links); err != nil || links == nil {
		return common.NewMessage("Backup payment links must be a JSON array")
	}
	if len(links) > MaxBackupPaymentLinks {
		return common.NewMessage("At most 10 backup payment links are allowed")
	}
	for _, link := range links {
		name := strings.TrimSpace(link.Name)
		if name == "" || utf8.RuneCountInString(name) > maxBackupPaymentNameRunes {
			return common.NewMessage("Each backup payment link needs a name of at most 32 characters")
		}
		if !isWebURL(link.URL) {
			return common.NewMessage("Each backup payment link must be an http or https URL")
		}
		if len(link.Icon) > maxBackupPaymentIconBytes {
			return common.NewMessage("A backup payment icon cannot exceed 256 characters")
		}
	}
	return nil
}

// WalletBackupPaymentLinks is the list the wallet shows. The upstream
// TopUpLink has no name and comes first; like the online methods, nothing is
// offered before payment compliance is confirmed.
func WalletBackupPaymentLinks(complianceConfirmed bool, topUpLink string) []BackupPaymentLink {
	links := []BackupPaymentLink{}
	if !complianceConfirmed {
		return links
	}
	if strings.TrimSpace(topUpLink) != "" {
		links = append(links, BackupPaymentLink{URL: strings.TrimSpace(topUpLink)})
	}
	return append(links, backupPaymentSetting.Links...)
}

func isWebURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
