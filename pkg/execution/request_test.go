package execution

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCandidateFailuresDoNotConsumeSharedCallBudget(t *testing.T) {
	r := New("request-one", Generation)
	for index := 0; index < 8; index++ {
		_, err := r.CheckCandidate(Candidate{ChannelID: index, Stage: "safe"})
		require.NoError(t, err)
	}
	require.Zero(t, r.CallCount())
	for index, stage := range []string{"compact_exact", "compact_base", "compatibility"} {
		candidate, err := r.CheckCandidate(Candidate{ChannelID: 12, Stage: stage})
		require.NoError(t, err)
		call, err := r.BeginCall(context.Background(), candidate.ID)
		require.NoError(t, err)
		require.Equal(t, index+1, call.Sequence)
		require.Equal(t, fmt.Sprintf("request-one:call:%d", index+1), call.ID)
	}
	candidate, err := r.CheckCandidate(Candidate{ChannelID: 13})
	require.NoError(t, err)
	_, err = r.BeginCall(context.Background(), candidate.ID)
	require.ErrorIs(t, err, ErrCallBudget)
}

func TestConcurrentCallsCannotExceedRequestBudget(t *testing.T) {
	r := New("concurrent", Submission)
	candidate, err := r.CheckCandidate(Candidate{ChannelID: 1})
	require.NoError(t, err)
	results := make(chan error, 4)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index := 0; index < 4; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := r.BeginCall(context.Background(), candidate.ID)
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	rejected := 0
	for err := range results {
		if err != nil {
			require.ErrorIs(t, err, ErrCallBudget)
			rejected++
		}
	}
	require.Equal(t, 1, rejected)
	require.Equal(t, 3, r.CallCount())
}

func TestCanceledAndCommittedRequestsCannotStartMoreCalls(t *testing.T) {
	r := New("cancel", Generation)
	candidate, err := r.CheckCandidate(Candidate{ChannelID: 1})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.BeginCall(ctx, candidate.ID)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, r.CallCount())
	r.Commit()
	_, err = r.BeginCall(context.Background(), candidate.ID)
	require.ErrorIs(t, err, ErrCallBudget)
}
