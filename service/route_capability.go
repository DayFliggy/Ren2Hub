package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

const (
	capabilityRefreshDefaultInterval = 30 * time.Second
	capabilityRefreshDefaultTimeout  = 5 * time.Minute
)

type capabilityPath struct {
	IncomingPath string   `json:"incoming_path,omitempty"`
	UpstreamPath string   `json:"upstream_path,omitempty"`
	Models       []string `json:"models,omitempty"`
}

type indexedCapability struct {
	Capability    model.ChannelModelCapability
	ChannelStatus int
	Priority      int64
	Weight        int
	AbilityGroups []string
	ChannelType   int
	Advanced      *dto.AdvancedCustomConfig
	Mixed         bool
}

type capabilityIndex struct {
	ByRequestModel map[string][]indexedCapability
	Generation     uint64
}

var routeCapabilityIndex atomic.Value // *capabilityIndex
var capabilityRefreshMu sync.Mutex
var capabilityRefreshLagSeconds atomic.Int64

type capabilityRefreshDispatcher struct {
	mu      sync.Mutex
	pending map[int]struct{}
	queue   []int
	running bool
	refresh func(context.Context, int) error
	timeout func() time.Duration
	onError func(int, error)
}

func newCapabilityRefreshDispatcher(
	refresh func(context.Context, int) error,
	timeout func() time.Duration,
	onError func(int, error),
) *capabilityRefreshDispatcher {
	return &capabilityRefreshDispatcher{
		pending: make(map[int]struct{}),
		refresh: refresh,
		timeout: timeout,
		onError: onError,
	}
}

func (d *capabilityRefreshDispatcher) enqueue(channelID int) {
	if d == nil || channelID <= 0 || d.refresh == nil || d.timeout == nil {
		return
	}
	d.mu.Lock()
	if _, exists := d.pending[channelID]; exists {
		d.mu.Unlock()
		return
	}
	d.pending[channelID] = struct{}{}
	d.queue = append(d.queue, channelID)
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.mu.Unlock()
	go d.run()
}

func (d *capabilityRefreshDispatcher) run() {
	for {
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.running = false
			d.mu.Unlock()
			return
		}
		channelID := d.queue[0]
		d.queue = d.queue[1:]
		delete(d.pending, channelID)
		d.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), d.timeout())
		err := d.refresh(ctx, channelID)
		cancel()
		if err != nil && d.onError != nil {
			d.onError(channelID, err)
		}
	}
}

