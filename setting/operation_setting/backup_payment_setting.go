package operation_setting

import (
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
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

// BackupPaymentLinks is the saved list. The shared config loader applies a
// stored value with json.Unmarshal into this field, which on a plain slice
// would reuse the old elements and keep an icon the new list omits.
type BackupPaymentLinks []BackupPaymentLink

type BackupPaymentSetting struct {
	Links BackupPaymentLinks `json:"links"`
}

const (
	BackupPaymentLinksOptionKey = "backup_payment_setting.links"
	MaxBackupPaymentLinks       = 10
	maxBackupPaymentNameRunes   = 32
	maxBackupPaymentIconBytes   = 256
)

var backupPaymentSetting = BackupPaymentSetting{Links: BackupPaymentLinks{}}

// walletBackupPaymentLinks is what wallet requests read. It is replaced as a
// whole when a valid list is applied and never edited in place.
var walletBackupPaymentLinks atomic.Pointer[[]BackupPaymentLink]

func init() {
	walletBackupPaymentLinks.Store(&[]BackupPaymentLink{})
	config.GlobalConfig.Register("backup_payment_setting", &backupPaymentSetting)
}

// UnmarshalJSON applies a stored list with the same checks as a save. A value
// that never passed them (imported, or written by an older version) is logged
// and dropped, and the previous valid list stays in effect.
func (l *BackupPaymentLinks) UnmarshalJSON(data []byte) error {
	links, err := parseBackupPaymentLinks(data)
	if err != nil {
		common.SysError(common.LogText("ignoring invalid %s: %s", BackupPaymentLinksOptionKey, err.Error()))
		return err
	}
	*l = links
	snapshot := slices.Clone(links)
	walletBackupPaymentLinks.Store(&snapshot)
	return nil
}

// ValidateBackupPaymentLinks checks an administrator's list before it is saved.
func ValidateBackupPaymentLinks(value string) error {
	_, err := parseBackupPaymentLinks([]byte(value))
	return err
}

func parseBackupPaymentLinks(data []byte) ([]BackupPaymentLink, error) {
	var links []BackupPaymentLink
	if err := common.Unmarshal(data, &links); err != nil || links == nil {
		return nil, common.NewMessage("Backup payment links must be a JSON array")
	}
	if len(links) > MaxBackupPaymentLinks {
		return nil, common.NewMessage("At most 10 backup payment links are allowed")
	}
	for _, link := range links {
		name := strings.TrimSpace(link.Name)
		if name == "" || utf8.RuneCountInString(name) > maxBackupPaymentNameRunes {
			return nil, common.NewMessage("Each backup payment link needs a name of at most 32 characters")
		}
		if !isWebURL(link.URL) {
			return nil, common.NewMessage("Each backup payment link must be an http or https URL")
		}
		if len(link.Icon) > maxBackupPaymentIconBytes {
			return nil, common.NewMessage("A backup payment icon cannot exceed 256 characters")
		}
	}
	return links, nil
}

// WalletBackupPaymentLinks is the list the wallet shows. The upstream
// TopUpLink has no name and comes first; it is saved without a URL check, so
// it is listed only when it is a web link. Like the online methods, nothing
// is offered before payment compliance is confirmed.
func WalletBackupPaymentLinks(complianceConfirmed bool, topUpLink string) []BackupPaymentLink {
	links := []BackupPaymentLink{}
	if !complianceConfirmed {
		return links
	}
	if isWebURL(topUpLink) {
		links = append(links, BackupPaymentLink{URL: strings.TrimSpace(topUpLink)})
	}
	return append(links, *walletBackupPaymentLinks.Load()...)
}

func isWebURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
