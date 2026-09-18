package relayconvert

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	geminichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/gemini_chat"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type ResponseConverterFunc func(c context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error)

type ResponseStreamConverterFunc func(c context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error)

type ResponseStreamStateFactory func(options ResponseStreamOptions) any

type ResponseStreamChunkConverterFunc func(c context.Context, info convmeta.Meta, response any, state any) ([]any, *dto.Usage, error)

type ResponseStreamFinalizerFunc func(c context.Context, info convmeta.Meta, state any) ([]any, *dto.Usage, error)

type ResponseConverterQuality string

const (
	ResponseConverterQualityGood        ResponseConverterQuality = "good"
	ResponseConverterQualityFair        ResponseConverterQuality = "fair"
	ResponseConverterQualityDiscouraged ResponseConverterQuality = "discouraged"
)

type ResponseStep struct {
	Converter string
	From      types.RelayFormat
	To        types.RelayFormat
}

type ResponseResult struct {
	Termination types.ResponseTermination
	Diagnostics []types.ConversionDiagnostic
	Value       any
	Usage       *dto.Usage
	From        types.RelayFormat
	To          types.RelayFormat
	Converter   string
	Quality     ResponseConverterQuality
	Steps       []ResponseStep
	Stream      bool
}

type ResponseConverterSpec struct {
	ID                 string
	From               types.RelayFormat
	To                 types.RelayFormat
	Quality            ResponseConverterQuality
	Convert            ResponseConverterFunc
	ConvertStream      ResponseStreamConverterFunc
	NewStreamState     ResponseStreamStateFactory
	ConvertStreamChunk ResponseStreamChunkConverterFunc
	FinalizeStream     ResponseStreamFinalizerFunc
	StepConverters     []string
}

type responseConverterRoute struct {
	from types.RelayFormat
	to   types.RelayFormat
}

type ResponseStreamOptions struct {
	// MaxInputBytes bounds all source frames retained/processed by a conversion
	// session, including text, arguments, citations and final snapshots. Zero
	// uses 128 MiB, matching the hosting gateway's default SSE event bound.
	MaxInputBytes int64
	ID            string
	Model         string
	Created       int64
	IncludeUsage  bool
}

type ResponseStreamState struct {
	From      types.RelayFormat
	To        types.RelayFormat
	Converter string
	Quality   ResponseConverterQuality
	Steps     []ResponseStep

	specs              []ResponseConverterSpec
	stepStates         []any
	usage              *dto.Usage
	finalized          bool
	termination        types.ResponseTermination
	diagnostics        []types.ConversionDiagnostic
	pendingDiagnostics []types.ConversionDiagnostic
	artifacts          *responseArtifactStreamState
	fallbackInfo       *convmeta.Values
	conversionError    error
	choiceTerminations map[int]types.ResponseTermination
	maxInputBytes      int64
	inputBytes         int64
	terminalSnapshot   string
}

const (
	ResponseConverterOAIChatToOAIResponses   = "oai_chat_to_oai_responses_resp"
	ResponseConverterOAIResponsesToOAIChat   = "oai_responses_to_oai_chat_resp"
	ResponseConverterOAIChatToClaudeMessages = "oai_chat_to_claude_messages_resp"
	ResponseConverterOAIChatToGeminiChat     = "oai_chat_to_gemini_chat_resp"
	ResponseConverterClaudeMessagesToOAIChat = "claude_messages_to_oai_chat_resp"
	ResponseConverterGeminiChatToOAIChat     = "gemini_chat_to_oai_chat_resp"

	responseConverterClaudeToGemini    = "claude_messages_to_gemini_chat_resp"
	responseConverterClaudeToResponses = "claude_messages_to_oai_responses_resp"
	responseConverterGeminiToClaude    = "gemini_chat_to_claude_messages_resp"
	responseConverterGeminiToResponses = "gemini_chat_to_oai_responses_resp"
	responseConverterResponsesToClaude = "oai_responses_to_claude_messages_resp"
	responseConverterResponsesToGemini = "oai_responses_to_gemini_chat_resp"
)

var (
	responseConverterMu      sync.RWMutex
	responseConverters       = make(map[string]ResponseConverterSpec)
	responseConverterAliases = make(map[string]string)
	responseConverterRoutes  = make(map[responseConverterRoute]string)
)

func registerBuiltinResponseConverter(spec ResponseConverterSpec) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		panic("response converter ID is required")
	}
	if spec.From == "" || spec.To == "" {
		panic(fmt.Sprintf("response converter %q must declare from and to formats", spec.ID))
	}
	if spec.Quality == "" {
		panic(fmt.Sprintf("response converter %q must declare quality", spec.ID))
	}
	if spec.Convert == nil &&
		spec.ConvertStream == nil &&
		spec.ConvertStreamChunk == nil &&
		len(spec.StepConverters) == 0 {
		panic(fmt.Sprintf("response converter %q must declare convert, stream convert, or step converters", spec.ID))
	}
	if len(spec.StepConverters) > 0 &&
		(spec.Convert != nil || spec.ConvertStream != nil || spec.NewStreamState != nil || spec.ConvertStreamChunk != nil || spec.FinalizeStream != nil) {
		panic(fmt.Sprintf("response converter %q cannot declare direct implementations and step converters together", spec.ID))
	}
	if _, exists := responseConverters[spec.ID]; exists {
		panic(fmt.Sprintf("response converter %q is already registered", spec.ID))
	}
	route := responseConverterRoute{from: spec.From, to: spec.To}
	if existingID, exists := responseConverterRoutes[route]; exists {
		panic(fmt.Sprintf("response converter route from %s to %s is already registered by %q", spec.From, spec.To, existingID))
	}

	if len(spec.StepConverters) > 0 {
		stepConverters := make([]string, 0, len(spec.StepConverters))
		current := spec.From
		for _, converterID := range spec.StepConverters {
			step, ok := responseConverters[converterID]
			if !ok {
				panic(fmt.Sprintf("response converter %q references unknown step converter %q", spec.ID, converterID))
			}
			if len(step.StepConverters) > 0 {
				panic(fmt.Sprintf("response converter %q step %q must be a direct converter", spec.ID, converterID))
			}
			if step.From != current {
				panic(fmt.Sprintf("response converter %q step %q expects %s after %s", spec.ID, converterID, step.From, current))
			}
			stepConverters = append(stepConverters, converterID)
			current = step.To
		}
		if current != spec.To {
			panic(fmt.Sprintf("response converter %q ends at %s, expected %s", spec.ID, current, spec.To))
		}
		spec.StepConverters = stepConverters
	}

	responseConverters[spec.ID] = spec
	responseConverterRoutes[route] = spec.ID
}

