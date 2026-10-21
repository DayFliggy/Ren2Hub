package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/pkg/pricesource"
)

// 指纹借鉴 Sub2API：发布方的 .sha256 只作为同步锚点，与下载内容不一致时
// 只警告、不阻止生成草稿（发布方可能先更新数据、后更新哈希文件）。
type PriceSourceFingerprint struct {
	SHA256          string  `json:"sha256"`
	PublishedSHA256 *string `json:"published_sha256"`
	Matches         *bool   `json:"matches"`
}

type PriceSourceDraft struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Status      string                  `json:"status"`
	Fingerprint *PriceSourceFingerprint `json:"fingerprint"`
	Models      []pricesource.Model     `json:"models"`
	Warnings    []string                `json:"warnings"`
	Errors      []string                `json:"errors"`
}

const (
	priceDocumentLimit = 10 << 20
	priceHashLimit     = 4 << 10
)

var sha256Hex = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Fixed sources contain no credentials and never accept caller-provided URLs.
// Fetching only creates editable drafts; publishing uses native pricing APIs.
func FetchPriceDrafts(ctx context.Context, ids []string) ([]PriceSourceDraft, error) {
	sources, err := pricesource.Select(ids)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return fetchPriceDrafts(ctx, sources, client)
}

func fetchPriceDrafts(ctx context.Context, sources []pricesource.Source, client *http.Client) ([]PriceSourceDraft, error) {
	results := make([]PriceSourceDraft, 0, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, fetchPriceDraft(ctx, source, client))
	}
	return results, nil
}

func fetchPriceDraft(ctx context.Context, source pricesource.Source, client *http.Client) PriceSourceDraft {
	result := PriceSourceDraft{ID: source.ID, Name: source.Name, Status: "error", Models: []pricesource.Model{}, Warnings: []string{}, Errors: []string{}}
	body, err := fetchLimited(ctx, client, source.URL, priceDocumentLimit)
	if err != nil {
		result.Errors = append(result.Errors, "price source: "+err.Error())
		return result
	}
	var warning string
	result.Fingerprint, warning = priceFingerprint(ctx, client, source, body)
	if warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}
	models, err := pricesource.Parse(source.ID, body)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.Status = "success"
	result.Models = models
	for _, row := range models {
		if row.UnsupportedReason != "" {
			result.Status = "partial"
			break
		}
	}
	return result
}

// 返回下载内容的指纹；发布指纹缺失、读取失败或不一致时附带一条警告。
func priceFingerprint(ctx context.Context, client *http.Client, source pricesource.Source, body []byte) (*PriceSourceFingerprint, string) {
	sum := sha256.Sum256(body)
	fingerprint := &PriceSourceFingerprint{SHA256: hex.EncodeToString(sum[:])}
	if source.HashURL == "" {
		return fingerprint, "source does not publish a sha256 fingerprint"
	}
	published, err := fetchPublishedSHA256(ctx, client, source.HashURL)
	if err != nil {
		return fingerprint, "published sha256 unavailable: " + err.Error()
	}
	matches := published == fingerprint.SHA256
	fingerprint.PublishedSHA256 = &published
	fingerprint.Matches = &matches
	if !matches {
		return fingerprint, "published sha256 does not match the downloaded document"
	}
	return fingerprint, ""
}

// .sha256 文件的第一个字段是十六进制摘要，后面可能跟文件名。
func fetchPublishedSHA256(ctx context.Context, client *http.Client, url string) (string, error) {
	body, err := fetchLimited(ctx, client, url, priceHashLimit)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 || !sha256Hex.MatchString(fields[0]) {
		return "", fmt.Errorf("hash file is not a sha256 digest")
	}
	return strings.ToLower(fields[0]), nil
}

func fetchLimited(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("remote file exceeds %d bytes", limit)
	}
	return body, nil
}
