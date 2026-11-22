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
	IsMonitor      bool
	Progress       int       // 0-100 task progress percentage (htop-style bar)
	ActiveTurns    int       // turns executed
	LastActivity   time.Time // timestamp of last activity
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

// MonitorStatus represents the health and stall monitor status for the swarm.
type MonitorStatus struct {
	Enabled        bool
	State          string // "healthy", "monitoring", "stalled", "recovering"
	LastHeartbeat  time.Time
	StalledWorkers []string
	AlertCount     int
	Message        string
}

// SwarmSnapshot represents the state of an active or completed swarm.
type SwarmSnapshot struct {
	Title     string
	Members   []Member
	Posts     []Post
	IsDone    bool
	Dismissed bool
	Monitor   *MonitorStatus
}
