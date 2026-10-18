package model

import (
	"context"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

// ChannelLabs selects only classification input. Secrets and addresses never
// enter the projection returned to the catalog or admin counts.
func ChannelLabs(ctx context.Context, ids []int) (map[int]modellab.Classification, error) {
	result := make(map[int]modellab.Classification, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var channels []struct {
		Id           int
		Models       string
		ModelMapping *string
	}
	if err := DB.WithContext(ctx).Model(&Channel{}).Select("id,models,model_mapping").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	var metadata []Model
	if err := DB.WithContext(ctx).Find(&metadata).Error; err != nil {
		return nil, err
	}
	var vendors []Vendor
	if err := DB.WithContext(ctx).Find(&vendors).Error; err != nil {
		return nil, err
	}
	vendorNames := map[int]string{}
	for _, v := range vendors {
		vendorNames[v.Id] = v.Name
	}
	names := []string{}
	for _, c := range channels {
		names = append(names, strings.Split(c.Models, ",")...)
		if c.ModelMapping != nil && strings.TrimSpace(*c.ModelMapping) != "" {
			mapping := map[string]string{}
			if err := common.UnmarshalJsonStr(*c.ModelMapping, &mapping); err != nil {
				return nil, err
			}
			for _, actual := range mapping {
				names = append(names, actual)
			}
		}
	}
	resolved := resolveModelMetadata(metadata, names)
	explicit := map[string]string{}
	for name, meta := range resolved {
		explicit[name] = vendorNames[meta.VendorID]
	}
	for _, c := range channels {
		mapping := ""
		if c.ModelMapping != nil {
			mapping = *c.ModelMapping
		}
		lab, err := modellab.ResolveChannel(strings.Split(c.Models, ","), mapping, explicit)
		if err != nil {
			return nil, err
		}
		result[c.Id] = lab
	}
	return result, nil
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
		// A count can combine vendor metadata, aliases and mappings.
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
