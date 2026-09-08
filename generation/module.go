package generation

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

type Module struct {
	config Configuration
	data   CricketData
	store  *MemoryRunStore
	clock  Clock
}

func NewModule(config Configuration, data CricketData, store *MemoryRunStore, clock Clock) *Module {
	return &Module{config: config, data: data, store: store, clock: clock}
}

func (m *Module) StartRun(_ context.Context, request StartRequest) (Run, error) {
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
		return m.startCardSet(request)
	}
	template, ok := m.config.Templates[request.Target.Template]
	if !ok {
		return Run{}, ErrInvalidRequest
	}
	inputs, err := validate(template, request.Inputs)
	if err != nil {
		return Run{}, err
	}
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	if request.Mode == Normal {
		if current := m.currentLocked(request.Target.Template, request.MatchID, request.CardState, inputs); current != nil {
			return Run{ID: "current-" + fmt.Sprint(current.Version), Target: request.Target, State: Succeeded, NormalizedInputs: inputs, ResultVersion: current.Version}, nil
		}
		for _, existing := range m.store.runs {
			if existing.Target == request.Target && existing.MatchID == request.MatchID && existing.CardState == request.CardState && existing.Mode == Normal && (existing.State == Queued || existing.State == Running) && reflect.DeepEqual(existing.NormalizedInputs, inputs) {
				return *existing, nil
			}
		}
	}
	return *m.newRunLocked(request, inputs, ""), nil
}

func (m *Module) startCardSet(request StartRequest) (Run, error) {
	templates, ok := m.config.CardSets[request.Target.CardSet]
	if !ok {
		return Run{}, ErrInvalidRequest
	}
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	parent := m.newRunLocked(request, canonicalInputs(request.Inputs), "")
	for _, id := range templates {
		inputs, err := validate(m.config.Templates[id], inputsForTemplate(m.config.Templates[id], request.Inputs))
		if err != nil {
			return Run{}, err
		}
		childRequest := request
		childRequest.Target = CardTarget{Template: id}
		child := m.newRunLocked(childRequest, inputs, parent.ID)
		parent.Children = append(parent.Children, child.ID)
	}
	return *parent, nil
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
func (m *Module) newRunLocked(request StartRequest, inputs map[string]any, parent string) *Run {
	m.store.sequence++
	run := &Run{ID: fmt.Sprintf("run-%d", m.store.sequence), ParentID: parent, Target: request.Target, MatchID: request.MatchID, CardState: request.CardState, NormalizedInputs: inputs, State: Queued, Mode: request.Mode, CreatedAt: m.clock.Now()}
	m.store.runs[run.ID] = run
	return run
}
func (m *Module) runAll(ctx context.Context) {
	for m.runNext(ctx) {
	}
}

// RecoverExpiredLeases returns abandoned child work to the durable queue.
func (m *Module) recoverExpiredLeases() int {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	recovered := 0
	for _, run := range m.store.runs {
		if run.Target.Template != "" && run.State == Running && m.clock.Now().After(run.LeaseUntil) {
			run.State = Queued
			run.LeaseUntil = time.Time{}
			recovered++
		}
	}
	return recovered
}
func (m *Module) runNext(ctx context.Context) bool {
	m.store.mu.Lock()
	var next *Run
	for _, run := range m.store.runs {
		if run.Target.Template != "" && run.State == Queued && (next == nil || manualFirst(run, next)) {
			next = run
		}
	}
	if next == nil {
		m.store.mu.Unlock()
		return false
	}
	next.State = Running
	next.LeaseUntil = m.clock.Now().Add(60 * time.Second)
	m.store.mu.Unlock()
	generated := m.data.Generate(ctx, GenerationQuery{Template: next.Target.Template, MatchID: next.MatchID, Inputs: canonicalInputs(next.NormalizedInputs)})
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	if generated.Err != nil {
		next.Failure = generated.Err
		if generated.Err.Kind == TransientFailure && next.Attempt < 2 {
			next.Attempt++
			next.State = Queued
		} else {
			next.State = Failed
		}
		m.refreshParentLocked(next.ParentID)
		return true
	}
	template := m.config.Templates[next.Target.Template]
	key := resultKey(next.Target.Template, next.MatchID, next.CardState, next.NormalizedInputs)
	version := len(m.store.results[key]) + 1
	result := Result{Version: version, Envelope: ResultEnvelope{Template: next.Target.Template, TemplateVersion: template.Version, NormalizedFilters: canonicalInputs(next.NormalizedInputs), SourceDataWindow: generated.SourceDataWindow, SampleSize: generated.SampleSize, GeneratedAt: m.clock.Now(), ResultVersion: version, Fallbacks: generated.Fallbacks}, Data: generated.Data}
	m.store.results[key] = append(m.store.results[key], result)
	next.ResultVersion = version
	next.State = Succeeded
	m.refreshParentLocked(next.ParentID)
	return true
}
func manualFirst(left, right *Run) bool {
	return left.Mode == Regeneration && right.Mode != Regeneration
}
func (m *Module) refreshParentLocked(parentID string) {
	if parentID == "" {
		return
	}
	parent := m.store.runs[parentID]
	failed, done := 0, 0
	for _, childID := range parent.Children {
		child := m.store.runs[childID]
		if child.State == Failed {
			failed++
			done++
		}
		if child.State == Succeeded {
			done++
		}
	}
	if done != len(parent.Children) {
		return
	}
	if failed == 0 {
		parent.State = Succeeded
	} else if failed == len(parent.Children) {
		parent.State = Failed
	} else {
		parent.State = CompletedWithErrors
	}
}
func (m *Module) GetRun(_ context.Context, id string) (Run, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	run, ok := m.store.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return *run, nil
}
func (m *Module) GetCurrentResult(_ context.Context, template TemplateID, matchID string, cardState CardState, inputs map[string]any) (Result, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	result := m.currentLocked(template, matchID, cardState, inputs)
	if result == nil {
		return Result{}, ErrNotFound
	}
	return *result, nil
}
func (m *Module) currentLocked(template TemplateID, matchID string, cardState CardState, inputs map[string]any) *Result {
	results := m.store.results[resultKey(template, matchID, cardState, inputs)]
	if len(results) == 0 {
		return nil
	}
	result := results[len(results)-1]
	freshness := time.Duration(m.config.Templates[template].FreshFor) * time.Minute
	if freshness > 0 && m.clock.Now().After(result.Envelope.GeneratedAt.Add(freshness)) {
		return nil
	}
	return &result
}
func resultKey(template TemplateID, matchID string, cardState CardState, inputs map[string]any) string {
	key := string(template) + "|match=" + matchID + "|state=" + string(cardState)
	for _, name := range sortedKeys(inputs) {
		key += fmt.Sprintf("|%s=%#v", name, inputs[name])
	}
	return key
}
func validate(template Template, inputs map[string]any) (map[string]any, error) {
	normalized := canonicalInputs(template.Defaults)
	for key, value := range inputs {
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
