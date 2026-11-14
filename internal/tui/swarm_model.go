package tui

import (
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/swarmboard"
)

// SwarmModel owns the swarm board overlay and the active swarm snapshot.
type SwarmModel struct {
	Board     *swarmboard.Board
	item      *activeitemspanel.SwarmItem
	dismissed bool
	fromBoard bool
}