func registerResponseConverterAlias(alias string, converter string) {
	alias = strings.TrimSpace(alias)
	converter = strings.TrimSpace(converter)
	if alias == "" {
		panic("response converter alias is required")
	}
	if converter == "" {
		panic(fmt.Sprintf("response converter alias %q target is required", alias))
	}
	if alias == converter {
		return
	}
	if _, exists := responseConverters[alias]; exists {
		panic(fmt.Sprintf("response converter alias %q conflicts with registered converter", alias))
	}
	if _, exists := responseConverters[converter]; !exists {
		panic(fmt.Sprintf("response converter alias %q references unknown converter %q", alias, converter))
	}
	if existing, exists := responseConverterAliases[alias]; exists && existing != converter {
		panic(fmt.Sprintf("response converter alias %q is already registered for %q", alias, existing))
	}
	responseConverterAliases[alias] = converter
}

func LookupResponseConverter(converter string) (ResponseConverterSpec, bool) {
	responseConverterMu.RLock()
	defer responseConverterMu.RUnlock()

	converterID := resolveResponseConverterID(converter)
	spec, ok := responseConverters[converterID]
	if !ok {
		return ResponseConverterSpec{}, false
	}
	return cloneResponseConverterSpec(spec), true
}

func ConvertResponse(c context.Context, info convmeta.Meta, target types.RelayFormat, response any) (*ResponseResult, error) {
	from, err := inferResponseRelayFormat(response)
	if err != nil {
		return nil, err
	}
	if target == "" {
		return nil, errors.New("target relay format is required")
	}
	if from == target {
		return &ResponseResult{
			Termination: TerminationFromResponse(response),
			Value:       response,
			Usage:       canonicalUsageFromResponse(response),
			From:        from,
			To:          target,
			Stream:      false,
		}, nil
	}

	spec, ok := lookupResponseRoute(from, target)
	if !ok {
		return nil, fmt.Errorf("response converter from %s to %s is not registered", from, target)
	}
	return executeResponseSpec(c, info, from, target, response, spec)
}

func ConvertResponseByID(c context.Context, info convmeta.Meta, converter string, response any) (*ResponseResult, error) {
	from, err := inferResponseRelayFormat(response)
	if err != nil {
		return nil, err
	}

	spec, ok := LookupResponseConverter(converter)
	if !ok {
		return nil, fmt.Errorf("response converter %q is not registered", strings.TrimSpace(converter))
	}
	if spec.From != "" && spec.From != from {
		return nil, fmt.Errorf("response converter %q expects %s response, got %s", spec.ID, spec.From, from)
	}
	return executeResponseSpec(c, info, from, spec.To, response, spec)
}

func ConvertStreamResponse(c context.Context, info convmeta.Meta, target types.RelayFormat, response any) (*ResponseResult, error) {
	from, err := inferResponseRelayFormat(response)
	if err != nil {
		return nil, err
	}
	if target == "" {
		return nil, errors.New("target relay format is required")
	}
	if from == target {
		return &ResponseResult{
			Termination: TerminationFromResponse(response),
			Value:       response,
			Usage:       canonicalUsageFromResponse(response),
			From:        from,
			To:          target,
			Stream:      true,
		}, nil
	}

	spec, ok := lookupResponseRoute(from, target)
	if !ok {
		return nil, fmt.Errorf("response converter from %s to %s is not registered", from, target)
	}
	return executeStatelessStreamResponseSpec(c, info, from, target, response, spec)
}

func NewResponseStreamState(from types.RelayFormat, target types.RelayFormat, options ResponseStreamOptions) (*ResponseStreamState, error) {
	if from == "" {
		return nil, errors.New("source relay format is required")
	}
	if target == "" {
		return nil, errors.New("target relay format is required")
	}
	if from == target {
		return &ResponseStreamState{
			From:          from,
			To:            target,
			maxInputBytes: responseStreamInputLimit(options),
		}, nil
	}

	spec, ok := lookupResponseRoute(from, target)
	if !ok {
		return nil, fmt.Errorf("response converter from %s to %s is not registered", from, target)
	}
	return newResponseStreamStateFromSpec(from, target, options, spec)
}

func NewResponseStreamStateByID(converter string, options ResponseStreamOptions) (*ResponseStreamState, error) {
	spec, ok := LookupResponseConverter(converter)
	if !ok {
		return nil, fmt.Errorf("response converter %q is not registered", strings.TrimSpace(converter))
	}
	return newResponseStreamStateFromSpec(spec.From, spec.To, options, spec)
}

