package generation

func PhasesFor(format string) []string {
	switch format {
	case "t20":
		return []string{"powerplay", "middle", "death"}
	case "odi":
		return []string{"powerplay", "middle", "death"}
	default:
		return nil
	}
}
