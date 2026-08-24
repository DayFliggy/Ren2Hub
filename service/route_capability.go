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

func RegisterRouteCapabilityRefreshHook() {
	model.SetChannelCapabilityRefreshHook(func(channelID int) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), RouteCapabilityRefreshTimeout())
			defer cancel()
			if err := RefreshChannelCapabilitiesByID(ctx, channelID); err != nil {
				common.SysError(fmt.Sprintf("incremental route capability refresh failed: channel_id=%d error=%v", channelID, err))
			}
		}()
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
		capabilityRefreshLagSeconds.Store(int64(time.Since(startedAt).Seconds()))
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
		if err := refreshOneChannelWithHash(ctx, channel, byChannel[channel.Id], catalog, hash); err != nil {
			markChannelCapabilityRefreshFailure(channel.Id, hash, catalog.Version, err)
			summary.Failed++
			observeCapabilityRefreshFailure(err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
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
	if err := refreshOneChannelWithHash(ctx, channel, abilities, catalog, hash); err != nil {
		markChannelCapabilityRefreshFailure(channel.Id, hash, catalog.Version, err)
		return err
	}
	if rebuild {
		return RebuildRouteCapabilityIndex(ctx)
	}
	return nil
}

func markChannelCapabilityRefreshFailure(channelID int, sourceHash, catalogVersion string, refreshErr error) {
	// A CAS loser must not change the status of the snapshot published by the
	// winner. Other failures may expose the latest attempt as failed while the
	// active capability rows remain intact.
	if errors.Is(refreshErr, model.ErrCapabilitySnapshotConflict) {
		return
	}
	_ = model.MarkChannelCapabilityRefreshFailure(channelID, sourceHash, catalogVersion)
}

func refreshOneChannelWithHash(ctx context.Context, channel *model.Channel, abilities []model.Ability, catalog *modellab.Catalog, sourceHash string) error {
	capabilities := projectChannelCapabilities(channel, abilities, catalog, sourceHash)
	return model.PublishChannelCapabilitySnapshot(ctx, channel.Id, sourceHash, catalog.Version, capabilities)
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
	if err := model.DB.WithContext(ctx).Where("active_version > ?", 0).Find(&snapshots).Error; err != nil {
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
	if err := model.DB.WithContext(ctx).Find(&channels).Error; err != nil {
		return err
	}
	channelsByID := make(map[int]*model.Channel, len(channels))
	for index := range channels {
		channelsByID[channels[index].Id] = &channels[index]
	}
	index := &capabilityIndex{
		ByRequestModel: make(map[string][]indexedCapability),
		Generation:     uint64(time.Now().UnixNano()),
	}
	mixedByChannel := make(map[int]bool, len(channelsByID))
	for channelID, channel := range channelsByID {
		mixedByChannel[channelID] = modellab.Resolve(channel.Models, channel.GetModelMapping()).GroupSlug == modellab.GroupMixed
	}
	for _, capability := range capabilities {
		if activeVersion[capability.ChannelID] != capability.SnapshotVersion {
			continue
		}
		channel := channelsByID[capability.ChannelID]
		if channel == nil {
			continue
		}
		// AbilityGroups is part of the immutable capability row. Falling back
		// to the live Ability table here would let a later edit change an old
		// snapshot during replay or after an unrelated index rebuild.
		groups := decodeStringList(capability.AbilityGroups)
		index.ByRequestModel[capability.RequestModel] = append(index.ByRequestModel[capability.RequestModel], indexedCapability{
			Capability:    capability,
			ChannelStatus: channel.Status,
			Priority:      channel.GetPriority(),
			Weight:        channel.GetWeight(),
			AbilityGroups: append([]string(nil), groups...),
			ChannelType:   channel.Type,
			Advanced:      advancedCustomPathConfig(channel),
			Mixed:         mixedByChannel[capability.ChannelID],
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
			ChannelID:        channel.Id,
			RequestModel:     requestModel,
			ActualModel:      chosen.RealModel,
			LabSlug:          chosen.LabSlug,
			Confidence:       chosen.Confidence,
			Source:           source,
			CatalogVersion:   resolution.CatalogVersion,
			SourceHash:       sourceHash,
			AbilityGroups:    string(groupsJSON),
			EndpointTypes:    string(endpointJSON),
			PathCapabilities: string(pathJSON),
			State:            state,
			UpdatedAt:        time.Now().Unix(),
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
