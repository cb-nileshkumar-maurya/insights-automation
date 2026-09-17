package generation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// SQLModule coordinates idempotent submissions and locator lookups. SQL keeps
// queue and result state durable across service restarts.
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
	result, resultErr := m.store.LoadCurrentResult(ctx, template.ID, template.Version, request.MatchID, request.CardState, hash)
	if resultErr != nil && resultErr != ErrNotFound {
		return Run{}, resultErr
	}
	if request.Mode == Normal {
		if resultErr == nil && !m.isStale(template, result) {
			return Run{ID: "current-" + fmt.Sprint(result.Version), Target: request.Target, MatchID: request.MatchID, CardState: request.CardState, NormalizedInputs: inputs, State: Succeeded, ResultVersion: result.Version}, nil
		}
	}
	if active, err := m.store.FindActiveRun(ctx, request.Target, request.MatchID, request.CardState, hash, request.Mode); err == nil {
		return active, nil
	} else if err != ErrNotFound {
		return Run{}, err
	}
	run := newSQLRun(request, inputs, "", m.clock.Now())
	if resultErr == nil {
		run.ResultVersion = result.Version
	}
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
	parentInputs := canonicalInputs(request.Inputs)
	parentHash, err := InputHash(TemplateID(request.Target.CardSet), "card-set-v1", request.MatchID, request.CardState, parentInputs)
	if err != nil {
		return Run{}, err
	}
	if active, err := m.store.FindActiveRun(ctx, request.Target, request.MatchID, request.CardState, parentHash, request.Mode); err == nil {
		return active, nil
	} else if err != ErrNotFound {
		return Run{}, err
	}
	type childPreparation struct {
		template      Template
		inputs        map[string]any
		hash          string
		resultVersion int
	}
	pending := make([]childPreparation, 0, len(templates))
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
		result, resultErr := m.store.LoadCurrentResult(ctx, id, template.Version, request.MatchID, request.CardState, hash)
		if resultErr != nil && resultErr != ErrNotFound {
			return Run{}, resultErr
		}
		if request.Mode == Normal {
			if resultErr == nil && !m.isStale(template, result) {
				continue
			}
		}
		version := 0
		if resultErr == nil {
			version = result.Version
		}
		pending = append(pending, childPreparation{template: template, inputs: inputs, hash: hash, resultVersion: version})
	}
	if len(pending) == 0 {
		return Run{ID: "current-card-set", Target: request.Target, MatchID: request.MatchID, CardState: request.CardState, NormalizedInputs: parentInputs, State: Succeeded}, nil
	}
	parent := newSQLRun(request, parentInputs, "", m.clock.Now())
	if err := m.store.SaveRun(ctx, parent, "card-set-v1", parentHash); err != nil {
		return Run{}, err
	}
	for _, prepared := range pending {
		childRequest := request
		childRequest.Target = CardTarget{Template: prepared.template.ID}
		child := newSQLRun(childRequest, prepared.inputs, parent.ID, m.clock.Now())
		child.ResultVersion = prepared.resultVersion
		if err := m.store.SaveRun(ctx, child, prepared.template.Version, prepared.hash); err != nil {
			return Run{}, err
		}
		parent.Children = append(parent.Children, child.ID)
	}
	if len(parent.Children) == 0 {
		parent.State = Succeeded
	}
	if err := m.store.SaveRun(ctx, parent, "card-set-v1", parentHash); err != nil {
		return Run{}, err
	}
	return parent, nil
}

func (m *SQLModule) GetRun(ctx context.Context, id string) (Run, error) {
	return m.store.LoadRun(ctx, id)
}

func (m *SQLModule) ResultLocators(run Run) (map[TemplateID]string, error) {
	if run.Target.Template != "" {
		locator, err := m.resultLocator(run.Target.Template, run.MatchID, run.CardState, run.NormalizedInputs)
		if err != nil {
			return nil, err
		}
		return map[TemplateID]string{run.Target.Template: locator}, nil
	}
	templates, ok := m.config.CardSets[run.Target.CardSet]
	if !ok {
		return nil, ErrInvalidRequest
	}
	locators := make(map[TemplateID]string, len(templates))
	for _, templateID := range templates {
		inputs, err := validate(m.config.Templates[templateID], inputsForTemplate(m.config.Templates[templateID], run.NormalizedInputs))
		if err != nil {
			return nil, err
		}
		locator, err := m.resultLocator(templateID, run.MatchID, run.CardState, inputs)
		if err != nil {
			return nil, err
		}
		locators[templateID] = locator
	}
	return locators, nil
}

func (m *SQLModule) GetResult(ctx context.Context, locator string) (LocatedResult, error) {
	if result, err := m.store.LoadCurrentResultByLocator(ctx, locator); err == nil {
		located := LocatedResult{Result: &result}
		located.ResultStatus.Status = Succeeded
		return located, nil
	} else if !errors.Is(err, ErrNotFound) {
		return LocatedResult{}, err
	}
	run, err := m.store.LoadLatestRunByLocator(ctx, locator)
	if err != nil {
		return LocatedResult{}, err
	}
	located := LocatedResult{Failure: run.Failure}
	located.ResultStatus.Status = run.State
	return located, nil
}

func (m *SQLModule) resultLocator(templateID TemplateID, matchID string, cardState CardState, inputs map[string]any) (string, error) {
	template, ok := m.config.Templates[templateID]
	if !ok {
		return "", ErrInvalidRequest
	}
	return InputHash(templateID, template.Version, matchID, cardState, inputs)
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
	bytes := make([]byte, 5)
	if _, err := rand.Read(bytes); err != nil {
		panic("cannot create generation run id")
	}
	return hex.EncodeToString(bytes)
}

func inputsForTemplate(template Template, inputs map[string]any) map[string]any {
	filtered := make(map[string]any)
	for key, value := range inputs {
		if _, allowed := template.Allowed[key]; allowed {
			filtered[key] = value
		}
	}
	return filtered
}

func validate(template Template, inputs map[string]any) (map[string]any, error) {
	normalized := canonicalInputs(template.Defaults)
	for key, value := range inputs {
		value, err := normalizeInputJSON(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be a valid integer", ErrInvalidRequest, key)
		}
		allowed, known := template.Allowed[key]
		if !known {
			return nil, fmt.Errorf("%w: %s is not allowed", ErrInvalidRequest, key)
		}
		if len(allowed) > 0 {
			valid := false
			for _, choice := range allowed {
				if reflect.DeepEqual(choice, value) {
					valid = true
				}
			}
			if !valid {
				return nil, fmt.Errorf("%w: %s is not approved", ErrInvalidRequest, key)
			}
		}
		normalized[key] = value
	}
	for _, required := range template.Required {
		if _, ok := normalized[required]; !ok {
			return nil, fmt.Errorf("%w: %s is required", ErrInvalidRequest, required)
		}
	}
	return normalized, nil
}