func ConvertStreamResponseChunk(c context.Context, info convmeta.Meta, state *ResponseStreamState, response any) ([]ResponseResult, error) {
	if state == nil {
		return nil, errors.New("response stream state is required")
	}
	if state.finalized {
		return nil, errors.New("response stream is already finalized")
	}
	if state.conversionError != nil {
		return nil, state.conversionError
	}
	from, err := inferResponseRelayFormat(response)
	if err != nil {
		return nil, err
	}
	if from != state.From {
		return nil, fmt.Errorf("response stream converter %q expects %s response, got %s", state.Converter, state.From, from)
	}
	terminal := TerminationFromResponse(response)
	state.observeResponseTermination(response)
	state.rememberUsage(canonicalUsageFromResponse(response))
	if err := ValidateResponseTermination(state.From, state.To, terminal); err != nil {
		state.conversionError = err
		return nil, err
	}
	if state.From == state.To {
		usage := canonicalUsageFromResponse(response)
		state.rememberUsage(usage)
		return responseStreamResults(state, streamValuesFromAny(response), usage), nil
	}
	if err := validateResponseAnnotations(response); err != nil {
		state.conversionError = err
		return nil, err
	}
	encoded, err := kitutil.Marshal(response)
	if err != nil {
		state.conversionError = err
		return nil, err
	}
	if int64(len(encoded)) > state.maxInputBytes-state.inputBytes {
		state.conversionError = fmt.Errorf("response_conversion_limit: stream conversion input exceeds configured byte limit")
		return nil, state.conversionError
	}
	state.inputBytes += int64(len(encoded))
	if duplicate, err := state.checkTerminalSnapshot(response); duplicate || err != nil {
		if err != nil {
			state.conversionError = err
		}
		return nil, err
	}

	if info == nil {
		if state.fallbackInfo == nil {
			state.fallbackInfo = &convmeta.Values{}
		}
		info = state.fallbackInfo
	}
	var hosted []any
	if state.From != types.RelayFormatGemini {
		hosted, err = state.convertHostedStreamArtifacts(response, info)
		if err != nil {
			state.conversionError = err
			return nil, err
		}
	}
	values, usage, err := executeResponseStreamSteps(c, info, state, []any{response}, 0)
	if err != nil {
		state.conversionError = err
		return nil, err
	}
	if state.From == types.RelayFormatGemini {
		hosted, err = state.convertHostedStreamArtifacts(response, info)
		if err != nil {
			state.conversionError = err
			return nil, err
		}
		values = append(values, hosted...)
		hosted = nil
	}
	state.rememberUsage(usage)
	return responseStreamResults(state, append(hosted, values...), usage), nil
}

func FinalizeStreamResponse(c context.Context, info convmeta.Meta, state *ResponseStreamState) ([]ResponseResult, error) {
	return FinalizeStreamResponseWithOutcome(c, info, state, StreamOutcomeCompleted)
}

func finalizeStreamResponse(c context.Context, info convmeta.Meta, state *ResponseStreamState) ([]ResponseResult, error) {
	if state == nil {
		return nil, errors.New("response stream state is required")
	}
	if state.finalized {
		return nil, nil
	}
	state.finalized = true
	if info == nil && state.fallbackInfo != nil {
		info = state.fallbackInfo
	}
	if state.From == state.To {
		return nil, nil
	}

	if state.To == types.RelayFormatClaude && info != nil {
		claudeInfo := info.EnsureClaudeConvertInfo()
		if claudeInfo.Usage == nil {
			claudeInfo.Usage = state.Usage()
		}
	}

	values := make([]any, 0)
	var usage *dto.Usage
	for i, spec := range state.specs {
		finalValues, stepUsage, err := finalizeResponseStreamStep(c, info, spec, state.stepStates[i])
		if err != nil {
			return nil, err
		}
		if stepUsage != nil {
			usage = stepUsage
			state.rememberUsage(stepUsage)
		}
		if len(finalValues) == 0 {
			continue
		}
		current, currentUsage, err := executeResponseStreamSteps(c, info, state, finalValues, i+1)
		if err != nil {
			return nil, err
		}
		if currentUsage != nil {
			usage = currentUsage
			state.rememberUsage(currentUsage)
		}
		values = append(values, current...)
	}
	return responseStreamResults(state, values, usage), nil
}

func (s *ResponseStreamState) Usage() *dto.Usage {
	if s == nil {
		return nil
	}
	if s.usage != nil {
		return s.usage
	}
	for _, state := range s.stepStates {
		switch typed := state.(type) {
		case *ChatToResponsesStreamState:
			if typed.Usage != nil {
				return typed.Usage
			}
		case *ResponsesToChatStreamState:
			if typed.Usage != nil {
				return typed.Usage
			}
		}
	}
	return nil
}

func (s *ResponseStreamState) Termination() types.ResponseTermination {
	if s == nil {
		return types.ResponseTermination{}
	}
	return s.termination
}

func (s *ResponseStreamState) Diagnostics() []types.ConversionDiagnostic {
	if s == nil {
		return nil
	}
	return append([]types.ConversionDiagnostic(nil), s.diagnostics...)
}

func (s *ResponseStreamState) SetUsage(usage *dto.Usage) {
	if s == nil || usage == nil {
		return
	}
	s.usage = usage
	for _, state := range s.stepStates {
		switch typed := state.(type) {
		case *ChatToResponsesStreamState:
			typed.Usage = UsageFromChatUsage(usage)
		case *ResponsesToChatStreamState:
			typed.Usage = usage
		}
	}
}

func (s *ResponseStreamState) UsageText() string {
	if s == nil {
		return ""
	}
	for _, state := range s.stepStates {
		switch typed := state.(type) {
		case interface{ UsageText() string }:
			if text := typed.UsageText(); text != "" {
				return text
			}
		}
	}
	return ""
}

func executeResponseSpec(c context.Context, info convmeta.Meta, from types.RelayFormat, target types.RelayFormat, response any, spec ResponseConverterSpec) (*ResponseResult, error) {
	steps, err := expandResponseConverterSteps(spec)
	if err != nil {
		return nil, err
	}
	return executeResponseSteps(c, info, from, target, response, spec.ID, spec.Quality, steps)
}

