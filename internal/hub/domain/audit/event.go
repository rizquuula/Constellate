package audit

import "context"

// Action identifies the security-relevant operation recorded in the audit log.
type Action string

const (
	ActionLogin  Action = "login"
	ActionEnroll Action = "enroll"
	ActionAttach Action = "attach"
	ActionOpen   Action = "open"
	ActionClose  Action = "close"
	ActionRevoke Action = "revoke"
)

// Event is an immutable record of a single security-relevant action.
// Fields are unexported; use constructors and accessors.
type Event struct {
	ts        int64
	actor     string
	action    Action
	machineID string
	sessionID string
	detail    string
}

// NewEvent constructs an Event. detail is an optional JSON string (empty is allowed).
func NewEvent(ts int64, actor string, action Action, machineID, sessionID, detail string) Event {
	return Event{
		ts:        ts,
		actor:     actor,
		action:    action,
		machineID: machineID,
		sessionID: sessionID,
		detail:    detail,
	}
}

func (e Event) TS() int64        { return e.ts }
func (e Event) Actor() string    { return e.actor }
func (e Event) Action() Action   { return e.action }
func (e Event) MachineID() string { return e.machineID }
func (e Event) SessionID() string { return e.sessionID }
func (e Event) Detail() string   { return e.detail }

// ctxKey is an unexported type for context keys in this package.
type ctxKey struct{}

// ContextWithActor returns a context carrying actor.
func ContextWithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, ctxKey{}, actor)
}

// ActorFromContext returns the actor stored in ctx, or "operator" when unset.
func ActorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return "operator"
}
