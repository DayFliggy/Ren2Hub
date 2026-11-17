package operation_setting

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
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
	useBackupPaymentLinks(t, `[{"name":"Store","url":"https://store.example.com"}]`)

	hidden, err := common.Marshal(WalletBackupPaymentLinks(false, "https://codes.example.com"))
	if err != nil || string(hidden) != "[]" {
		t.Fatalf("unconfirmed compliance must publish an empty list, got %s (%v)", hidden, err)
	}
	links := WalletBackupPaymentLinks(true, " https://codes.example.com ")
	want := []BackupPaymentLink{
		{URL: "https://codes.example.com"},
		{Name: "Store", URL: "https://store.example.com"},
	}
	if !slices.Equal(links, want) {
		t.Fatalf("links = %+v, want %+v", links, want)
	}
	if got := WalletBackupPaymentLinks(true, ""); !slices.Equal(got, want[1:]) {
		t.Fatalf("without TopUpLink only the saved links remain, got %+v", got)
	}
	if got := WalletBackupPaymentLinks(true, "javascript:alert(1)"); !slices.Equal(got, want[1:]) {
		t.Fatalf("a TopUpLink that is not a web link must not be listed, got %+v", got)
	}
}

func TestBackupPaymentLinksReplaceTheWholeList(t *testing.T) {
	useBackupPaymentLinks(t, `[{"name":"Old shop","url":"https://old.example","icon":"https://old.example/track.png"},{"name":"Bot","url":"https://t.me/bot","icon":"telegram"}]`)
	applyBackupPaymentLinks(t, `[{"name":"Bot","url":"https://t.me/bot"},{"name":"New shop","url":"https://new.example"}]`)

	want := []BackupPaymentLink{{Name: "Bot", URL: "https://t.me/bot"}, {Name: "New shop", URL: "https://new.example"}}
	if got := WalletBackupPaymentLinks(true, ""); !slices.Equal(got, want) {
		t.Fatalf("an icon the new list omits must be cleared, got %+v", got)
	}
}

func TestBackupPaymentLinksIgnoreAnInvalidStoredValue(t *testing.T) {
	useBackupPaymentLinks(t, `[{"name":"Store","url":"https://store.example.com"}]`)
	var errorLog bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &errorLog
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})
	for _, stored := range []string{
		`[{"name":"Stored","url":"javascript:alert(1)"}]`,
		`[{"name":"","url":"https://store.example.com"}]`,
		"[" + strings.TrimSuffix(strings.Repeat(`{"name":"Shop","url":"https://shop.example.com"},`, 11), ",") + "]",
		`[{"name":"Truncated"`,
	} {
		errorLog.Reset()
		LoadBackupPaymentLinks(stored)
		want := []BackupPaymentLink{{Name: "Store", URL: "https://store.example.com"}}
		if got := WalletBackupPaymentLinks(true, ""); !slices.Equal(got, want) {
			t.Fatalf("stored %s must not replace the valid list, got %+v", stored, got)
		}
		if !strings.Contains(errorLog.String(), "ignoring invalid "+BackupPaymentLinksOptionKey) {
			t.Fatalf("stored %s was dropped without an error log", stored)
		}
	}
}

// One writer, as option updates are serialized, replaces the list while
// wallet requests read it; under -race this guards the read path.
func TestWalletBackupPaymentLinksConcurrentReplace(t *testing.T) {
	first := `[{"name":"A","url":"https://a.example","icon":"telegram"}]`
	second := `[{"name":"B","url":"https://b.example"},{"name":"C","url":"https://c.example"}]`
	useBackupPaymentLinks(t, first)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			LoadBackupPaymentLinks([]string{second, first}[i%2])
		}
	}()
	wantFirst := []BackupPaymentLink{{Name: "A", URL: "https://a.example", Icon: "telegram"}}
	wantSecond := []BackupPaymentLink{{Name: "B", URL: "https://b.example"}, {Name: "C", URL: "https://c.example"}}
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		got := WalletBackupPaymentLinks(true, "")
		if !slices.Equal(got, wantFirst) && !slices.Equal(got, wantSecond) {
			t.Fatalf("read a mixed list %+v", got)
		}
	}
}

func useBackupPaymentLinks(t *testing.T, value string) {
	t.Helper()
	applyBackupPaymentLinks(t, value)
	t.Cleanup(func() { applyBackupPaymentLinks(t, "[]") })
}

func applyBackupPaymentLinks(t *testing.T, value string) {
	t.Helper()
	LoadBackupPaymentLinks(value)
}