func executeResponseSteps(c context.Context, info convmeta.Meta, from types.RelayFormat, target types.RelayFormat, response any, converter string, quality ResponseConverterQuality, specs []ResponseConverterSpec) (*ResponseResult, error) {
	termination := TerminationFromResponse(response)
	if from != target && !termination.IsTerminal() {
		return nil, ErrResponseTerminalMissing
	}
	if err := ValidateResponseTermination(from, target, termination); err != nil {
		return nil, err
	}
	if err := validateResponseAnnotations(response); err != nil {
		return nil, err
	}
	current, artifacts := extractResponseArtifacts(from, response)
	var usage *dto.Usage
	steps := make([]ResponseStep, 0, len(specs))
	for _, spec := range specs {
		var step ResponseStep
		var err error
		current, usage, step, err = executeResponseStep(c, info, spec, current)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	current, diagnostics, err := attachResponseArtifacts(target, current, artifacts, info)
	if err != nil {
		return nil, err
	}
	converters := make([]string, 0, len(steps))
	for _, step := range steps {
		converters = append(converters, step.Converter)
	}
	if converter == "" {
		converter = strings.Join(converters, ",")
	}
	return &ResponseResult{
		Termination: termination,
		Diagnostics: diagnostics,
		Value:       current,
		Usage:       usage,
		From:        from,
		To:          target,
		Converter:   converter,
		Quality:     quality,
		Steps:       steps,
		Stream:      false,
	}, nil
}

func executeResponseStep(c context.Context, info convmeta.Meta, spec ResponseConverterSpec, response any) (any, *dto.Usage, ResponseStep, error) {
	if spec.Convert == nil {
		return nil, nil, ResponseStep{}, fmt.Errorf("response converter %q has no non-stream implementation", spec.ID)
	}

	value, usage, err := spec.Convert(c, info, response)
	if err != nil {
		return nil, nil, ResponseStep{}, err
	}
	return value, usage, ResponseStep{
		Converter: spec.ID,
		From:      spec.From,
		To:        spec.To,
	}, nil
}

func executeStatelessStreamResponseSpec(c context.Context, info convmeta.Meta, from types.RelayFormat, target types.RelayFormat, response any, spec ResponseConverterSpec) (*ResponseResult, error) {
	termination := TerminationFromResponse(response)
	if err := ValidateResponseTermination(from, target, termination); err != nil {
		return nil, err
	}
	if err := validateResponseAnnotations(response); err != nil {
		return nil, err
	}
	_, artifacts := extractResponseArtifacts(from, response)
	switch value := response.(type) {
	case *dto.ClaudeResponse:
		if value.ContentBlock != nil && isClaudeHostedResponse(value.ContentBlock.Type) {
			return nil, fmt.Errorf("hosted_response_state_required: use stateful response conversion")
		}
	case *dto.ResponsesStreamResponse:
		if value.Item != nil && isResponsesHostedResponse(value.Item.Type) {
			return nil, fmt.Errorf("hosted_response_state_required: use stateful response conversion")
		}
	}
	if len(artifacts.claude) > 0 || len(artifacts.responses) > 0 || len(artifacts.diagnostics) > 0 {
		return nil, fmt.Errorf("response_artifact_state_required: use stateful response conversion")
	}
	steps, err := expandResponseConverterSteps(spec)
	if err != nil {
		return nil, err
	}
	current := response
	var usage *dto.Usage
	resultSteps := make([]ResponseStep, 0, len(steps))
	for _, step := range steps {
		if step.ConvertStream == nil {
			return nil, fmt.Errorf("response converter %q has no stream implementation", step.ID)
		}
		var err error
		current, usage, err = step.ConvertStream(c, info, current)
		if err != nil {
			return nil, err
		}
		resultSteps = append(resultSteps, ResponseStep{
			Converter: step.ID,
			From:      step.From,
			To:        step.To,
		})
	}
	return &ResponseResult{
		Termination: termination,
		Value:       current,
		Usage:       usage,
		From:        from,
		To:          target,
		Converter:   spec.ID,
		Quality:     spec.Quality,
		Steps:       resultSteps,
		Stream:      true,
	}, nil
}

func newResponseStreamStateFromSpec(from types.RelayFormat, target types.RelayFormat, options ResponseStreamOptions, spec ResponseConverterSpec) (*ResponseStreamState, error) {
	steps, err := expandResponseConverterSteps(spec)
	if err != nil {
		return nil, err
	}
	stepStates := make([]any, len(steps))
	resultSteps := make([]ResponseStep, 0, len(steps))
	for i, step := range steps {
		if step.NewStreamState != nil {
			stepStates[i] = step.NewStreamState(options)
		}
		resultSteps = append(resultSteps, ResponseStep{
			Converter: step.ID,
			From:      step.From,
			To:        step.To,
		})
	}
	return &ResponseStreamState{
		From:          from,
		To:            target,
		Converter:     spec.ID,
		Quality:       spec.Quality,
		Steps:         resultSteps,
		specs:         steps,
		stepStates:    stepStates,
		maxInputBytes: responseStreamInputLimit(options),
	}, nil
}

func responseStreamInputLimit(options ResponseStreamOptions) int64 {
	if options.MaxInputBytes > 0 {
		return options.MaxInputBytes
	}
	return 128 << 20
}

func (s *ResponseStreamState) checkTerminalSnapshot(response any) (bool, error) {
	var event *dto.ResponsesStreamResponse
	switch value := response.(type) {
	case *dto.ResponsesStreamResponse:
		event = value
	case dto.ResponsesStreamResponse:
		event = &value
	default:
		return false, nil
	}
	if !TerminationFromResponse(event).IsTerminal() {
		return false, nil
	}
	encoded, err := kitutil.Marshal(event.Response)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(encoded)
	snapshot := string(digest[:])
	if s.terminalSnapshot != "" {
		if s.terminalSnapshot != snapshot {
			return false, fmt.Errorf("response_terminal_conflict: upstream changed its terminal snapshot")
		}
		return true, nil
	}
	s.terminalSnapshot = snapshot
	return false, nil
}

func executeResponseStreamSteps(c context.Context, info convmeta.Meta, state *ResponseStreamState, values []any, start int) ([]any, *dto.Usage, error) {
	current := values
	var usage *dto.Usage
	for i := start; i < len(state.specs); i++ {
		spec := state.specs[i]
		next := make([]any, 0)
		for _, value := range current {
			prepareResponseStreamInfo(info, spec)
			stepValues, stepUsage, err := executeResponseStreamStep(c, info, spec, state.stepStates[i], value)
			if err != nil {
				return nil, nil, err
			}
			if stepUsage != nil {
				usage = stepUsage
				state.rememberUsage(stepUsage)
			}
			next = append(next, stepValues...)
		}
		current = next
		if len(current) == 0 {
			return nil, usage, nil
		}
	}
	return current, usage, nil
}

func prepareResponseStreamInfo(info convmeta.Meta, spec ResponseConverterSpec) {
	if info == nil {
		return
	}
	if spec.From != types.RelayFormatOpenAI {
		return
	}
	if spec.To != types.RelayFormatClaude && spec.To != types.RelayFormatGemini {
		return
	}
	info.IncrSendResponseCount()
}

func executeResponseStreamStep(c context.Context, info convmeta.Meta, spec ResponseConverterSpec, state any, response any) ([]any, *dto.Usage, error) {
	if spec.ConvertStreamChunk != nil {
		return spec.ConvertStreamChunk(c, info, response, state)
	}
	if spec.ConvertStream == nil {
		return nil, nil, fmt.Errorf("response converter %q has no stream implementation", spec.ID)
	}
	value, usage, err := spec.ConvertStream(c, info, response)
	if err != nil {
		return nil, nil, err
	}
	return streamValuesFromAny(value), usage, nil
}

func finalizeResponseStreamStep(c context.Context, info convmeta.Meta, spec ResponseConverterSpec, state any) ([]any, *dto.Usage, error) {
	if spec.FinalizeStream == nil {
		return nil, nil, nil
	}
	return spec.FinalizeStream(c, info, state)
}

func (s *ResponseStreamState) rememberUsage(usage *dto.Usage) {
	if s != nil && usage != nil {
		s.usage = usage
	}
}

func responseStreamResults(state *ResponseStreamState, values []any, usage *dto.Usage) []ResponseResult {
	if state == nil || len(values) == 0 {
		return nil
	}
	results := make([]ResponseResult, 0, len(values))
	for i, value := range values {
		var diagnostics []types.ConversionDiagnostic
		if i == 0 {
			diagnostics = append([]types.ConversionDiagnostic(nil), state.pendingDiagnostics...)
			state.pendingDiagnostics = nil
		}
		results = append(results, ResponseResult{
			Termination: state.termination,
			Diagnostics: diagnostics,
			Value:       value,
			Usage:       usage,
			From:        state.From,
			To:          state.To,
			Converter:   state.Converter,
			Quality:     state.Quality,
			Steps:       append([]ResponseStep{}, state.Steps...),
			Stream:      true,
		})
	}
	return results
}

func streamValuesFromAny(value any) []any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return []any{value}
	}
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		return []any{value}
	}
	values := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Pointer && item.IsNil() {
			continue
		}
		values = append(values, item.Interface())
	}
	return values
}

