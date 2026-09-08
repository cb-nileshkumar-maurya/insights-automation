package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// InputHash identifies one template result for a match, card state, and
// normalized filter set. It is stable across processes and map iteration order.
func InputHash(template TemplateID, templateVersion, matchID string, cardState CardState, inputs map[string]any) (string, error) {
	canonical := make([]filterValue, 0, len(inputs))
	for _, key := range sortedKeys(inputs) {
		value, err := json.Marshal(inputs[key])
		if err != nil {
			return "", fmt.Errorf("encode normalized filter %q: %w", key, err)
		}
		canonical = append(canonical, filterValue{Key: key, Value: value})
	}
	payload, err := json.Marshal(struct {
		Template  TemplateID    `json:"template"`
		Version   string        `json:"version"`
		MatchID   string        `json:"match_id"`
		CardState CardState     `json:"card_state"`
		Filters   []filterValue `json:"filters"`
	}{template, templateVersion, matchID, cardState, canonical})
	if err != nil {
		return "", fmt.Errorf("encode generation identity: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

type filterValue struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}
