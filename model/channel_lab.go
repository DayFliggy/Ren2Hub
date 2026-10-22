package model

import (
	"context"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

var (
	labUnknown        = modellab.Classification{ID: "unknown", Name: "unknown", Source: "unknown"}
	labMixed          = modellab.Classification{ID: "mixed", Name: "mixed", Source: "models"}
	labInvalidMapping = modellab.Classification{ID: "unknown", Name: "unknown", Source: "invalid_mapping"}
)

// supplementVendorID 在原生 defaultVendorRules 没有命中时，用 modellab 推断 NewAPI 厂商。
// 定价页（initDefaultVendorMapping）和渠道分类共用这一处补充规则。
func supplementVendorID(modelName string, vendorID int, vendorMap map[int]*Vendor) int {
	if vendorID != 0 {
		return vendorID
	}
	name := modellab.VendorName(modelName)
	if name == "" {
		return 0
	}
	return getDisplayVendor(name, vendorMap)
}

type labChannel struct {
	id             int
	models         []string
	mapping        map[string]string
	invalidMapping bool
}

// labResolver 以 NewAPI 厂商为唯一词表：显式模型元数据优先，其次原生默认规则和补充推断。
type labResolver struct {
	explicit map[string]bool
	meta     map[string]*Model
	vendors  map[int]*Vendor
}

// ChannelLabs 只读取分类所需的列，密钥和地址不进入目录或管理计数。
func ChannelLabs(ctx context.Context, ids []int) (map[int]modellab.Classification, error) {
	result := make(map[int]modellab.Classification, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	channels, names, err := loadLabChannels(ctx, ids)
	if err != nil {
		return nil, err
	}
	resolver, err := newLabResolver(ctx, names)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		result[channel.id] = resolver.channel(channel)
	}
	return result, nil
}

func loadLabChannels(ctx context.Context, ids []int) ([]labChannel, []string, error) {
	var rows []struct {
		Id           int
		Models       string
		ModelMapping *string
	}
	if err := DB.WithContext(ctx).Model(&Channel{}).Select("id,models,model_mapping").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	channels := make([]labChannel, 0, len(rows))
	names := []string{}
	for _, row := range rows {
		channel := labChannel{id: row.Id, models: strings.Split(row.Models, ","), mapping: map[string]string{}}
		if row.ModelMapping != nil && strings.TrimSpace(*row.ModelMapping) != "" {
			// 原生转发对无法解析的映射直接报错；这里只把该渠道标为无效，不拖垮其他渠道和模型目录。
			if err := common.UnmarshalJsonStr(*row.ModelMapping, &channel.mapping); err != nil {
				channel.mapping = map[string]string{}
				channel.invalidMapping = true
			}
		}
		names = append(names, channel.models...)
		for _, actual := range channel.mapping {
			names = append(names, actual)
		}
		channels = append(channels, channel)
	}
	return channels, names, nil
}

// newLabResolver 与 updatePricing 相同：先解析模型元数据，再交给原生 initDefaultVendorMapping。
func newLabResolver(ctx context.Context, names []string) (labResolver, error) {
	var metadata []Model
	if err := DB.WithContext(ctx).Find(&metadata).Error; err != nil {
		return labResolver{}, err
	}
	var vendors []Vendor
	if err := DB.WithContext(ctx).Find(&vendors).Error; err != nil {
		return labResolver{}, err
	}
	resolver := labResolver{explicit: map[string]bool{}, meta: resolveModelMetadata(metadata, names), vendors: map[int]*Vendor{}}
	for i := range vendors {
		resolver.vendors[vendors[i].Id] = &vendors[i]
	}
	abilities := make([]AbilityWithChannel, 0, len(names))
	for _, name := range names {
		if _, exists := resolver.meta[name]; exists {
			resolver.explicit[name] = true
		}
		abilities = append(abilities, AbilityWithChannel{Ability: Ability{Model: name}})
	}
	initDefaultVendorMapping(resolver.meta, resolver.vendors, abilities)
	return resolver, nil
}

func (r labResolver) classify(name, source string) modellab.Classification {
	vendor := r.vendors[r.meta[name].VendorID]
	if vendor == nil {
		return labUnknown
	}
	return modellab.Classification{ID: modellab.VendorID(vendor.Name), Name: vendor.Name, Source: source}
}

// channel 只分类渠道公开的模型名；映射键不会给渠道增加模型，无法解析或成环的映射报告为无效。
func (r labResolver) channel(channel labChannel) modellab.Classification {
	if channel.invalidMapping {
		return labInvalidMapping
	}
	found := map[string]modellab.Classification{}
	for _, name := range channel.models {
		actual, ok := followMapping(name, channel.mapping)
		if !ok {
			return labInvalidMapping
		}
		var item modellab.Classification
		switch {
		case r.explicit[name]:
			item = r.classify(name, "vendor")
		case r.explicit[actual]:
			item = r.classify(actual, "vendor")
		case actual != name:
			item = r.classify(actual, "mapping")
		default:
			item = r.classify(name, "default")
		}
		found[item.ID] = item
	}
	if len(found) == 1 {
		for _, item := range found {
			return item
		}
	}
	if len(found) > 1 {
		return labMixed
	}
	return labUnknown
}

func followMapping(name string, mapping map[string]string) (string, bool) {
	seen := map[string]bool{}
	for mapping[name] != "" && mapping[name] != name {
		if seen[name] {
			return "", false
		}
		seen[name] = true
		name = mapping[name]
	}
	return name, true
}

func AllChannelLabs(ctx context.Context) (map[int]modellab.Classification, error) {
	var ids []int
	if err := DB.WithContext(ctx).Model(&Channel{}).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ChannelLabs(ctx, ids)
}

func AttachChannelLabs(ctx context.Context, channels []*Channel) error {
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.Id)
	}
	labs, err := ChannelLabs(ctx, ids)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		channel.Lab = labs[channel.Id]
	}
	return nil
}

type ChannelLabCount struct {
	modellab.Classification
	Count int `json:"count"`
}

func ChannelLabCounts(ctx context.Context) ([]ChannelLabCount, int, error) {
	labs, err := AllChannelLabs(ctx)
	if err != nil {
		return nil, 0, err
	}
	counts := map[string]ChannelLabCount{}
	for _, lab := range labs {
		item := counts[lab.ID]
		item.Classification = lab
		// 计数可能合并了元数据、默认规则和映射几种来源。
		item.Source = "aggregate"
		item.Count++
		counts[lab.ID] = item
	}
	items := make([]ChannelLabCount, 0, len(counts))
	for _, item := range counts {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, len(labs), nil
}