func expandResponseConverterSteps(spec ResponseConverterSpec) ([]ResponseConverterSpec, error) {
	if len(spec.StepConverters) == 0 {
		if spec.Convert == nil && spec.ConvertStream == nil && spec.ConvertStreamChunk == nil {
			return nil, fmt.Errorf("response converter %q has no registered implementation", spec.ID)
		}
		return []ResponseConverterSpec{spec}, nil
	}

	steps := make([]ResponseConverterSpec, 0, len(spec.StepConverters))
	current := spec.From
	for _, converterID := range spec.StepConverters {
		step, ok := LookupResponseConverter(converterID)
		if !ok {
			return nil, fmt.Errorf("response converter %q references missing step converter %q", spec.ID, converterID)
		}
		if len(step.StepConverters) > 0 {
			return nil, fmt.Errorf("response converter %q step %q is not a direct converter", spec.ID, converterID)
		}
		if step.From != current {
			return nil, fmt.Errorf("response converter %q step %q expects %s response, got %s", spec.ID, converterID, step.From, current)
		}
		steps = append(steps, step)
		current = step.To
	}
	if current != spec.To {
		return nil, fmt.Errorf("response converter %q ends at %s, expected %s", spec.ID, current, spec.To)
	}
	return steps, nil
}

func lookupResponseRoute(from types.RelayFormat, to types.RelayFormat) (ResponseConverterSpec, bool) {
	responseConverterMu.RLock()
	defer responseConverterMu.RUnlock()

	converterID, ok := responseConverterRoutes[responseConverterRoute{from: from, to: to}]
	if !ok {
		return ResponseConverterSpec{}, false
	}
	spec, ok := responseConverters[converterID]
	return cloneResponseConverterSpec(spec), ok
}

func resolveResponseConverterID(converter string) string {
	converter = strings.TrimSpace(converter)
	if canonical, ok := responseConverterAliases[converter]; ok {
		return canonical
	}
	return converter
}

func cloneResponseConverterSpec(spec ResponseConverterSpec) ResponseConverterSpec {
	if len(spec.StepConverters) > 0 {
		spec.StepConverters = append([]string{}, spec.StepConverters...)
	}
	return spec
}

