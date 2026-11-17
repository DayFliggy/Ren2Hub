package operation_setting

import (
	"net/url"
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

type BackupPaymentSetting struct {
	Links []BackupPaymentLink `json:"links"`
}

const (
	BackupPaymentLinksOptionKey = "backup_payment_setting.links"
	MaxBackupPaymentLinks       = 10
	maxBackupPaymentNameRunes   = 32
	maxBackupPaymentIconBytes   = 256
	maxBackupPaymentURLBytes    = 2048
)

// backupPaymentSetting is registered only so the option map starts with the
// default empty list. Saved values never reach it: model/option.go hands the
// key to LoadBackupPaymentLinks, as upstream does for tool prices, because the
// shared loader would decode into the old elements and drop errors silently.
var backupPaymentSetting = BackupPaymentSetting{Links: []BackupPaymentLink{}}

// walletBackupPaymentLinks is what wallet requests read. It is replaced as a
// whole when a valid list is loaded and never edited in place.
var walletBackupPaymentLinks atomic.Pointer[[]BackupPaymentLink]

func init() {
	walletBackupPaymentLinks.Store(&[]BackupPaymentLink{})
	config.GlobalConfig.Register("backup_payment_setting", &backupPaymentSetting)
}

// LoadBackupPaymentLinks applies a saved or synced value with the same checks
// as a save. A value that fails them (imported, or written by an older
// version) is logged and dropped, and the previous valid list stays in effect.
func LoadBackupPaymentLinks(value string) {
	links, err := parseBackupPaymentLinks([]byte(value))
	if err != nil {
		common.SysError(common.LogText("ignoring invalid %s: %s", BackupPaymentLinksOptionKey, err.Error()))
		return
	}
	walletBackupPaymentLinks.Store(&links)
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
	for i := range links {
		link := &links[i]
		// The wallet shows the checked name; the option keeps the submitted text.
		link.Name = strings.TrimSpace(link.Name)
		if link.Name == "" || utf8.RuneCountInString(link.Name) > maxBackupPaymentNameRunes {
			return nil, common.NewMessage("Each backup payment link needs a name of at most 32 characters")
		}
		if !isWebURL(link.URL) {
			return nil, common.NewMessage("Each backup payment link must be an http or https URL")
		}
		if len(link.URL) > maxBackupPaymentURLBytes {
			return nil, common.NewMessage("A backup payment link cannot exceed 2048 bytes")
		}
		if hasURLUserinfo(link.URL) {
			return nil, common.NewMessage("A backup payment link cannot contain a user name or password")
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

// hasURLUserinfo applies to backup links only; the upstream TopUpLink keeps
// its own projection rules.
func hasURLUserinfo(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.User != nil
}

func isWebURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
