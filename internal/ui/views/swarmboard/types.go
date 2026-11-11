package swarmboard

import "time"

// Member represents a swarm member on the roster.
type Member struct {
	Name           string
	Role           string
	State          string // "waiting", "working", "stopped", "killed"
	Model          string
	ConversationID string
	IsCoordinator  bool
}

// Post represents a message posted on the swarm message board.
type Post struct {
	Seq       int
	Author    string
	Channel   string
	Body      string
	CreatedAt time.Time
	Mentions  []string
	IsDone    bool
}

// SwarmSnapshot represents the state of an active or completed swarm.
type SwarmSnapshot struct {
	Title     string
	Members   []Member
	Posts     []Post
	IsDone    bool
	Dismissed bool
}
