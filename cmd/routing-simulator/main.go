package main

import (
	"fmt"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
)

type simulationCandidate struct {
	ChannelID         int     `json:"channel_id"`
	RequestModel      string  `json:"request_model"`
	ActualModel       string  `json:"actual_model"`
	Priority          int64   `json:"priority"`
	Position          int     `json:"position"`
	Weight            int     `json:"weight"`
	FilterReason      string  `json:"filter_reason"`
	HealthUsable      bool    `json:"health_usable"`
	ErrorRate         float64 `json:"error_rate"`
	ErrorRateKnown    bool    `json:"error_rate_known"`
	LatencyMS         float64 `json:"latency_ms"`
	LatencyKnown      bool    `json:"latency_known"`
	TTFTMS            float64 `json:"ttft_ms"`
	TTFTKnown         bool    `json:"ttft_known"`
	RateLimitHeadroom float64 `json:"rate_limit_headroom"`
	RateLimitKnown    bool    `json:"rate_limit_known"`
	QuotaHeadroom     float64 `json:"quota_headroom"`
	QuotaKnown        bool    `json:"quota_known"`
	Sticky            bool    `json:"sticky"`
	CompatibilityTier int     `json:"compatibility_tier"`
}

type simulationInput struct {
	Mode                  string                `json:"mode"`
	RequestID             string                `json:"request_id"`
	RequestModel          string                `json:"request_model"`
	TopK                  int                   `json:"top_k"`
	ManualGroupEnabled    bool                  `json:"manual_group_enabled"`
	ManualLoadBalance     bool                  `json:"manual_load_balance"`
	DynamicScoringEnabled bool                  `json:"dynamic_scoring_enabled"`
	Candidates            []simulationCandidate `json:"candidates"`
}

type simulationOutput struct {
	Decision                   service.RouteDecision `json:"decision"`
	CandidateChannelIDs        []int                 `json:"candidate_channel_ids"`
	ReserveCandidateChannelIDs []int                 `json:"reserve_candidate_channel_ids"`
}

func simulate(input simulationInput) (simulationOutput, error) {
	if input.Mode != string(service.RouteSourceManual) && input.Mode != string(service.RouteSourceAutoLab) {
		return simulationOutput{}, fmt.Errorf("mode must be manual or auto_lab")
	}
	candidates := make([]service.RouteSelectionCandidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		candidates = append(candidates, service.RouteSelectionCandidate{
			ChannelID: candidate.ChannelID, RequestModel: candidate.RequestModel, ActualModel: candidate.ActualModel,
			Priority: candidate.Priority, Position: candidate.Position, Weight: candidate.Weight,
			FilterReason: candidate.FilterReason, HealthUsable: candidate.HealthUsable,
			ErrorRate: candidate.ErrorRate, ErrorRateKnown: candidate.ErrorRateKnown,
			LatencyMS: candidate.LatencyMS, LatencyKnown: candidate.LatencyKnown,
			TTFTMS: candidate.TTFTMS, TTFTKnown: candidate.TTFTKnown,
			RateLimitHeadroom: candidate.RateLimitHeadroom, RateLimitKnown: candidate.RateLimitKnown,
			QuotaHeadroom: candidate.QuotaHeadroom, QuotaKnown: candidate.QuotaKnown,
			Sticky: candidate.Sticky, CompatibilityTier: candidate.CompatibilityTier,
		})
	}
	result, err := service.SelectTokenRoute(service.RouteSelectionInput{
		SourceInput:        service.RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: input.Mode},
		ManualGroupEnabled: input.ManualGroupEnabled, ManualLoadBalance: input.ManualLoadBalance,
		ManualCandidates: candidates, AutoCandidates: candidates,
		TopK: input.TopK, RequestID: input.RequestID, RequestModel: input.RequestModel,
		DynamicScoringEnabled: input.DynamicScoringEnabled,
	})
	if err != nil {
		return simulationOutput{}, err
	}
	output := simulationOutput{Decision: result.Decision}
	for _, candidate := range result.Candidates {
		output.CandidateChannelIDs = append(output.CandidateChannelIDs, candidate.ChannelID)
	}
	for _, candidate := range result.ReserveCandidates {
		output.ReserveCandidateChannelIDs = append(output.ReserveCandidateChannelIDs, candidate.ChannelID)
	}
	return output, nil
}

func main() {
	var input simulationInput
	if err := common.DecodeJson(os.Stdin, &input); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	output, err := simulate(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := common.Marshal(output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
