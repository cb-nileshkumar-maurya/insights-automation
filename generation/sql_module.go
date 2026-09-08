package generation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// SQLModule is the production generation-module implementation. Its queue and
// result state live in SQL; MemoryRunStore remains only a deterministic test adapter.
type SQLModule struct {
	config Configuration
	store  *SQLRunStore
	clock  Clock
}

func NewSQLModule(config Configuration, store *SQLRunStore, clock Clock) *SQLModule {
	return &SQLModule{config: config, store: store, clock: clock}
}

func (m *SQLModule) StartRun(ctx context.Context, request StartRequest) (Run, error) {
	if request.Mode == "" {
		request.Mode = Normal
	}
	if request.Mode == Regeneration && request.Caller.Role != Editor && request.Caller.Role != Operations {
		return Run{}, ErrUnauthorized
	}
	if request.Target.Template != "" && request.Target.CardSet != "" {
		return Run{}, ErrInvalidRequest
	}
	if request.Target.CardSet != "" {
		return m.startCardSet(ctx, request)
	}
	template, ok := m.config.Templates[request.Target.Template]
	if !ok {
		return Run{}, ErrInvalidRequest
	}
	inputs, err := validate(template, request.Inputs)
	if err != nil {
		return Run{}, err
	}
	hash, err := InputHash(template.ID, template.Version, request.MatchID, request.CardState, inputs)
	if err != nil {
		return Run{}, err
	}
	if request.Mode == Normal {
		if result, err := m.store.LoadCurrentResult(ctx, template.ID, template.Version, request.MatchID, request.CardState, hash); err == nil && !m.isStale(template, result) {
			return Run{ID: "current-" + fmt.Sprint(result.Version), Target: request.Target, MatchID: request.MatchID, CardState: request.CardState, NormalizedInputs: inputs, State: Succeeded, ResultVersion: result.Version}, nil
		} else if err != nil && err != ErrNotFound {
			return Run{}, err
		}
	}
	run := newSQLRun(request, inputs, "", m.clock.Now())
	if err := m.store.SaveRun(ctx, run, template.Version, hash); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (m *SQLModule) startCardSet(ctx context.Context, request StartRequest) (Run, error) {
	templates, ok := m.config.CardSets[request.Target.CardSet]
	if !ok {
		return Run{}, ErrInvalidRequest
	}
	parent := newSQLRun(request, canonicalInputs(request.Inputs), "", m.clock.Now())
	if err := m.store.SaveRun(ctx, parent, "card-set-v1", ""); err != nil {
		return Run{}, err
	}
	for _, id := range templates {
		template := m.config.Templates[id]
		inputs, err := validate(template, inputsForTemplate(template, request.Inputs))
		if err != nil {
			return Run{}, err
		}
		hash, err := InputHash(id, template.Version, request.MatchID, request.CardState, inputs)
		if err != nil {
			return Run{}, err
		}
		if request.Mode == Normal {
			if result, err := m.store.LoadCurrentResult(ctx, id, template.Version, request.MatchID, request.CardState, hash); err == nil && !m.isStale(template, result) {
				continue
			} else if err != nil && err != ErrNotFound {
				return Run{}, err
			}
		}
		childRequest := request
		childRequest.Target = CardTarget{Template: id}
		child := newSQLRun(childRequest, inputs, parent.ID, m.clock.Now())
		if err := m.store.SaveRun(ctx, child, template.Version, hash); err != nil {
			return Run{}, err
		}
		parent.Children = append(parent.Children, child.ID)
	}
	if len(parent.Children) == 0 {
		parent.State = Succeeded
	}
	if err := m.store.SaveRun(ctx, parent, "card-set-v1", ""); err != nil {
		return Run{}, err
	}
	return parent, nil
}

func (m *SQLModule) GetRun(ctx context.Context, id string) (Run, error) {
	return m.store.LoadRun(ctx, id)
}
func (m *SQLModule) GetCurrentResult(ctx context.Context, templateID TemplateID, matchID string, cardState CardState, inputs map[string]any) (Result, error) {
	template, ok := m.config.Templates[templateID]
	if !ok {
		return Result{}, ErrInvalidRequest
	}
	normalized, err := validate(template, inputs)
	if err != nil {
		return Result{}, err
	}
	hash, err := InputHash(templateID, template.Version, matchID, cardState, normalized)
	if err != nil {
		return Result{}, err
	}
	result, err := m.store.LoadCurrentResult(ctx, templateID, template.Version, matchID, cardState, hash)
	if err != nil {
		return Result{}, err
	}
	if m.isStale(template, result) {
		return Result{}, ErrNotFound
	}
	return result, nil
}
func (m *SQLModule) isStale(template Template, result Result) bool {
	return template.FreshFor > 0 && m.clock.Now().After(result.Envelope.GeneratedAt.Add(time.Duration(template.FreshFor)*time.Minute))
}
func newSQLRun(request StartRequest, inputs map[string]any, parentID string, now time.Time) Run {
	return Run{ID: newRunID(), ParentID: parentID, Target: request.Target, MatchID: request.MatchID, CardState: request.CardState, NormalizedInputs: inputs, State: Queued, Mode: request.Mode, CreatedAt: now}
}
func newRunID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("cannot create generation run id")
	}
	return hex.EncodeToString(bytes)
}
