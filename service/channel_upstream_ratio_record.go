package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	upstreamRatioCycleLimit       = 20
	upstreamRatioCycleConcurrency = 4
)

var upstreamRatioManualMax = decimal.NewFromInt(1000)

// ChannelUpstreamRatioView 是管理端读取的形状；Ratio 是保存值，EffectiveRatio 是此刻生效值
// （Sub2API 高峰时段按当前时间计算）。Groups 只在单渠道详情里返回。
type ChannelUpstreamRatioView struct {
	ChannelId      int             `json:"channel_id"`
	Kind           string          `json:"kind"`
	Source         string          `json:"source"`
	Mode           string          `json:"mode"`
	Status         string          `json:"status"`
	Ratio          string          `json:"ratio"`
	EffectiveRatio string          `json:"effective_ratio"`
	GroupName      string          `json:"group_name"`
	Mapping        string          `json:"mapping"`
	TokenName      string          `json:"token_name"`
	Stale          bool            `json:"stale"`
	ObservedAt     int64           `json:"observed_at"`
	ReceivedAt     int64           `json:"received_at"`
	FreshUntil     int64           `json:"fresh_until"`
	NextProbeAt    int64           `json:"next_probe_at"`
	HTTPStatus     int             `json:"http_status"`
	LastError      string          `json:"last_error"`
	FailureCount   int             `json:"failure_count"`
	Version        int64           `json:"version"`
	Sub2API        *Sub2APIBilling `json:"sub2api,omitempty"`
	Groups         []NewAPIGroup   `json:"groups,omitempty"`
}

func decodeSub2APIDetail(record *model.ChannelUpstreamRatio) *Sub2APIBilling {
	if record.Source != model.UpstreamRatioSourceSub2API || record.Detail == "" {
		return nil
	}
	var billing Sub2APIBilling
	if common.UnmarshalJsonStr(record.Detail, &billing) != nil {
		return nil
	}
	return &billing
}

func decodeNewAPIDetail(record *model.ChannelUpstreamRatio) *NewAPIDetail {
	if record.Source != model.UpstreamRatioSourceNewAPI || record.Detail == "" {
		return nil
	}
	var detail NewAPIDetail
	if common.UnmarshalJsonStr(record.Detail, &detail) != nil {
		return nil
	}
	return &detail
}

func buildUpstreamRatioView(channel *model.Channel, record *model.ChannelUpstreamRatio, withGroups bool, now time.Time) ChannelUpstreamRatioView {
	view := ChannelUpstreamRatioView{ChannelId: channel.Id, Kind: UpstreamRatioKind(channel), Mode: model.UpstreamRatioModeAuto}
	if record == nil || !record.BelongsTo(channel) {
		return view
	}
	view.Source, view.Mode, view.Status = record.Source, record.Mode, record.Status
	view.Ratio, view.EffectiveRatio = record.Ratio, record.Ratio
	view.GroupName, view.Mapping, view.TokenName = record.GroupName, record.Mapping, record.TokenName
	view.ObservedAt, view.ReceivedAt, view.FreshUntil, view.NextProbeAt = record.ObservedAt, record.ReceivedAt, record.FreshUntil, record.NextProbeAt
	view.HTTPStatus, view.LastError, view.FailureCount, view.Version = record.HTTPStatus, record.LastError, record.FailureCount, record.Version
	view.Sub2API = decodeSub2APIDetail(record)
	if view.Sub2API != nil && record.Mode == model.UpstreamRatioModeAuto && record.Ratio != "" {
		view.EffectiveRatio = ""
		if rate, ok := view.Sub2API.effectiveRateAt(now); ok {
			view.EffectiveRatio = ratioString(rate)
		}
	}
	if withGroups {
		if detail := decodeNewAPIDetail(record); detail != nil {
			view.Groups = detail.Groups
		}
	}
	autoExpired := record.Mode == model.UpstreamRatioModeAuto && record.Ratio != "" && now.Unix() > record.FreshUntil
	view.Stale = record.CredentialFP != upstreamRatioFingerprint(channel) || autoExpired
	return view
}

func GetChannelUpstreamRatioViews(channelIDs []int, withGroups bool) ([]ChannelUpstreamRatioView, error) {
	channels, err := model.GetChannelsByIds(channelIDs)
	if err != nil {
		return nil, err
	}
	records, err := model.GetChannelUpstreamRatios(channelIDs)
	if err != nil {
		return nil, err
	}
	byChannel := make(map[int]*model.ChannelUpstreamRatio, len(records))
	for i := range records {
		byChannel[records[i].ChannelId] = &records[i]
	}
	now := time.Now()
	views := make([]ChannelUpstreamRatioView, 0, len(channels))
	for _, channel := range channels {
		views = append(views, buildUpstreamRatioView(channel, byChannel[channel.Id], withGroups, now))
	}
	return views, nil
}

