package generation

import (
	"context"
	"fmt"
)

// TemplateData dispatches a generation query to its configured card-template
// adapter without exposing query details to the command layer.
type TemplateData struct{ templates map[TemplateID]CricketData }

func NewTemplateData(templates map[TemplateID]CricketData) *TemplateData {
	return &TemplateData{templates: templates}
}

func (d *TemplateData) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	template, ok := d.templates[query.Template]
	if !ok {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: fmt.Sprintf("no configured generator for %s", query.Template)}}
	}
	return template.Generate(ctx, query)
}
