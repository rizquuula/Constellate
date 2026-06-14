package machine

// Status represents the liveness of a machine's agent connection.
type Status string

const (
	StatusOnline  Status = "online"
	StatusOffline Status = "offline"
)