func newUpstreamRatioRecord(channelID int) *model.ChannelUpstreamRatio {
	return &model.ChannelUpstreamRatio{ChannelId: channelID, Mode: model.UpstreamRatioModeAuto}
}

// applyNewAPIMapping 按“手动关联优先，其次令牌名严格等于分组名”确定分组。
// 找不到分组时保留旧倍率且不延长有效期，读取时显示为过期。
func applyNewAPIMapping(record *model.ChannelUpstreamRatio, detail *NewAPIDetail, dataTime time.Time) {
	if record.Mode != model.UpstreamRatioModeAuto {
		return
	}
	groupName, mapping := "", ""
	if record.Mapping == model.UpstreamRatioMappingManual && record.GroupName != "" {
		groupName, mapping = record.GroupName, model.UpstreamRatioMappingManual
	} else if _, ok := detail.ratioOf(record.TokenName); ok {
		groupName, mapping = record.TokenName, model.UpstreamRatioMappingNameMatch
	}
	if ratio, ok := detail.ratioOf(groupName); ok {
		record.GroupName, record.Mapping, record.Ratio = groupName, mapping, ratioString(ratio)
		record.Status, record.LastError = model.UpstreamRatioStatusOK, ""
		record.FreshUntil = dataTime.Add(2 * upstreamRatioInterval()).Unix()
		return
	}
	record.Status = model.UpstreamRatioStatusNeedsMapping
	record.LastError = ""
	if record.GroupName != "" {
		record.LastError = "group_missing"
	}
}

func applyUpstreamProbe(prev *model.ChannelUpstreamRatio, channelID int, kind, fingerprint string, result upstreamProbeResult, now time.Time) *model.ChannelUpstreamRatio {
	record := newUpstreamRatioRecord(channelID)
	if prev != nil {
		copied := *prev
		record = &copied
	}
	record.HTTPStatus = result.Outcome.HTTPStatus
	record.UpdatedAt = now.Unix()
	switch kind {
	case UpstreamRatioKindSub2API, UpstreamRatioKindNewAPI:
		// 旧明细属于另一种上游，或经过不支持类型后已没有来源标记，都不能按新来源解码。
		if record.Source != kind {
			clearUpstreamRatioSourceData(record)
		}
		record.Source = kind
	case UpstreamRatioKindUnsupported:
		record.Source = ""
	}
	if result.Outcome.ok() && oversizedUpstreamDetail(result) {
		result.Outcome = probeOutcome{Status: model.UpstreamRatioStatusFailed, Reason: "detail_too_large", HTTPStatus: result.Outcome.HTTPStatus}
	}
	// 失败时保留旧指纹：渠道配置已变而探测失败，旧倍率仍然显示为过期。
	// 还没有任何旧数据时没有可过期的内容，直接记当前指纹。
	if !result.Outcome.ok() {
		if record.Ratio == "" && record.Detail == "" {
			record.CredentialFP = fingerprint
		}
		record.Status, record.LastError = result.Outcome.Status, result.Outcome.Reason
		record.FailureCount++
		delay := failedUpstreamRatioProbeDelay(upstreamRatioIntervalMinutes, record.FailureCount, result.Outcome.RetryAfter)
		if record.Status == model.UpstreamRatioStatusUnsupported {
			delay = unsupportedUpstreamRatioProbeDelay(upstreamRatioIntervalMinutes, result.Outcome.RetryAfter)
			if kind == UpstreamRatioKindDetect {
				record.Source = ""
			}
		}
		record.NextProbeAt = now.Add(delay).Unix()
		return record
	}

	if record.Source != result.Source {
		clearUpstreamRatioSourceData(record)
	} else if record.CredentialFP != fingerprint && record.Mode == model.UpstreamRatioModeAuto {
		// 凭据、地址或代理变了：旧的自动倍率和名称匹配属于旧配置，按本次结果重算；手动关联保留。
		record.Ratio, record.FreshUntil = "", 0
		if record.Mapping != model.UpstreamRatioMappingManual {
			record.GroupName, record.Mapping = "", ""
		}
	}
	record.CredentialFP = fingerprint
	record.Source = result.Source
	record.Status, record.LastError, record.FailureCount = model.UpstreamRatioStatusOK, "", 0
	record.ReceivedAt = now.Unix()
	record.NextProbeAt = now.Add(nextUpstreamRatioProbeDelay(upstreamRatioIntervalMinutes, 0)).Unix()
	switch result.Source {
	case model.UpstreamRatioSourceSub2API:
		record.Detail = marshalUpstreamRatioDetail(result.Billing)
		record.ObservedAt = result.Billing.ObservedAtUnix
		if record.Mode == model.UpstreamRatioModeAuto {
			record.Ratio = ratioString(result.Billing.ResolvedRate)
			record.FreshUntil = now.Add(2 * upstreamRatioInterval()).Unix()
		}
	case model.UpstreamRatioSourceNewAPI:
		record.Detail = marshalUpstreamRatioDetail(result.NewAPI)
		record.ObservedAt = now.Unix()
		record.TokenName = result.TokenName
		applyNewAPIMapping(record, result.NewAPI, now)
	}
	return record
}

