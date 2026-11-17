package operation_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

func TestValidateBackupPaymentLinks(t *testing.T) {
	tooMany := "[" + strings.TrimSuffix(strings.Repeat(`{"name":"Shop","url":"https://shop.example.com"},`, 11), ",") + "]"
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"empty list", `[]`, true},
		{"named web links", `[{"name":"发卡网","url":"https://shop.example.com/codes"},{"name":"Store","url":"http://store.example.com"}]`, true},
		{"null", `null`, false},
		{"object", `{"name":"Shop","url":"https://shop.example.com"}`, false},
		{"eleven links", tooMany, false},
		{"blank name", `[{"name":"  ","url":"https://shop.example.com"}]`, false},
		{"long name", `[{"name":"` + strings.Repeat("店", 33) + `","url":"https://shop.example.com"}]`, false},
		{"script url", `[{"name":"Shop","url":"javascript:alert(1)"}]`, false},
		{"relative url", `[{"name":"Shop","url":"/wallet"}]`, false},
		{"icon", `[{"name":"Shop","url":"https://shop.example.com","icon":"telegram"}]`, true},
		{"long icon", `[{"name":"Shop","url":"https://shop.example.com","icon":"` + strings.Repeat("a", 257) + `"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBackupPaymentLinks(tc.value)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestWalletBackupPaymentLinks(t *testing.T) {
	saved := backupPaymentSetting.Links
	t.Cleanup(func() { backupPaymentSetting.Links = saved })
	if err := config.UpdateConfigFromMap(&backupPaymentSetting, map[string]string{
		"links": `[{"name":"Store","url":"https://store.example.com"}]`,
	}); err != nil {
		t.Fatal(err)
	}

	hidden, err := common.Marshal(WalletBackupPaymentLinks(false, "https://codes.example.com"))
	if err != nil || string(hidden) != "[]" {
		t.Fatalf("unconfirmed compliance must publish an empty list, got %s (%v)", hidden, err)
	}
	links := WalletBackupPaymentLinks(true, " https://codes.example.com ")
	want := []BackupPaymentLink{
		{URL: "https://codes.example.com"},
		{Name: "Store", URL: "https://store.example.com"},
	}
	if len(links) != len(want) || links[0] != want[0] || links[1] != want[1] {
		t.Fatalf("links = %+v, want %+v", links, want)
	}
	if got := WalletBackupPaymentLinks(true, ""); len(got) != 1 || got[0] != want[1] {
		t.Fatalf("without TopUpLink only the saved links remain, got %+v", got)
	}
}