func (d *capabilityRefreshDispatcher) idle() bool {
	if d == nil {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.running && len(d.queue) == 0 && len(d.pending) == 0
}

var routeCapabilityRefreshDispatcher = newCapabilityRefreshDispatcher(
	RefreshChannelCapabilitiesByID,
	RouteCapabilityRefreshTimeout,
	func(channelID int, err error) {
		common.SysError(fmt.Sprintf("incremental route capability refresh failed: channel_id=%d error=%v", channelID, err))
	},
)

func RegisterRouteCapabilityRefreshHook() {
	model.SetChannelCapabilityRefreshHook(func(channelID int) {
		routeCapabilityRefreshDispatcher.enqueue(channelID)
	})
}

type CapabilityRefreshSummary struct {
	Scanned   int `json:"scanned"`
	Refreshed int `json:"refreshed"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// InitRouteCapabilityIndex performs the startup rebuild and publishes the
// immutable in-memory active snapshot used by Shadow mode.
func InitRouteCapabilityIndex(ctx context.Context) error {
	_, err := refreshAllChannels(ctx, false, nil)
	return err
}

func RefreshChannelCapabilitiesByID(ctx context.Context, channelID int) error {
	if channelID <= 0 {
		return errors.New("channel id is required")
	}
	capabilityRefreshMu.Lock()
	defer capabilityRefreshMu.Unlock()

	var channel model.Channel
	if err := model.DB.WithContext(ctx).Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A deleted channel must disappear from the immutable in-memory
			// index even though its last database snapshot is retained.
			return RebuildRouteCapabilityIndex(ctx)
		}
		return err
	}
	var abilities []model.Ability
	if err := model.DB.WithContext(ctx).Where("channel_id = ?", channelID).Find(&abilities).Error; err != nil {
		return err
	}
	catalog := modellab.DefaultCatalog()
	err := refreshOneChannel(ctx, &channel, abilities, catalog, true)
	if err != nil {
		observeCapabilityRefreshFailure(err)
	} else {
		routeShadowMetrics.RefreshSuccess.Add(1)
	}
	return err
}

// RefreshStaleChannelCapabilities is used by the scheduled fingerprint scan.
// It deliberately tolerates one channel failure so a broken configuration does
// not prevent unrelated channels from publishing new active snapshots.
func RefreshStaleChannelCapabilities(ctx context.Context, report func(processed, total int)) (CapabilityRefreshSummary, error) {
	capabilityRefreshMu.Lock()
	defer capabilityRefreshMu.Unlock()
	return refreshAllChannels(ctx, true, report)
}

func refreshAllChannels(ctx context.Context, fingerprintOnly bool, report func(processed, total int)) (CapabilityRefreshSummary, error) {
	startedAt := time.Now()
	defer func() {
		scanDuration := time.Since(startedAt)
		capabilityRefreshLagSeconds.Store(int64(scanDuration.Seconds()))
		observeCapabilityRefreshDurations(scanDuration, -1)
	}()
	var channels []model.Channel
	if err := model.DB.WithContext(ctx).Find(&channels).Error; err != nil {
		return CapabilityRefreshSummary{}, err
	}
	var abilities []model.Ability
	if err := model.DB.WithContext(ctx).Find(&abilities).Error; err != nil {
		return CapabilityRefreshSummary{}, err
	}
	byChannel := make(map[int][]model.Ability, len(channels))
	for _, ability := range abilities {
		byChannel[ability.ChannelId] = append(byChannel[ability.ChannelId], ability)
	}
	catalog := modellab.DefaultCatalog()
	summary := CapabilityRefreshSummary{Scanned: len(channels)}
	var firstErr error
	for index := range channels {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		channel := &channels[index]
		hash, hashErr := channelCapabilityFingerprint(channel, byChannel[channel.Id])
		if hashErr != nil {
			summary.Failed++
			if firstErr == nil {
				firstErr = hashErr
			}
			continue
		}
		if fingerprintOnly && channelCapabilitySnapshotMatches(channel.Id, hash, catalog.Version) {
			summary.Skipped++
			if report != nil {
				report(index+1, len(channels))
			}
			continue
		}
		detectedAt := time.Now()
		expected, fenceErr := model.GetChannelCapabilitySnapshotFence(ctx, channel.Id)
		if fenceErr != nil {
			summary.Failed++
			if firstErr == nil {
				firstErr = fenceErr
			}
			observeCapabilityRefreshFailure(fenceErr)
			continue
		}
		publishStartedAt := time.Now()
		if err := refreshOneChannelWithHash(ctx, channel, byChannel[channel.Id], catalog, hash, expected); err != nil {
			if markerErr := markChannelCapabilityRefreshFailure(channel.Id, expected, hash, catalog.Version, err); markerErr != nil {
				err = errors.Join(err, markerErr)
			}
			summary.Failed++
			observeCapabilityRefreshFailure(err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			observeCapabilityRefreshDurations(-1, time.Since(publishStartedAt))
			observeCapabilityRefreshDetectionToActive(time.Since(detectedAt))
			summary.Refreshed++
			routeShadowMetrics.RefreshSuccess.Add(1)
		}
		if report != nil {
			report(index+1, len(channels))
		}
	}
	if err := RebuildRouteCapabilityIndex(ctx); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}
	return summary, firstErr
}

func observeCapabilityRefreshFailure(err error) {
	routeShadowMetrics.RefreshFailure.Add(1)
	if errors.Is(err, model.ErrCapabilitySnapshotConflict) {
		routeShadowMetrics.SnapshotConflicts.Add(1)
	}
}

func refreshOneChannel(ctx context.Context, channel *model.Channel, abilities []model.Ability, catalog *modellab.Catalog, rebuild bool) error {
	hash, err := channelCapabilityFingerprint(channel, abilities)
	if err != nil {
		return err
	}
	detectedAt := time.Now()
	expected, err := model.GetChannelCapabilitySnapshotFence(ctx, channel.Id)
	if err != nil {
		return err
	}
	publishStartedAt := time.Now()
	if err := refreshOneChannelWithHash(ctx, channel, abilities, catalog, hash, expected); err != nil {
		if markerErr := markChannelCapabilityRefreshFailure(channel.Id, expected, hash, catalog.Version, err); markerErr != nil {
			return errors.Join(err, markerErr)
		}
		return err
	}
	observeCapabilityRefreshDurations(-1, time.Since(publishStartedAt))
	observeCapabilityRefreshDetectionToActive(time.Since(detectedAt))
	if rebuild {
		return RebuildRouteCapabilityIndex(ctx)
	}
	return nil
}

func markChannelCapabilityRefreshFailure(channelID int, expected model.ChannelCapabilitySnapshotFence, sourceHash, catalogVersion string, refreshErr error) error {
	// A CAS loser must not change the status of the snapshot published by the
	// winner. Existing active rows remain active; the model layer records the
	// failed source separately so a failed refresh cannot hide usable data.
	if errors.Is(refreshErr, model.ErrCapabilitySnapshotConflict) {
		return nil
	}
	// The caller records the complete active tuple before projection. A failed
	// refresh cannot change a snapshot replaced by another worker.
	if err := model.MarkChannelCapabilityRefreshFailure(channelID, expected, sourceHash, catalogVersion); err != nil {
		return fmt.Errorf("record channel capability refresh failure: %w", err)
	}
	return nil
}

func refreshOneChannelWithHash(ctx context.Context, channel *model.Channel, abilities []model.Ability, catalog *modellab.Catalog, sourceHash string, expected model.ChannelCapabilitySnapshotFence) error {
	capabilities := projectChannelCapabilities(channel, abilities, catalog, sourceHash)
	return model.PublishChannelCapabilitySnapshot(ctx, channel.Id, expected, sourceHash, catalog.Version, capabilities)
}

func channelCapabilitySnapshotMatches(channelID int, sourceHash, catalogVersion string) bool {
	var snapshot model.ChannelCapabilitySnapshot
	if err := model.DB.Where("channel_id = ?", channelID).First(&snapshot).Error; err != nil {
		return false
	}
	return snapshot.RefreshStatus == model.RouteCapabilityRefreshActive &&
		snapshot.SourceHash == sourceHash && snapshot.CatalogVersion == catalogVersion
}

func RebuildRouteCapabilityIndex(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var snapshots []model.ChannelCapabilitySnapshot
	if err := model.DB.WithContext(ctx).Where("active_version > ? AND refresh_status = ?", 0, model.RouteCapabilityRefreshActive).Find(&snapshots).Error; err != nil {
		return err
	}
	activeVersion := make(map[int]int64, len(snapshots))
	for _, snapshot := range snapshots {
		activeVersion[snapshot.ChannelID] = snapshot.ActiveVersion
	}
	var capabilities []model.ChannelModelCapability
	if err := model.DB.WithContext(ctx).Find(&capabilities).Error; err != nil {
		return err
	}
	var channels []model.Channel
	if err := model.DB.WithContext(ctx).Select("id").Find(&channels).Error; err != nil {
		return err
	}
	channelExists := make(map[int]struct{}, len(channels))
	for _, channel := range channels {
		channelExists[channel.Id] = struct{}{}
	}
	index := &capabilityIndex{
		ByRequestModel: make(map[string][]indexedCapability),
		Generation:     uint64(time.Now().UnixNano()),
	}
	for _, capability := range capabilities {
		if activeVersion[capability.ChannelID] != capability.SnapshotVersion {
			continue
		}
		if _, exists := channelExists[capability.ChannelID]; !exists {
			continue
		}
		// AbilityGroups is part of the immutable capability row. Falling back
		// to the live Ability table here would let a later edit change an old
		// snapshot during replay or after an unrelated index rebuild.
		groups := decodeStringList(capability.AbilityGroups)
		index.ByRequestModel[capability.RequestModel] = append(index.ByRequestModel[capability.RequestModel], indexedCapability{
			Capability:    capability,
			ChannelStatus: capability.ChannelStatus,
			Priority:      capability.Priority,
			Weight:        capability.Weight,
			AbilityGroups: append([]string(nil), groups...),
			ChannelType:   capability.ChannelType,
			Advanced:      advancedCustomPathConfigFromCapability(capability),
			Mixed:         capability.IsMixed,
		})
	}
	for requestModel := range index.ByRequestModel {
		sort.Slice(index.ByRequestModel[requestModel], func(i, j int) bool {
			left := index.ByRequestModel[requestModel][i]
			right := index.ByRequestModel[requestModel][j]
			if left.Priority != right.Priority {
				return left.Priority > right.Priority
			}
			return left.Capability.ChannelID < right.Capability.ChannelID
		})
	}
	routeCapabilityIndex.Store(index)
	return nil
}

func projectChannelCapabilities(channel *model.Channel, abilities []model.Ability, catalog *modellab.Catalog, sourceHash string) []model.ChannelModelCapability {
	resolution := modellab.ResolveWithCatalog(catalog, channel.Models, channel.GetModelMapping())
	type projection struct {
		matches []modellab.ModelMatch
	}
	byRequest := make(map[string]*projection)
	for _, match := range resolution.Models {
		requestModel := modellab.NormalizeModel(match.InputModel)
		if requestModel == "" {
			continue
		}
		if byRequest[requestModel] == nil {
			byRequest[requestModel] = &projection{}
		}
		byRequest[requestModel].matches = append(byRequest[requestModel].matches, match)
	}
	abilityGroups := make(map[string][]string)
	for _, ability := range abilities {
		if !ability.Enabled {
			continue
		}
		key := modellab.NormalizeModel(ability.Model)
		abilityGroups[key] = append(abilityGroups[key], ability.Group)
	}
	for key := range abilityGroups {
		abilityGroups[key] = sortedUniqueStrings(abilityGroups[key])
	}

	paths := buildCapabilityPaths(channel)
	endpointTypes := make(map[string]struct{})
	for _, matchGroup := range byRequest {
		for _, match := range matchGroup.matches {
			for _, endpoint := range endpointTypesForModel(channel, match) {
				endpointTypes[string(endpoint)] = struct{}{}
			}
		}
	}
	endpointList := make([]string, 0, len(endpointTypes))
	for endpoint := range endpointTypes {
		endpointList = append(endpointList, endpoint)
	}
	sort.Strings(endpointList)
	endpointJSON, _ := common.Marshal(endpointList)
	pathJSON, _ := common.Marshal(paths)

	requestModels := make([]string, 0, len(byRequest))
	for requestModel := range byRequest {
		requestModels = append(requestModels, requestModel)
	}
	sort.Strings(requestModels)
	result := make([]model.ChannelModelCapability, 0, len(requestModels))
	isMixed := resolution.GroupSlug == modellab.GroupMixed
	for _, requestModel := range requestModels {
		matches := byRequest[requestModel].matches
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].RealModel != matches[j].RealModel {
				return matches[i].RealModel < matches[j].RealModel
			}
			if matches[i].LabSlug != matches[j].LabSlug {
				return matches[i].LabSlug < matches[j].LabSlug
			}
			return matches[i].Source < matches[j].Source
		})
		chosen := matches[0]
		conflict := false
		for _, match := range matches[1:] {
			if match.RealModel != chosen.RealModel || match.LabSlug != chosen.LabSlug {
				conflict = true
				break
			}
		}
		state := model.RouteCapabilityStateEligible
		source := chosen.Source
		if conflict {
			state = model.RouteCapabilityStateConflict
		} else if chosen.LabSlug == "" || chosen.Source == "unknown" {
			state = model.RouteCapabilityStateUnresolved
		}
		groupsJSON, _ := common.Marshal(abilityGroups[requestModel])
		result = append(result, model.ChannelModelCapability{
			ChannelID:         channel.Id,
			RequestModel:      requestModel,
			ActualModel:       chosen.RealModel,
			LabSlug:           chosen.LabSlug,
			Confidence:        chosen.Confidence,
			Source:            source,
			CatalogVersion:    resolution.CatalogVersion,
			SourceHash:        sourceHash,
			AbilityGroups:     string(groupsJSON),
			EndpointTypes:     string(endpointJSON),
			PathCapabilities:  string(pathJSON),
			ChannelStatus:     channel.Status,
			Priority:          channel.GetPriority(),
			Weight:            channel.GetWeight(),
			ChannelType:       channel.Type,
			ProjectionVersion: model.ChannelCapabilityProjectionV1,
			IsMixed:           isMixed,
			State:             state,
			UpdatedAt:         time.Now().Unix(),
		})
	}
	if resolution.MappingError {
		for index := range result {
			if result[index].State == model.RouteCapabilityStateEligible {
				result[index].State = model.RouteCapabilityStateUnresolved
			}
		}
	}
	return result
}

func endpointTypesForModel(channel *model.Channel, match modellab.ModelMatch) []constant.EndpointType {
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		config := advancedCustomPathConfig(channel)
		if config == nil {
			return nil
		}
		endpoints := config.SupportedEndpointTypesForModel(match.InputModel)
		endpoints = append(endpoints, config.SupportedEndpointTypesForModel(match.RealModel)...)
		return endpoints
	}
	return common.GetEndpointTypesByChannelType(channel.Type, match.RealModel)
}

func buildCapabilityPaths(channel *model.Channel) []capabilityPath {
	config := advancedCustomPathConfig(channel)
	if config == nil {
		return nil
	}
	paths := make([]capabilityPath, 0, len(config.Routes))
	for _, route := range config.Routes {
		paths = append(paths, capabilityPath{
			IncomingPath: strings.TrimSpace(route.IncomingPath),
			UpstreamPath: strings.TrimSpace(route.UpstreamPath),
			Models:       append([]string(nil), route.Models...),
		})
	}
	return paths
}

func advancedCustomPathConfig(channel *model.Channel) *dto.AdvancedCustomConfig {
	if channel == nil || channel.Type != constant.ChannelTypeAdvancedCustom {
		return nil
	}
	config := channel.GetOtherSettings().AdvancedCustom
	if config == nil {
		return nil
	}
	copyConfig := &dto.AdvancedCustomConfig{Routes: make([]dto.AdvancedCustomRoute, 0, len(config.Routes))}
	for _, route := range config.Routes {
		copyConfig.Routes = append(copyConfig.Routes, dto.AdvancedCustomRoute{
			IncomingPath: route.IncomingPath,
			UpstreamPath: route.UpstreamPath,
			Converter:    route.Converter,
			Models:       append([]string(nil), route.Models...),
		})
	}
	return copyConfig
}

func advancedCustomPathConfigFromCapability(capability model.ChannelModelCapability) *dto.AdvancedCustomConfig {
	if strings.TrimSpace(capability.PathCapabilities) == "" {
		return nil
	}
	var paths []capabilityPath
	if err := common.UnmarshalJsonStr(capability.PathCapabilities, &paths); err != nil || len(paths) == 0 {
		return nil
	}
	config := &dto.AdvancedCustomConfig{Routes: make([]dto.AdvancedCustomRoute, 0, len(paths))}
	for _, path := range paths {
		config.Routes = append(config.Routes, dto.AdvancedCustomRoute{
			IncomingPath: path.IncomingPath,
			UpstreamPath: path.UpstreamPath,
			Models:       append([]string(nil), path.Models...),
		})
	}
	return config
}

type channelFingerprint struct {
	Status    int
	Models    string
	Mapping   string
	Type      int
	Group     string
	Priority  int64
	Weight    int
	Advanced  []capabilityPath
	Abilities []abilityFingerprint
}

type abilityFingerprint struct {
	Model   string
	Group   string
	Enabled bool
}

func channelCapabilityFingerprint(channel *model.Channel, abilities []model.Ability) (string, error) {
	if channel == nil {
		return "", errors.New("channel is required")
	}
	fingerprint := channelFingerprint{
		Status:   channel.Status,
		Models:   channel.Models,
		Mapping:  channel.GetModelMapping(),
		Type:     channel.Type,
		Group:    channel.Group,
		Priority: channel.GetPriority(),
		Weight:   channel.GetWeight(),
		Advanced: buildCapabilityPaths(channel),
	}
	for _, ability := range abilities {
		fingerprint.Abilities = append(fingerprint.Abilities, abilityFingerprint{
			Model:   ability.Model,
			Group:   ability.Group,
			Enabled: ability.Enabled,
		})
	}
	sort.Slice(fingerprint.Abilities, func(i, j int) bool {
		left, right := fingerprint.Abilities[i], fingerprint.Abilities[j]
		if left.Model != right.Model {
			return left.Model < right.Model
		}
		if left.Group != right.Group {
			return left.Group < right.Group
		}
		return !left.Enabled && right.Enabled
	})
	data, err := common.Marshal(fingerprint)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}

func decodeStringList(data string) []string {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	var values []string
	if err := common.UnmarshalJsonStr(data, &values); err != nil {
		return nil
	}
	return sortedUniqueStrings(values)
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func routeCapabilityRefreshInterval() time.Duration {
	seconds := common.GetEnvOrDefault("ROUTE_CAPABILITY_REFRESH_INTERVAL_SECONDS", int(capabilityRefreshDefaultInterval/time.Second))
	if seconds < 5 {
		seconds = int(capabilityRefreshDefaultInterval / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func routeCapabilityRefreshEnabled() bool {
	return common.GetEnvOrDefaultBool("ROUTE_CAPABILITY_REFRESH_TASK_ENABLED", true)
}

func RouteCapabilityRefreshInterval() time.Duration { return routeCapabilityRefreshInterval() }

func RouteCapabilityRefreshTimeout() time.Duration {
	seconds := common.GetEnvOrDefault("ROUTE_CAPABILITY_REFRESH_TIMEOUT_SECONDS", int(capabilityRefreshDefaultTimeout/time.Second))
	if seconds < 5 {
		seconds = int(capabilityRefreshDefaultTimeout / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func RouteCapabilityRefreshEnabled() bool { return routeCapabilityRefreshEnabled() }

func RouteCapabilityRefreshLagSeconds() int64 { return capabilityRefreshLagSeconds.Load() }

func resetRouteCapabilityIndexForTest() {
	routeCapabilityIndex.Store((*capabilityIndex)(nil))
}
