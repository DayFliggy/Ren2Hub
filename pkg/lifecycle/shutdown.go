package lifecycle

import (
	"context"
	"errors"
	"fmt"
)

type Stopper struct {
	Name string
	Stop func(context.Context) error
}

// StopAll starts every stop operation and bounds the wait even when a worker's
// implementation does not honor its context. An incomplete drain is an error.
func StopAll(ctx context.Context, stoppers ...Stopper) error {
	results := make(chan error, len(stoppers))
	for _, stopper := range stoppers {
		go func(stopper Stopper) {
			err := stopper.Stop(ctx)
			if err != nil {
				err = fmt.Errorf("%s: %w", stopper.Name, err)
			}
			results <- err
		}(stopper)
	}
	var result error
	for range stoppers {
		select {
		case err := <-results:
			result = errors.Join(result, err)
		case <-ctx.Done():
			return errors.Join(result, fmt.Errorf("shutdown wait incomplete: %w", ctx.Err()))
		}
	}
	return result
}
