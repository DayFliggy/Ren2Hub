package service

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	version "github.com/hashicorp/go-version"
)

const SystemReleaseAPI = "https://api.github.com/repos/DayFliggy/Ren2Hub"

var (
	ErrSystemSourceBuild      = errors.New("source builds cannot install releases")
	ErrSystemInvalidVersion   = errors.New("invalid release version")
	ErrSystemRollbackNotFound = errors.New("version is not in the rollback list")
	ErrSystemRelease          = errors.New("release retrieval or verification failed")
	ErrSystemReplace          = errors.New("failed to replace application executable")
)

type SystemReleaseInfo struct {
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type SystemUpdateInfo struct {
	CurrentVersion string             `json:"current_version"`
	LatestVersion  string             `json:"latest_version"`
	BuildType      string             `json:"build_type"`
	HasUpdate      bool               `json:"has_update"`
	Cached         bool               `json:"cached"`
	Warning        string             `json:"warning,omitempty"`
	ReleaseInfo    *SystemReleaseInfo `json:"release_info,omitempty"`
}

type RollbackVersion struct {
	Version     string `json:"version"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

const maxReleaseSize int64 = 500 << 20

var stableReleaseTag = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type systemRelease struct {
	SystemReleaseInfo
	Tag        string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []systemAsset `json:"assets"`
}

type systemAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// SystemUpdateService reads release metadata and installs verified binaries on this node.
type SystemUpdateService struct {
	apiBase    string
	client     *http.Client
	executable string
	pathErr    error

	mu       sync.Mutex
	cached   *systemRelease
	cachedAt time.Time
}

func NewSystemUpdateService(apiBase string, client *http.Client) *SystemUpdateService {
	// On Linux /proc/self/exe follows the running inode, including its rename to .backup.
	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.EvalSymlinks(executable)
	}
	return &SystemUpdateService{
		apiBase:    strings.TrimRight(apiBase, "/"),
		client:     client,
		executable: executable,
		pathErr:    err,
	}
}

func releaseMetadata(ctx context.Context, client *http.Client, endpoint string, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSystemRelease, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GitHub returned %d", ErrSystemRelease, resp.StatusCode)
	}
	if err := common.DecodeJsonBody(resp.Body, dst); err != nil {
		return fmt.Errorf("%w: invalid release metadata: %w", ErrSystemRelease, err)
	}
	return nil
}

func (s *SystemUpdateService) latest(ctx context.Context) (*systemRelease, error) {
	var release systemRelease
	err := releaseMetadata(ctx, s.client, s.apiBase+"/releases/latest", &release)
	if err != nil {
		return nil, err
	}
	if release.Draft || release.Prerelease || !stableReleaseTag.MatchString(release.Tag) {
		return nil, fmt.Errorf("%w: invalid latest release", ErrSystemRelease)
	}
	s.mu.Lock()
	s.cached, s.cachedAt = &release, time.Now()
	s.mu.Unlock()
	return &release, nil
}

func updateInfo(release *systemRelease, cached bool, warning string) (*SystemUpdateInfo, error) {
	latest, err := version.NewVersion(release.Tag)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid tag: %w", ErrSystemRelease, err)
	}
	current, err := version.NewVersion(common.Version)
	if err != nil && common.BuildType == "release" {
		return nil, fmt.Errorf("invalid installed version: %w", err)
	}
	details := release.SystemReleaseInfo
	return &SystemUpdateInfo{
		CurrentVersion: common.Version,
		LatestVersion:  release.Tag,
		HasUpdate:      current != nil && current.LessThan(latest),
		BuildType:      common.BuildType,
		Cached:         cached,
		Warning:        warning,
		ReleaseInfo:    &details,
	}, nil
}

func (s *SystemUpdateService) CheckUpdate(ctx context.Context, force bool) (*SystemUpdateInfo, error) {
	s.mu.Lock()
	cached, at := s.cached, s.cachedAt
	s.mu.Unlock()
	if !force && cached != nil && time.Since(at) < 20*time.Minute {
		return updateInfo(cached, true, "")
	}
	release, err := s.latest(ctx)
	if err != nil {
		if cached != nil {
			return updateInfo(cached, true, "GitHub is unavailable. Showing cached release information.")
		}
		return nil, err
	}
	return updateInfo(release, false, "")
}

func (s *SystemUpdateService) PerformUpdate(ctx context.Context) (*SystemUpdateInfo, error) {
	if common.BuildType != "release" {
		return nil, ErrSystemSourceBuild
	}
	release, err := s.latest(ctx)
	if err != nil {
		return nil, err
	}
	info, err := updateInfo(release, false, "")
	if err != nil || !info.HasUpdate {
		return info, err
	}
	return info, s.install(ctx, release)
}

func (s *SystemUpdateService) rollbackCandidates(ctx context.Context) ([]systemRelease, error) {
	current, err := version.NewVersion(common.Version)
	if err != nil {
		if common.BuildType == "source" {
			// A commit SHA has no semantic ordering with released versions.
			return nil, nil
		}
		return nil, fmt.Errorf("invalid installed version: %w", err)
	}
	var releases []systemRelease
	if err := releaseMetadata(ctx, s.client, s.apiBase+"/releases?per_page=100", &releases); err != nil {
		return nil, err
	}
	versions := make(map[string]*version.Version)
	var eligible []systemRelease
	for _, release := range releases {
		key := strings.TrimPrefix(release.Tag, "v")
		if release.Draft || release.Prerelease || !stableReleaseTag.MatchString(release.Tag) || versions[key] != nil {
			continue
		}
		v, err := version.NewVersion(key)
		if err != nil || !v.LessThan(current) {
			continue
		}
		versions[key] = v
		release.Tag = key
		eligible = append(eligible, release)
	}
	slices.SortFunc(eligible, func(a, b systemRelease) int { return versions[b.Tag].Compare(versions[a.Tag]) })
	return eligible[:min(len(eligible), 3)], nil
}

func (s *SystemUpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.rollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		result = append(result, RollbackVersion{Version: r.Tag, PublishedAt: r.PublishedAt, HTMLURL: r.HTMLURL})
	}
	return result, nil
}

func (s *SystemUpdateService) RollbackToVersion(ctx context.Context, v string) error {
	if common.BuildType != "release" {
		return ErrSystemSourceBuild
	}
	if !stableReleaseTag.MatchString(v) {
		return ErrSystemInvalidVersion
	}
	releases, err := s.rollbackCandidates(ctx)
	if err != nil {
		return err
	}
	for _, release := range releases {
		if release.Tag == strings.TrimPrefix(v, "v") {
			return s.install(ctx, &release)
		}
	}
	return ErrSystemRollbackNotFound
}

func (r *systemRelease) asset(name string) (systemAsset, error) {
	for _, asset := range r.Assets {
		if asset.Name != name {
			continue
		}
		if asset.Size > maxReleaseSize {
			return systemAsset{}, fmt.Errorf("%w: asset too large", ErrSystemRelease)
		}
		return asset, nil
	}
	return systemAsset{}, fmt.Errorf("%w: missing asset %s", ErrSystemRelease, name)
}

func allowedSystemAsset(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && (u.Host == "github.com" || u.Host == "objects.githubusercontent.com")
}

func (s *SystemUpdateService) download(ctx context.Context, asset systemAsset, out io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil || !allowedSystemAsset(req.URL) {
		return fmt.Errorf("%w: invalid asset URL", ErrSystemRelease)
	}
	// Validate the release manifest's asset URL; GitHub redirects it to signed CDN URLs.
	resp, err := s.client.Do(req)
	if err != nil {
		// net/http errors may include signed redirect URLs.
		return fmt.Errorf("%w: cannot download %s", ErrSystemRelease, asset.Name)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: asset returned %d", ErrSystemRelease, resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("%w: asset too large", ErrSystemRelease)
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return systemArchiveError(err)
	}
	if n > limit {
		return fmt.Errorf("%w: asset too large", ErrSystemRelease)
	}
	return nil
}

func systemArchiveError(err error) error {
	var fileErr *os.PathError
	if errors.As(err, &fileErr) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrSystemRelease, err)
}

func expectedReleaseHash(sums io.Reader, name string) ([]byte, error) {
	scanner := bufio.NewScanner(sums)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		hash, err := hex.DecodeString(fields[0])
		if err != nil || len(hash) != sha256.Size {
			return nil, fmt.Errorf("%w: invalid checksum", ErrSystemRelease)
		}
		return hash, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSystemRelease, err)
	}
	return nil, fmt.Errorf("%w: missing checksum for %s", ErrSystemRelease, name)
}

func (s *SystemUpdateService) install(ctx context.Context, release *systemRelease) error {
	if s.pathErr != nil {
		return s.pathErr
	}
	name := "ren2hub_" + strings.TrimPrefix(release.Tag, "v") + "_linux_amd64.tar.gz"
	archive, err := release.asset(name)
	if err != nil {
		return err
	}
	checksums, err := release.asset("checksums.txt")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(s.executable), ".ren2hub-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	sums, err := os.Create(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return err
	}
	defer sums.Close()
	if err := s.download(ctx, checksums, sums, maxReleaseSize); err != nil {
		return err
	}
	if _, err := sums.Seek(0, io.SeekStart); err != nil {
		return err
	}
	expected, err := expectedReleaseHash(sums, name)
	if err != nil {
		return err
	}

	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if err := s.download(ctx, archive, io.MultiWriter(file, digest), maxReleaseSize); err != nil {
		return err
	}
	if !bytes.Equal(digest.Sum(nil), expected) {
		return fmt.Errorf("%w: checksum mismatch", ErrSystemRelease)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	staged := filepath.Join(dir, "ren2hub")
	if err := extractSystemBinary(ctx, file, staged); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrSystemRelease, err)
	}
	return replaceSystemBinary(s.executable, staged)
}

func extractSystemBinary(ctx context.Context, archive io.Reader, staged string) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return systemArchiveError(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrSystemRelease, err)
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return systemArchiveError(err)
		}
		if !filepath.IsLocal(header.Name) || slices.Contains(strings.Split(header.Name, "/"), "..") {
			return fmt.Errorf("%w: unsafe archive path", ErrSystemRelease)
		}
		if header.Size > maxReleaseSize {
			return fmt.Errorf("%w: archive entry too large", ErrSystemRelease)
		}
		if header.Name != "ren2hub" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 {
			return fmt.Errorf("%w: invalid binary entry", ErrSystemRelease)
		}
		if err := writeSystemBinary(staged, tr); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("%w: archive has no ren2hub binary", ErrSystemRelease)
	}
	return nil
}

func writeSystemBinary(staged string, src io.Reader) error {
	out, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, src); err != nil {
		out.Close()
		return systemArchiveError(err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func replaceSystemBinary(current, staged string) error {
	backup := current + ".backup"
	if err := os.Rename(current, backup); err != nil {
		return fmt.Errorf("%w: backup binary: %w", ErrSystemReplace, err)
	}
	if err := os.Rename(staged, current); err != nil {
		if restore := os.Rename(backup, current); restore != nil {
			return fmt.Errorf("%w: replace and restore binary: %w", ErrSystemReplace, errors.Join(err, restore))
		}
		return fmt.Errorf("%w (backup restored): %w", ErrSystemReplace, err)
	}
	return nil
}