// clearUpstreamRatioSourceData 清掉只属于某一种上游的数据：明细、分组关联和自动倍率。手动倍率保留。
func clearUpstreamRatioSourceData(record *model.ChannelUpstreamRatio) {
	record.Detail, record.ObservedAt, record.ReceivedAt = "", 0, 0
	record.GroupName, record.Mapping, record.TokenName = "", "", ""
	if record.Mode == model.UpstreamRatioModeAuto {
		record.Ratio, record.FreshUntil = "", 0
	}
}

// upstreamRatioDetailLimit 与 MySQL TEXT 列上限一致（65535 字节）。超过时不能截断保存，按失败处理。
const upstreamRatioDetailLimit = 65535

func oversizedUpstreamDetail(result upstreamProbeResult) bool {
	var detail any = result.NewAPI
	if result.Source == model.UpstreamRatioSourceSub2API {
		detail = result.Billing
	}
	return len(marshalUpstreamRatioDetail(detail)) > upstreamRatioDetailLimit
}

func marshalUpstreamRatioDetail(detail any) string {
	encoded, err := common.Marshal(detail)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func verifyUpstreamRatioFingerprint(fingerprint string) func(*model.Channel) error {
	return func(channel *model.Channel) error {
		if upstreamRatioFingerprint(channel) != fingerprint {
			return ErrUpstreamRatioChannelChanged
		}
		return nil
	}
}

func activeUpstreamBindingForChannel(channelID int) (*model.UpstreamChannelBinding, *model.UpstreamAccount, error) {
	b, err := model.GetUpstreamBindingForChannel(channelID)
	if err != nil || b == nil || b.Stale {
		return nil, nil, err
	}
	a, err := model.GetUpstreamAccount(b.AccountID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil || a.Status != "active" {
		return nil, nil, err
	}
	return b, a, nil
}

// applyImmediateBoundRatioLocked assumes the caller holds lockUpstreamRatioChannel(ch.Id)
// and never acquires lockManagedAccount, preserving strict lock hierarchy (Level 2 -> 3 -> 4 -> 5).
func applyImmediateBoundRatioLocked(b *model.UpstreamChannelBinding, ch *model.Channel, rec *model.ChannelUpstreamRatio, now time.Time) error {
	if b == nil || b.Stale || !b.RatioSyncEnabled || b.RemoteStatus != "active" {
		return nil
	}
	code, err := writeBoundRatio(*b, ch, rec, now)
	if err != nil {
		return handleBindingError(*b, err)
	}
	if code == b.RatioSyncError {
		return nil
	}
	if err := model.UpdateUpstreamBinding(b.ID, map[string]any{"ratio_sync_error": code}); err != nil {
		return err
	}
	b.RatioSyncError = code
	return nil
}

// probeAndSaveUpstreamRatio 探测并以 CAS 写回。同一渠道的探测按渠道串行：后到的探测在前一次
// 写回之后才读取版本与指纹，因此不会因版本过期而 409，也不会复用定时探测的结果。
func probeAndSaveUpstreamRatio(ctx context.Context, channelID int, fullDetect bool) (*model.ChannelUpstreamRatio, error) {
	defer lockUpstreamRatioChannel(channelID)()
	return probeAndSaveUpstreamRatioLocked(ctx, channelID, fullDetect, "")
}

func probeAndSaveUpstreamRatioLocked(ctx context.Context, channelID int, fullDetect bool, forcedSource string) (*model.ChannelUpstreamRatio, error) {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	prev, err := model.GetChannelUpstreamRatio(channelID)
	if err != nil {
		return nil, err
	}
	// 同 ID 重建前留下的记录属于上一代渠道，按没有记录处理；版本 0 写入会替换它。
	if prev != nil && !prev.BelongsTo(channel) {
		prev = nil
	}
	var expectedVersion int64
	knownSource := forcedSource
	if prev != nil {
		expectedVersion = prev.Version
		if knownSource == "" {
			knownSource = prev.Source
		}
	}
	binding, account, bindErr := activeUpstreamBindingForChannel(channelID)
	if bindErr != nil {
		return nil, bindErr
	}
	if binding != nil && account != nil && account.Platform == model.UpstreamRatioSourceSub2API && binding.Verify(channel) == nil {
		if knownSource == "" {
			knownSource = UpstreamRatioKindSub2API
		}
	}
	kind := UpstreamRatioKind(channel)
	fingerprint := upstreamRatioFingerprint(channel)
	now := time.Now()
	// When an active Sub2API binding identifies a mainstream-protocol channel,
	// probe /v1/sub2api/billing directly instead of blind dual-path fullDetect.
	probeFullDetect := fullDetect && !(binding != nil && account != nil && account.Platform == model.UpstreamRatioSourceSub2API && knownSource == UpstreamRatioKindSub2API)
	result := runUpstreamRatioProbe(ctx, channel, kind, knownSource, probeFullDetect, now)
	record := applyUpstreamProbe(prev, channelID, kind, fingerprint, result, now)
	record.ChannelCreatedTime = channel.CreatedTime
	if err := model.SaveChannelUpstreamRatio(record, expectedVersion, verifyUpstreamRatioFingerprint(fingerprint)); err != nil {
		return nil, err
	}
	if binding == nil || account == nil || account.Platform != model.UpstreamRatioSourceSub2API {
		return record, nil
	}
	// Re-read after the network probe: the binding may have been disabled or changed meanwhile.
	binding, account, err = activeUpstreamBindingForChannel(channelID)
	if err != nil {
		return nil, err
	}
	if binding == nil || account == nil || account.Platform != model.UpstreamRatioSourceSub2API {
		return record, nil
	}
	if err := applyImmediateBoundRatioLocked(binding, channel, record, now); err != nil {
		return nil, err
	}
	return record, nil
}

// upstreamRatioChannelLocks 按渠道 ID 串行化探测与写回，见 probeAndSaveUpstreamRatio。
var upstreamRatioChannelLocks sync.Map

func lockUpstreamRatioChannel(channelID int) func() {
	lock, _ := upstreamRatioChannelLocks.LoadOrStore(channelID, &sync.Mutex{})
	mutex := lock.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

// RefreshChannelUpstreamRatio 是管理员手动刷新：绑定到活跃 NewAPI 上游账号时走上游账号同步
// 以保留已认证分组倍率（mapping="account"），其余渠道执行直接探测并在开启同步时即时回写 channel_ratio。
func RefreshChannelUpstreamRatio(ctx context.Context, channelID int) (*ChannelUpstreamRatioView, error) {
	binding, account, err := activeUpstreamBindingForChannel(channelID)
	if err != nil {
		return nil, err
	}
	if binding != nil && account != nil && account.Platform == model.UpstreamRatioSourceNewAPI {
		// Verify channel identity before routing through the upstream account;
		// if the channel changed, mark stale and fall through to direct probe.
		if err := model.CheckUpstreamBindingIdentities(ctx, account.ID); err != nil {
			return nil, err
		}
		binding, account, err = activeUpstreamBindingForChannel(channelID)
		if err != nil {
			return nil, err
		}
		if binding != nil && account != nil && account.Platform == model.UpstreamRatioSourceNewAPI {
			if err := SyncUpstreamAccount(ctx, account.ID); err != nil {
				return nil, err
			}
			return getChannelUpstreamRatioView(channelID)
		}
	}
	if _, err := probeAndSaveUpstreamRatio(ctx, channelID, true); err != nil {
		return nil, err
	}
	return getChannelUpstreamRatioView(channelID)
}

func getChannelUpstreamRatioView(channelID int) (*ChannelUpstreamRatioView, error) {
	views, err := GetChannelUpstreamRatioViews([]int{channelID}, true)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &views[0], nil
}

type UpstreamRatioUpdate struct {
	Mode      string  `json:"mode"`
	Ratio     string  `json:"ratio"`
	GroupName *string `json:"group_name"`
	Version   int64   `json:"version"`
}

func normalizeManualUpstreamRatio(value string) (string, error) {
	ratio, ok := model.ParseBoundedRatio(value)
	if !ok || ratio.IsNegative() || ratio.GreaterThan(upstreamRatioManualMax) {
		return "", ErrUpstreamRatioInvalid
	}
	return ratio.String(), nil
}

// UpdateChannelUpstreamRatio 处理管理员手动设置：manual 模式写入手填倍率；auto 模式可选择
// NewAPI 分组（group_name 为空串表示取消手动关联，恢复名称匹配）。若渠道存在开启倍率同步的
// 活跃上游绑定，则立即按最新有效倍率 × 同步系数回写 channel_ratio。
func UpdateChannelUpstreamRatio(channelID int, update UpstreamRatioUpdate) (*ChannelUpstreamRatioView, error) {
	defer lockUpstreamRatioChannel(channelID)()
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	prev, err := model.GetChannelUpstreamRatio(channelID)
	if err != nil {
		return nil, err
	}
	if prev != nil && !prev.BelongsTo(channel) {
		prev = nil
	}
	fingerprint := upstreamRatioFingerprint(channel)
	record := newUpstreamRatioRecord(channelID)
	record.CredentialFP = fingerprint
	if prev != nil {
		copied := *prev
		record = &copied
	} else if kind := UpstreamRatioKind(channel); kind == UpstreamRatioKindSub2API || kind == UpstreamRatioKindNewAPI {
		record.Source = kind
	}
	now := time.Now()
	record.UpdatedAt = now.Unix()

	switch update.Mode {
	case model.UpstreamRatioModeManual:
		if update.GroupName != nil {
			return nil, ErrUpstreamRatioInvalid
		}
		ratio, err := normalizeManualUpstreamRatio(update.Ratio)
		if err != nil {
			return nil, err
		}
		record.Mode, record.Ratio, record.CredentialFP = model.UpstreamRatioModeManual, ratio, fingerprint
	case model.UpstreamRatioModeAuto:
		if update.Ratio != "" {
			return nil, ErrUpstreamRatioInvalid
		}
		if err := applyAutoUpstreamRatioUpdate(record, update.GroupName); err != nil {
			return nil, err
		}
	default:
		return nil, ErrUpstreamRatioInvalid
	}
	record.ChannelCreatedTime = channel.CreatedTime
	if err := model.SaveChannelUpstreamRatio(record, update.Version, verifyUpstreamRatioFingerprint(fingerprint)); err != nil {
		return nil, err
	}
	binding, account, err := activeUpstreamBindingForChannel(channelID)
	if err != nil {
		return nil, err
	}
	if binding != nil && account != nil {
		if err := applyImmediateBoundRatioLocked(binding, channel, record, now); err != nil {
			return nil, err
		}
	}
	return getChannelUpstreamRatioView(channelID)
}

func applyAutoUpstreamRatioUpdate(record *model.ChannelUpstreamRatio, groupName *string) error {
	if record.Mode == model.UpstreamRatioModeManual {
		// 从手动切回自动：手填值不再代表上游，清掉并让调度尽快重探。
		record.Mode, record.Ratio, record.FreshUntil, record.NextProbeAt = model.UpstreamRatioModeAuto, "", 0, 0
	}
	if groupName == nil {
		return nil
	}
	detail := decodeNewAPIDetail(record)
	if detail == nil {
		return ErrUpstreamRatioInvalid
	}
	if *groupName == "" {
		if record.Mapping == model.UpstreamRatioMappingManual {
			// 管理员取消手动关联：旧分组的倍率不再对应任何分组，清掉并停止有效期。
			record.Mapping, record.GroupName = "", ""
			record.Ratio, record.FreshUntil = "", 0
		}
	} else {
		if _, ok := detail.ratioOf(*groupName); !ok {
			return ErrUpstreamRatioInvalid
		}
		record.Mapping, record.GroupName = model.UpstreamRatioMappingManual, *groupName
	}
	if record.Status == model.UpstreamRatioStatusOK || record.Status == model.UpstreamRatioStatusNeedsMapping {
		applyNewAPIMapping(record, detail, time.Unix(record.ReceivedAt, 0))
	}
	return nil
}

type UpstreamRatioCycleSummary struct {
	Orphans  int64          `json:"orphans"`
	Probed   int            `json:"probed"`
	Statuses map[string]int `json:"statuses"`
	Errors   int            `json:"errors"`
}

// RunChannelUpstreamRatioCycle 每轮清理孤儿记录，再探测最多 20 个到期渠道，并发 4。
// Ren2Hub 单节点运行，系统任务租约已保证同一时刻只有一轮。
func RunChannelUpstreamRatioCycle(ctx context.Context) (*UpstreamRatioCycleSummary, error) {
	summary := &UpstreamRatioCycleSummary{Statuses: map[string]int{}}
	orphans, err := model.DeleteOrphanChannelUpstreamRatios()
	if err != nil {
		return summary, err
	}
	summary.Orphans = orphans
	channels, err := model.GetDueUpstreamRatioChannels(time.Now().Unix(), upstreamRatioCycleLimit)
	if err != nil {
		return summary, err
	}
	var mutex sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, upstreamRatioCycleConcurrency)
	for _, channel := range channels {
		wg.Add(1)
		slots <- struct{}{}
		go func(channelID int) {
			defer wg.Done()
			defer func() { <-slots }()
			record, err := probeAndSaveUpstreamRatio(ctx, channelID, false)
			mutex.Lock()
			defer mutex.Unlock()
			summary.Probed++
			if err != nil {
				summary.Errors++
				if !errors.Is(err, model.ErrUpstreamRatioConflict) && !errors.Is(err, ErrUpstreamRatioChannelChanged) {
					common.SysLog(common.LogText("channel %d upstream ratio probe failed: %v", channelID, err))
				}
				return
			}
			summary.Statuses[record.Status]++
		}(channel.Id)
	}
	wg.Wait()
	return summary, nil
}

func handleBindingError(b model.UpstreamChannelBinding, err error) error {
	if errors.Is(err, model.ErrUpstreamBindingStale) {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if checkErr := model.ValidateUpstreamBinding(b); errors.Is(checkErr, model.ErrUpstreamBindingStale) {
			return nil
		} else if checkErr != nil {
			return checkErr
		}
	}
	return err
}

func writeBoundRatio(b model.UpstreamChannelBinding, ch *model.Channel, rec *model.ChannelUpstreamRatio, now time.Time) (string, error) {
	verify := b.Verify
	if err := verify(ch); err != nil {
		if err = model.ValidateUpstreamBinding(b); err != nil {
			return "", err
		}
		return "", model.ErrUpstreamRatioConflict
	}
	target, code := upstreamTargetRatio(buildUpstreamRatioView(ch, rec, false, now), b.RatioSyncFactor)
	if code != "" {
		return code, nil
	}
	_, err := model.UpdateChannelRatioFromUpstreamSyncLocked(ch.Id, b.ChannelCreatedTime, verify, target)
	return "", err
}

// 高峰窗口的开始和结束独立于半小时一次的上游探测，分钟任务只读本地账单快照。
func refreshSub2APIBoundRatios(ctx context.Context) error {
	ids, err := model.Sub2APIRatioAccountIDs()
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, errors.Join(failures...))
		}
		if err := refreshSub2APIRatiosForAccount(ctx, id, time.Now()); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func refreshSub2APIRatiosForAccount(ctx context.Context, id int, now time.Time) error {
	defer lockManagedAccount(id)()
	a, err := model.GetUpstreamAccount(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.Platform != "sub2api" || a.Status != "active" || a.LastError != "" || a.LastSyncAt == 0 {
		return nil
	}
	bindings, err := model.ListUpstreamBindings(id)
	if err != nil {
		return err
	}
	var failures []error
	for _, b := range bindings {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, errors.Join(failures...))
		}
		if b.Stale || !b.RatioSyncEnabled || b.RemoteStatus != "active" {
			continue
		}
		if err := refreshSub2APIRatio(b, now); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func refreshSub2APIRatio(b model.UpstreamChannelBinding, now time.Time) error {
	defer lockUpstreamRatioChannel(b.ChannelID)()
	ch, err := model.GetChannelById(b.ChannelID, true)
	if err != nil {
		return handleBindingError(b, err)
	}
	rec, err := model.GetChannelUpstreamRatio(b.ChannelID)
	if err != nil {
		return err
	}
	code, err := writeBoundRatio(b, ch, rec, now)
	if err != nil {
		return handleBindingError(b, err)
	}
	if code != b.RatioSyncError {
		return model.UpdateUpstreamBinding(b.ID, map[string]any{"ratio_sync_error": code})
	}
	return nil
}
