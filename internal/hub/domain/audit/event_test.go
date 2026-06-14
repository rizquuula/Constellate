package audit_test

import (
	"context"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

func TestNewEvent_Accessors(t *testing.T) {
	tests := []struct {
		name      string
		ts        int64
		actor     string
		action    audit.Action
		machineID string
		sessionID string
		detail    string
	}{
		{
			name:      "full event",
			ts:        1000,
			actor:     "operator",
			action:    audit.ActionAttach,
			machineID: "m1",
			sessionID: "s1",
			detail:    `{"key":"val"}`,
		},
		{
			name:   "empty optional fields",
			ts:     2000,
			actor:  "system",
			action: audit.ActionLogin,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := audit.NewEvent(tc.ts, tc.actor, tc.action, tc.machineID, tc.sessionID, tc.detail)

			if e.TS() != tc.ts {
				t.Errorf("TS: got %d, want %d", e.TS(), tc.ts)
			}
			if e.Actor() != tc.actor {
				t.Errorf("Actor: got %q, want %q", e.Actor(), tc.actor)
			}
			if e.Action() != tc.action {
				t.Errorf("Action: got %q, want %q", e.Action(), tc.action)
			}
			if e.MachineID() != tc.machineID {
				t.Errorf("MachineID: got %q, want %q", e.MachineID(), tc.machineID)
			}
			if e.SessionID() != tc.sessionID {
				t.Errorf("SessionID: got %q, want %q", e.SessionID(), tc.sessionID)
			}
			if e.Detail() != tc.detail {
				t.Errorf("Detail: got %q, want %q", e.Detail(), tc.detail)
			}
		})
	}
}

func TestActorFromContext_Default(t *testing.T) {
	got := audit.ActorFromContext(context.Background())
	if got != "operator" {
		t.Errorf("ActorFromContext (unset): got %q, want %q", got, "operator")
	}
}

func TestActorFromContext_RoundTrip(t *testing.T) {
	ctx := audit.ContextWithActor(context.Background(), "alice")
	got := audit.ActorFromContext(ctx)
	if got != "alice" {
		t.Errorf("ActorFromContext: got %q, want %q", got, "alice")
	}
}

func TestActorFromContext_OverridesDefault(t *testing.T) {
	ctx := audit.ContextWithActor(context.Background(), "bob")
	ctx = audit.ContextWithActor(ctx, "charlie")
	got := audit.ActorFromContext(ctx)
	if got != "charlie" {
		t.Errorf("ActorFromContext override: got %q, want %q", got, "charlie")
	}
}
