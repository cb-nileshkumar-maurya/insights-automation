package generation

import "fmt"

// playerPopulation keeps the normalized player selection and its match-scoped
// identities together while preserving the persisted input representation.
type playerPopulation struct {
	ids        []int
	identities map[int]MatchPlayer
}

func playerPopulationFrom(inputs map[string]any) (playerPopulation, error) {
	ids, err := playerIDs(inputs["players"])
	if err != nil {
		return playerPopulation{}, err
	}
	identities, err := playerContext(inputs["player_context"])
	if err != nil {
		return playerPopulation{}, err
	}
	return playerPopulation{ids: ids, identities: identities}, nil
}

func playerContext(raw any) (map[int]MatchPlayer, error) {
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("player_context is required")
	}
	players := make(map[int]MatchPlayer, len(values))
	for id, rawPlayer := range values {
		playerID, err := inputID(map[string]any{"player": id}, "player")
		if err != nil {
			return nil, err
		}
		value, ok := rawPlayer.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("player_context must contain player details")
		}
		teamID, err := inputID(value, "team_id")
		if err != nil {
			return nil, err
		}
		name, ok := value["player_name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("player_name is required")
		}
		players[playerID] = MatchPlayer{ID: playerID, TeamID: teamID, FullName: name}
	}
	return players, nil
}

func playerIDs(raw any) ([]int, error) {
	values, ok := raw.([]string)
	if !ok || len(values) == 0 {
		return nil, fmt.Errorf("players must be a non-empty stable ID list")
	}
	ids := make([]int, 0, len(values))
	for _, value := range values {
		id, err := inputID(map[string]any{"player": value}, "player")
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