func inferResponseRelayFormat(response any) (types.RelayFormat, error) {
	if isNilResponse(response) {
		return "", errors.New("response is nil")
	}
	switch response.(type) {
	case *dto.OpenAITextResponse, dto.OpenAITextResponse, *dto.ChatCompletionsStreamResponse, dto.ChatCompletionsStreamResponse:
		return types.RelayFormatOpenAI, nil
	case *dto.OpenAIResponsesResponse, dto.OpenAIResponsesResponse, *dto.ResponsesStreamResponse, dto.ResponsesStreamResponse:
		return types.RelayFormatOpenAIResponses, nil
	case *dto.ClaudeResponse, dto.ClaudeResponse:
		return types.RelayFormatClaude, nil
	case *dto.GeminiChatResponse, dto.GeminiChatResponse:
		return types.RelayFormatGemini, nil
	default:
		return "", fmt.Errorf("unsupported response type %T", response)
	}
}

func isNilResponse(response any) bool {
	if response == nil {
		return true
	}
	value := reflect.ValueOf(response)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func canonicalUsageFromResponse(response any) *dto.Usage {
	switch resp := response.(type) {
	case *dto.OpenAITextResponse:
		return UsageFromChatUsage(&resp.Usage)
	case dto.OpenAITextResponse:
		return UsageFromChatUsage(&resp.Usage)
	case *dto.ChatCompletionsStreamResponse:
		if resp.Usage == nil {
			return nil
		}
		return UsageFromChatUsage(resp.Usage)
	case dto.ChatCompletionsStreamResponse:
		if resp.Usage == nil {
			return nil
		}
		return UsageFromChatUsage(resp.Usage)
	case *dto.OpenAIResponsesResponse:
		return UsageFromResponsesUsage(resp.Usage)
	case dto.OpenAIResponsesResponse:
		return UsageFromResponsesUsage(resp.Usage)
	case *dto.ResponsesStreamResponse:
		if resp.Response == nil {
			return nil
		}
		return UsageFromResponsesUsage(resp.Response.Usage)
	case dto.ResponsesStreamResponse:
		if resp.Response == nil {
			return nil
		}
		return UsageFromResponsesUsage(resp.Response.Usage)
	case *dto.ClaudeResponse:
		return usageFromClaudeResponse(resp)
	case dto.ClaudeResponse:
		return usageFromClaudeResponse(&resp)
	case *dto.GeminiChatResponse:
		return UsageFromGeminiMetadata(resp.GetUsageMetadata(), 0)
	case dto.GeminiChatResponse:
		return UsageFromGeminiMetadata(resp.GetUsageMetadata(), 0)
	default:
		return nil
	}
}

func usageFromClaudeResponse(resp *dto.ClaudeResponse) *dto.Usage {
	if resp == nil {
		return nil
	}
	if resp.Usage != nil {
		return UsageFromClaudeAPIUsage(resp.Usage)
	}
	if resp.Message != nil && resp.Message.Usage != nil {
		return UsageFromClaudeAPIUsage(resp.Message.Usage)
	}
	return nil
}

func convertOAIChatResponseToOAIResponses(_ context.Context, _ convmeta.Meta, response any) (any, *dto.Usage, error) {
	chatResponse, err := asOAIChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	id := strings.TrimSpace(chatResponse.Id)
	if id == "" {
		id = fmt.Sprintf("resp_%s", kitutil.GetUUID())
	}
	return ChatCompletionsResponseToResponsesResponse(chatResponse, id)
}

func convertOAIResponsesResponseToOAIChat(_ context.Context, _ convmeta.Meta, response any) (any, *dto.Usage, error) {
	responsesResponse, err := asOAIResponsesResponse(response)
	if err != nil {
		return nil, nil, err
	}
	id := strings.TrimSpace(responsesResponse.ID)
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID())
	}
	return ResponsesResponseToChatCompletionsResponse(responsesResponse, id)
}

func newOAIChatToOAIResponsesStreamState(options ResponseStreamOptions) any {
	id := strings.TrimSpace(options.ID)
	if id == "" {
		id = fmt.Sprintf("resp_%s", kitutil.GetUUID())
	}
	state := NewChatToResponsesStreamState(id, strings.TrimSpace(options.Model))
	if options.Created != 0 {
		state.Created = options.Created
	}
	return state
}

func convertOAIChatStreamResponseToOAIResponses(_ context.Context, _ convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	chatResponse, err := asOAIChatStreamResponse(response)
	if err != nil {
		return nil, nil, err
	}
	streamState, ok := state.(*ChatToResponsesStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("OAI chat to OAI responses stream state is required")
	}
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chatResponse, streamState)
	if err != nil {
		return nil, nil, err
	}
	return streamValuesFromAny(events), streamState.Usage, nil
}

func finalizeOAIChatStreamResponseToOAIResponses(_ context.Context, _ convmeta.Meta, state any) ([]any, *dto.Usage, error) {
	streamState, ok := state.(*ChatToResponsesStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("OAI chat to OAI responses stream state is required")
	}
	events := FinalizeChatCompletionsStreamToResponses(streamState)
	return streamValuesFromAny(events), streamState.Usage, nil
}

func newOAIResponsesToOAIChatStreamState(options ResponseStreamOptions) any {
	state := NewResponsesToChatStreamState(strings.TrimSpace(options.Model), options.IncludeUsage)
	state.ID = strings.TrimSpace(options.ID)
	if options.Created != 0 {
		state.Created = options.Created
	}
	return state
}

func convertOAIResponsesStreamResponseToOAIChat(_ context.Context, _ convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	responsesResponse, err := asOAIResponsesStreamResponse(response)
	if err != nil {
		return nil, nil, err
	}
	streamState, ok := state.(*ResponsesToChatStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("OAI responses to OAI chat stream state is required")
	}
	chunks, err := ResponsesStreamEventToChatChunks(responsesResponse, streamState)
	if err != nil {
		return nil, nil, err
	}
	return streamValuesFromAny(chunks), streamState.Usage, nil
}

func finalizeOAIResponsesStreamResponseToOAIChat(_ context.Context, _ convmeta.Meta, state any) ([]any, *dto.Usage, error) {
	streamState, ok := state.(*ResponsesToChatStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("OAI responses to OAI chat stream state is required")
	}
	chunks := FinalizeResponsesToChatStream(streamState)
	return streamValuesFromAny(chunks), streamState.Usage, nil
}

