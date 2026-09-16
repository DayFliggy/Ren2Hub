package service

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/model"
)

var ErrGatewayRetryBudget = errors.New("gateway retry budget exhausted")

type RetryBudgetSnapshot struct {
	Enabled             bool   `json:"enabled"`
	AvailableHundredths int64  `json:"available_hundredths"`
	Reserved            int64  `json:"reserved"`
	FirstSends          uint64 `json:"first_sends_total"`
	RetrySends          uint64 `json:"retry_sends_total"`
	Rejected            uint64 `json:"rejected_total"`
}

type GatewayRetryBudget struct {
	mu          sync.Mutex
	version     int64
	initialized bool
	percent     int64
	capacity    int64
	state       RetryBudgetSnapshot
}

type RetryTicket struct {
	once     sync.Once
	budget   *GatewayRetryBudget
	reserved bool
}

var gatewayRetries GatewayRetryBudget

func (b *GatewayRetryBudget) Configure(config model.GatewayConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.initialized && config.Version < b.version {
		return
	}
	b.version, b.percent, b.capacity = config.Version, int64(config.RetryBudgetPercent), int64(config.RetryBudgetBurst)*100
	if !b.initialized {
		b.state.AvailableHundredths = b.capacity
		b.initialized = true
	}
	b.state.Enabled = config.RetryBudgetEnabled
	b.state.AvailableHundredths = min(b.state.AvailableHundredths, max(b.capacity-b.state.Reserved*100, 0))
}

func (b *GatewayRetryBudget) Reserve() (*RetryTicket, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := &RetryTicket{budget: b}
	if !b.state.Enabled {
		return t, nil
	}
	if b.state.AvailableHundredths < 100 {
		b.state.Rejected++
		return nil, ErrGatewayRetryBudget
	}
	b.state.AvailableHundredths -= 100
	b.state.Reserved++
	t.reserved = true
	return t, nil
}

func (t *RetryTicket) Finish(sent bool) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		b := t.budget
		b.mu.Lock()
		defer b.mu.Unlock()
		if sent {
			b.state.RetrySends++
		}
		if !t.reserved {
			return
		}
		b.state.Reserved--
		if !sent {
			b.state.AvailableHundredths = min(b.state.AvailableHundredths+100, max(b.capacity-b.state.Reserved*100, 0))
		}
	})
}

func (b *GatewayRetryBudget) FirstSend() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.FirstSends++
	if b.state.Enabled {
		b.state.AvailableHundredths = min(b.state.AvailableHundredths+b.percent, max(b.capacity-b.state.Reserved*100, 0))
	}
}

func (b *GatewayRetryBudget) Snapshot() RetryBudgetSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func ReserveGatewayRetry() (*RetryTicket, error) { return gatewayRetries.Reserve() }
func RecordGatewayFirstSend()                    { gatewayRetries.FirstSend() }
