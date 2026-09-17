package generation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrMatchNotFound         = errors.New("match not found")
	ErrUnsupportedMatchType  = errors.New("match format is unsupported")
	ErrHistoricalUnavailable = errors.New("historical source is unavailable")
)

type MatchPlayer struct {
	ID       int
	TeamID   int
	FullName string
}

// MatchContext is the server-derived input shared by card templates. Keeping
// it separate from request inputs prevents clients from changing match facts.
type MatchContext struct {
	TeamA, TeamB int
	Format       string
	Venue        int
	Players      []MatchPlayer
}

// MatchContextResolver reads authoritative match facts and its recorded squad.
// Player templates use the squad to validate an optional player selection.
type MatchContextResolver interface {
	ResolveMatchContext(context.Context, string) (MatchContext, error)
}

type MariaDBMatchContextResolver struct{ db *sql.DB }

func NewMariaDBMatchContextResolver(db *sql.DB) *MariaDBMatchContextResolver {
	return &MariaDBMatchContextResolver{db: db}
}

func (resolver *MariaDBMatchContextResolver) ResolveMatchContext(ctx context.Context, matchID string) (MatchContext, error) {
	var context MatchContext
	var matchType int
	err := resolver.db.QueryRowContext(ctx, `SELECT match_record.teama, match_record.teamb, match_record.match_type_id, COALESCE(venue.id, 0) FROM krik_match_archive match_record LEFT JOIN krik_match_venue venue ON venue.id = match_record.venueid WHERE match_record.id = ?`, matchID).Scan(&context.TeamA, &context.TeamB, &matchType, &context.Venue)
	if err == sql.ErrNoRows {
		return MatchContext{}, fmt.Errorf("%w for match_id %s", ErrMatchNotFound, matchID)
	}
	if err != nil {
		return MatchContext{}, fmt.Errorf("load match context: %w", err)
	}
	switch matchType {
	case 2:
		context.Format = "odi"
	case 3:
		context.Format = "t20"
	default:
		return MatchContext{}, fmt.Errorf("%w for match_id %s", ErrUnsupportedMatchType, matchID)
	}
	rows, err := resolver.db.QueryContext(ctx, `SELECT squad.teamId, squad.playerId, player.fullName FROM stats_import3_dump_matchplayers_tbl squad JOIN stats_import_player_master player ON player.id = squad.playerId WHERE squad.matchId = ? ORDER BY squad.playerId, squad.teamId`, matchID)
	if err != nil {
		return MatchContext{}, fmt.Errorf("load match squad: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var player MatchPlayer
		if err := rows.Scan(&player.TeamID, &player.ID, &player.FullName); err != nil {
			return MatchContext{}, fmt.Errorf("scan match squad: %w", err)
		}
		context.Players = append(context.Players, player)
	}
	if err := rows.Err(); err != nil {
		return MatchContext{}, fmt.Errorf("iterate match squad: %w", err)
	}
	return context, nil
}