func convertOAIChatResponseToClaudeMessages(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	chatResponse, err := asOAIChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	if len(chatResponse.Choices) > 1 || (len(chatResponse.Choices) == 1 && chatResponse.Choices[0].Index != 0) {
		return nil, nil, errors.New("Claude response conversion cannot represent multiple choices")
	}
	for _, choice := range chatResponse.Choices {
		if _, err := oaichat.CitationTextBlocks(choice.Message.StringContent(), choice.Message.Annotations); err != nil {
			return nil, nil, err
		}
	}
	return ResponseOpenAI2Claude(chatResponse, info), UsageFromChatUsage(&chatResponse.Usage), nil
}

func convertOAIChatStreamResponseToClaudeMessages(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	chatResponse, err := asOAIChatStreamResponse(response)
	if err != nil {
		return nil, nil, err
	}
	if len(chatResponse.Choices) > 1 || (len(chatResponse.Choices) == 1 && chatResponse.Choices[0].Index != 0) {
		return nil, nil, errors.New("Claude stream conversion cannot represent multiple choices")
	}
	converted := StreamResponseOpenAI2Claude(chatResponse, info)
	if info != nil && info.EnsureClaudeConvertInfo().ResponseError != nil {
		return nil, canonicalUsageFromResponse(chatResponse), info.EnsureClaudeConvertInfo().ResponseError
	}
	return converted, canonicalUsageFromResponse(chatResponse), nil
}

func finalizeOAIChatStreamResponseToClaudeMessages(_ context.Context, info convmeta.Meta, _ any) ([]any, *dto.Usage, error) {
	if info == nil {
		info = &convmeta.Values{}
	}
	usage := info.EnsureClaudeConvertInfo().Usage
	responses := oaichat.FinalizeStreamResponseOpenAI2Claude(info)
	return streamValuesFromAny(responses), usage, nil
}

func convertOAIChatResponseToGeminiChat(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	chatResponse, err := asOAIChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	converted := ResponseOpenAI2Gemini(chatResponse, info)
	for i := range chatResponse.Choices {
		metadata, err := oaichat.ChatAnnotationsToGemini(chatResponse.Choices[i].Message.Annotations, chatResponse.Choices[i].Message.StringContent())
		if err != nil {
			return nil, nil, err
		}
		converted.Candidates[i].GroundingMetadata = metadata
	}
	return converted, UsageFromChatUsage(&chatResponse.Usage), nil
}

func convertOAIChatStreamResponseToGeminiChat(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	chatResponse, err := asOAIChatStreamResponse(response)
	if err != nil {
		return nil, nil, err
	}
	state := oaichat.GeminiStreamState(info)
	converted := oaichat.StreamResponseOpenAI2GeminiWithState(chatResponse, info, state)
	return converted, canonicalUsageFromResponse(chatResponse), state.Err
}

func newOAIChatToGeminiStreamState(_ ResponseStreamOptions) any {
	return &convmeta.GeminiConvertInfo{}
}

func convertOAIChatStreamChunkToGeminiChat(_ context.Context, info convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	chatResponse, err := asOAIChatStreamResponse(response)
	if err != nil {
		return nil, nil, err
	}
	streamState, ok := state.(*convmeta.GeminiConvertInfo)
	if !ok || streamState == nil {
		return nil, nil, errors.New("OpenAI chat to Gemini stream state is required")
	}
	converted := oaichat.StreamResponseOpenAI2GeminiWithState(chatResponse, info, streamState)
	return streamValuesFromAny(converted), canonicalUsageFromResponse(chatResponse), streamState.Err
}

func finalizeOAIChatStreamResponseToGeminiChat(_ context.Context, _ convmeta.Meta, state any) ([]any, *dto.Usage, error) {
	streamState, ok := state.(*convmeta.GeminiConvertInfo)
	if !ok || streamState == nil {
		return nil, nil, errors.New("OpenAI chat to Gemini stream state is required")
	}
	if streamState.Err != nil {
		return nil, nil, streamState.Err
	}
	for key, tool := range streamState.Tools {
		if !tool.Emitted {
			return nil, nil, fmt.Errorf("incomplete streamed tool call at choice %d index %d", key[0], key[1])
		}
	}
	return nil, nil, nil
}

func convertClaudeMessagesResponseToOAIChat(_ context.Context, _ convmeta.Meta, response any) (any, *dto.Usage, error) {
	claudeResponse, err := asClaudeResponse(response)
	if err != nil {
		return nil, nil, err
	}
	usage := usageFromClaudeResponse(claudeResponse)
	openAIResponse := ResponseClaude2OpenAI(claudeResponse)
	if usage != nil {
		openAIResponse.Usage = *usage
	}
	return openAIResponse, usage, nil
}

func convertClaudeMessagesStreamResponseToOAIChat(_ context.Context, _ convmeta.Meta, response any) (any, *dto.Usage, error) {
	claudeResponse, err := asClaudeResponse(response)
	if err != nil {
		return nil, nil, err
	}
	if (claudeResponse.ContentBlock != nil && claudeResponse.ContentBlock.Type == "tool_use") ||
		(claudeResponse.Delta != nil && claudeResponse.Delta.Type == "input_json_delta") {
		return nil, nil, errors.New("Claude tool stream conversion requires ConvertStreamResponseChunk with per-stream state")
	}
	openAIResponse := StreamResponseClaude2OpenAI(claudeResponse)
	usage := usageFromClaudeResponse(claudeResponse)
	if openAIResponse != nil && usage != nil {
		openAIResponse.Usage = usage
	}
	return openAIResponse, usage, nil
}

func newClaudeMessagesToOAIChatStreamState(_ ResponseStreamOptions) any {
	return &ClaudeToChatStreamState{}
}

