package generation

import (
	"context"
	"fmt"
	"time"
)

const reconciliationInterval = 15 * time.Minute

type EligibleMatch struct {
	MatchID   string
	CardState CardState
	Inputs    map[string]any
}
type EligibleMatchSource interface {
	EligibleMatches(context.Context) ([]EligibleMatch, error)
}
type RunStarter interface {
	StartRun(context.Context, StartRequest) (Run, error)
}

// Reconciler submits one versioned card set per eligible match. The generation
// module decides whether that request joins work, reuses current results, or
// creates the missing and stale child runs.
type Reconciler struct {
	source  EligibleMatchSource
	starter RunStarter
	cardSet string
}

func NewReconciler(source EligibleMatchSource, starter RunStarter, cardSet string) *Reconciler {
	return &Reconciler{source: source, starter: starter, cardSet: cardSet}
}
func (r *Reconciler) Reconcile(ctx context.Context) ([]Run, error) {
	matches, err := r.source.EligibleMatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("list eligible matches: %w", err)
	}
	runs := make([]Run, 0, len(matches))
	for _, match := range matches {
		run, err := r.starter.StartRun(ctx, StartRequest{Target: CardTarget{CardSet: r.cardSet}, MatchID: match.MatchID, CardState: match.CardState, Inputs: canonicalInputs(match.Inputs), Caller: Caller{Role: Scheduler}, Mode: Normal})
		if err != nil {
			return runs, fmt.Errorf("start card set for match %s: %w", match.MatchID, err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}
func (r *Reconciler) Run(ctx context.Context) error {
	if _, err := r.Reconcile(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(reconciliationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := r.Reconcile(ctx); err != nil {
				return err
			}
		}
	}
}
