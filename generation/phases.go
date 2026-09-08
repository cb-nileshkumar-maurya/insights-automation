package generation

type MatchPhase struct {
	Name      string
	FirstOver int
	LastOver  int
}

func PhasesFor(format string) []MatchPhase {
	switch format {
	case "t20":
		return []MatchPhase{{Name: "powerplay", FirstOver: 1, LastOver: 6}, {Name: "middle", FirstOver: 7, LastOver: 15}, {Name: "death", FirstOver: 16, LastOver: 20}}
	case "odi":
		return []MatchPhase{{Name: "powerplay", FirstOver: 1, LastOver: 10}, {Name: "middle", FirstOver: 11, LastOver: 40}, {Name: "death", FirstOver: 41, LastOver: 50}}
	default:
		return nil
	}
}
