package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const MaxCalls = 3
const MaxCandidates = 64

var ErrCallBudget = errors.New("upstream application call budget exhausted")
var ErrCandidateBudget = errors.New("candidate inspection budget exhausted")
var ErrRequestEnded = errors.New("request execution already ended")

type Class string

const (
	Generation Class = "generation"
	Submission Class = "submission"
	TaskQuery  Class = "task_query"
)

type Candidate struct {
	ID        string `json:"candidate_id"`
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
	Protocol  string `json:"protocol"`
	Stage     string `json:"stage"`
}

type Call struct {
	ID             string    `json:"call_id"`
	CandidateID    string    `json:"candidate_id"`
	Sequence       int       `json:"sequence"`
	StartedAt      time.Time `json:"started_at"`
	HeadersAt      time.Time `json:"headers_at,omitempty"`
	StatusCode     int       `json:"status_code,omitempty"`
	TransportError bool      `json:"transport_error,omitempty"`
}

type Snapshot struct {
	RequestID  string      `json:"request_id"`
	Class      Class       `json:"class"`
	CallLimit  int         `json:"call_limit"`
	Candidates []Candidate `json:"candidates"`
	Calls      []Call      `json:"calls"`
	Committed  bool        `json:"committed"`
	Ended      bool        `json:"ended"`
}

type Request struct {
	mu    sync.Mutex
	state Snapshot
}

func New(requestID string, class Class) *Request {
	return &Request{state: Snapshot{RequestID: requestID, Class: class, CallLimit: MaxCalls}}
}

// TightenCallLimit cannot restore budget spent by an earlier semantic stage.
func (r *Request) TightenCallLimit(limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit >= 0 && limit < r.state.CallLimit {
		r.state.CallLimit = limit
	}
}

func (r *Request) CheckCandidate(candidate Candidate) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Ended {
		return Candidate{}, ErrRequestEnded
	}
	for _, previous := range r.state.Candidates {
		if previous.ChannelID == candidate.ChannelID && previous.Model == candidate.Model && previous.Protocol == candidate.Protocol && previous.Stage == candidate.Stage {
			return previous, nil
		}
	}
	if len(r.state.Candidates) >= MaxCandidates {
		return Candidate{}, ErrCandidateBudget
	}
	candidate.ID = fmt.Sprintf("%s:candidate:%d", r.state.RequestID, len(r.state.Candidates)+1)
	r.state.Candidates = append(r.state.Candidates, candidate)
	return candidate, nil
}

// BeginCall is invoked only after conversion, request construction and local
// admission succeed. A net/http transparent replay remains part of this call.
func (r *Request) BeginCall(ctx context.Context, candidateID string) (Call, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Call{}, err
	}
	if r.state.Ended {
		return Call{}, ErrRequestEnded
	}
	if r.state.Committed || len(r.state.Calls) >= r.state.CallLimit {
		return Call{}, ErrCallBudget
	}
	known := false
	for _, candidate := range r.state.Candidates {
		if candidate.ID == candidateID {
			known = true
			break
		}
	}
	if !known {
		return Call{}, errors.New("upstream call references unknown candidate")
	}
	sequence := len(r.state.Calls) + 1
	call := Call{ID: fmt.Sprintf("%s:call:%d", r.state.RequestID, sequence), CandidateID: candidateID, Sequence: sequence, StartedAt: time.Now()}
	r.state.Calls = append(r.state.Calls, call)
	return call, nil
}

func (r *Request) ObserveHeaders(callID string, statusCode int, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.state.Calls {
		if r.state.Calls[i].ID == callID && r.state.Calls[i].HeadersAt.IsZero() {
			r.state.Calls[i].HeadersAt = time.Now()
			r.state.Calls[i].StatusCode = statusCode
			r.state.Calls[i].TransportError = failed
			return
		}
	}
}

func (r *Request) Commit() { r.mu.Lock(); r.state.Committed = true; r.mu.Unlock() }
func (r *Request) End()    { r.mu.Lock(); r.state.Ended = true; r.mu.Unlock() }

func (r *Request) CallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.state.Calls)
}

func (r *Request) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.state
	state.Candidates = append([]Candidate(nil), state.Candidates...)
	state.Calls = append([]Call(nil), state.Calls...)
	return state
}