func convertClaudeMessagesStreamChunkToOAIChat(_ context.Context, _ convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	claudeResponse, err := asClaudeResponse(response)
	if err != nil {
		return nil, nil, err
	}
	streamState, ok := state.(*ClaudeToChatStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("Claude-to-Chat stream state is required")
	}
	converted, err := streamState.ConvertChunk(claudeResponse)
	if err != nil {
		return nil, nil, err
	}
	usage := usageFromClaudeResponse(claudeResponse)
	if converted != nil && usage != nil {
		converted.Usage = usage
	}
	return streamValuesFromAny(converted), usage, nil
}

func convertGeminiChatResponseToOAIChat(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	geminiResponse, err := asGeminiChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	usage := UsageFromGeminiMetadata(geminiResponse.GetUsageMetadata(), fallbackPromptTokens(info))
	openAIResponse := ResponseGeminiChat2OpenAI(fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID()), kitutil.GetTimestamp(), geminiResponse)
	for i, candidate := range geminiResponse.Candidates {
		annotations, err := geminichat.GroundingAnnotationsToChat(candidate.GroundingMetadata, candidate.Content, openAIResponse.Choices[i].Message.StringContent())
		if err != nil {
			return nil, nil, err
		}
		openAIResponse.Choices[i].Message.Annotations = annotations
	}
	if info != nil && info.HasChannelMeta() {
		openAIResponse.Model = info.GetUpstreamModelName()
	}
	if usage != nil {
		openAIResponse.Usage = *usage
	}
	return openAIResponse, usage, nil
}

func newGeminiChatToOAIChatStreamState(options ResponseStreamOptions) any {
	return geminichat.NewGeminiToChatStreamState(options.ID, options.Created)
}

func convertGeminiChatStreamResponseChunkToOAIChat(_ context.Context, info convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	geminiResponse, err := asGeminiChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	streamState, ok := state.(*geminichat.GeminiToChatStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("Gemini chat to OAI chat stream state is required")
	}
	usage := UsageFromGeminiMetadata(geminiResponse.GetUsageMetadata(), fallbackPromptTokens(info))
	model := ""
	if info != nil && info.HasChannelMeta() {
		model = info.GetUpstreamModelName()
	}
	responses := streamState.ConvertChunk(geminiResponse, model, usage)
	return streamValuesFromAny(responses), usage, streamState.Error()
}

func finalizeGeminiChatStreamResponseToOAIChat(_ context.Context, info convmeta.Meta, state any) ([]any, *dto.Usage, error) {
	streamState, ok := state.(*geminichat.GeminiToChatStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("Gemini chat to OAI chat stream state is required")
	}
	model := ""
	if info != nil && info.HasChannelMeta() {
		model = info.GetUpstreamModelName()
	}
	responses := streamState.Finalize(model)
	return streamValuesFromAny(responses), streamState.Usage(), nil
}

func convertGeminiChatStreamResponseToOAIChat(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	geminiResponse, err := asGeminiChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	openAIResponse, _ := StreamResponseGeminiChat2OpenAI(geminiResponse)
	usage := UsageFromGeminiMetadata(geminiResponse.GetUsageMetadata(), fallbackPromptTokens(info))
	if openAIResponse != nil {
		openAIResponse.Id = fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID())
		openAIResponse.Created = kitutil.GetTimestamp()
		if info != nil && info.HasChannelMeta() {
			openAIResponse.Model = info.GetUpstreamModelName()
		}
		openAIResponse.Usage = usage
	}
	return openAIResponse, usage, nil
}

func fallbackPromptTokens(info convmeta.Meta) int {
	if info == nil {
		return 0
	}
	return info.GetEstimatePromptTokens()
}

func asOAIChatResponse(response any) (*dto.OpenAITextResponse, error) {
	switch resp := response.(type) {
	case *dto.OpenAITextResponse:
		return resp, nil
	case dto.OpenAITextResponse:
		return &resp, nil
	default:
		return nil, fmt.Errorf("expected OAI chat response, got %T", response)
	}
}

func asOAIChatStreamResponse(response any) (*dto.ChatCompletionsStreamResponse, error) {
	switch resp := response.(type) {
	case *dto.ChatCompletionsStreamResponse:
		return resp, nil
	case dto.ChatCompletionsStreamResponse:
		return &resp, nil
	default:
		return nil, fmt.Errorf("expected OAI chat stream response, got %T", response)
	}
}

func asOAIResponsesResponse(response any) (*dto.OpenAIResponsesResponse, error) {
	switch resp := response.(type) {
	case *dto.OpenAIResponsesResponse:
		return resp, nil
	case dto.OpenAIResponsesResponse:
		return &resp, nil
	default:
		return nil, fmt.Errorf("expected OAI responses response, got %T", response)
	}
}

func asOAIResponsesStreamResponse(response any) (*dto.ResponsesStreamResponse, error) {
	switch resp := response.(type) {
	case *dto.ResponsesStreamResponse:
		return resp, nil
	case dto.ResponsesStreamResponse:
		return &resp, nil
	default:
		return nil, fmt.Errorf("expected OAI responses stream response, got %T", response)
	}
}

func asClaudeResponse(response any) (*dto.ClaudeResponse, error) {
	switch resp := response.(type) {
	case *dto.ClaudeResponse:
		return resp, nil
	case dto.ClaudeResponse:
		return &resp, nil
	default:
		return nil, fmt.Errorf("expected Claude messages response, got %T", response)
	}
}

func asGeminiChatResponse(response any) (*dto.GeminiChatResponse, error) {
	switch resp := response.(type) {
	case *dto.GeminiChatResponse:
		return resp, nil
	case dto.GeminiChatResponse:
		return &resp, nil
	default:
		return nil, fmt.Errorf("expected Gemini chat response, got %T", response)
	}
}
