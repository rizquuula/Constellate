package dashboard

import (
	"context"
	"log/slog"
	"sort"

	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// recentAuditLimit is the number of audit events included in every View.
const recentAuditLimit = 20

// Totals holds fleet-wide aggregate counts.
type Totals struct {
	MachinesOnline           int
	MachinesTotal            int
	SessionsRunning          int
	SessionsExited           int
	SessionsLost             int
	SessionsDisconnected     int
	SessionsTotal            int
	ProjectsTotal            int
	SessionsActive           int
	SessionsIdle             int
	SessionsAwaitingInput    int
}

// MachineRollup holds per-machine status + session counts.
type MachineRollup struct {
	ID         string
	Name       string
	OS         string
	Online     bool
	Revoked    bool
	LastSeenAt int64
	Running    int // sessions on this machine with status running
	Total      int // all sessions on this machine regardless of status
}

// ProjectRollup holds per-project session status counts.
// A synthetic entry with ID="" and Name="Ungrouped" aggregates all
// project-less sessions across all machines. This mirrors the "Ungrouped"
// bucket used by the frontend sidebar.
type ProjectRollup struct {
	ID           string
	Name         string
	MachineID    string
	Running      int
	Exited       int
	Lost         int
	Disconnected int
	Total        int
}

// AttentionItem surfaces a condition that requires operator attention.
// Kind is one of: "lost_session", "disconnected_sessions", "awaiting_input".
// "disconnected_sessions" is machine-level: an offline machine still owns
// sessions marked disconnected (its PTYs are presumed alive pending reconnect).
type AttentionItem struct {
	Kind      string
	MachineID string
	SessionID string
	Label     string
}

// AuditEntry is a flattened representation of a recent audit event.
type AuditEntry struct {
	TS        int64
	Actor     string
	Action    string
	MachineID string
	SessionID string
	Detail    string
}

// View is the aggregated output of Overview.
type View struct {
	Totals         Totals
	Machines       []MachineRollup
	Projects       []ProjectRollup
	AttentionItems []AttentionItem
	RecentAudit    []AuditEntry
}

// UseCase aggregates fleet status/liveness into a single View.
type UseCase struct {
	machines MachineStore
	live     LiveAgents
	sessions SessionStore
	projects ProjectStore
	audit    AuditReader
	log      *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(machines MachineStore, live LiveAgents, sessions SessionStore, projects ProjectStore, audit AuditReader, log *slog.Logger) *UseCase {
	return &UseCase{
		machines: machines,
		live:     live,
		sessions: sessions,
		projects: projects,
		audit:    audit,
		log:      log,
	}
}

// Overview fetches all four data lists once and aggregates them into a View.
// It performs a single pass over sessions to build machine and project rollups.
func (u *UseCase) Overview(ctx context.Context) (View, error) {
	machines, err := u.machines.List(ctx)
	if err != nil {
		return View{}, err
	}
	sessions, err := u.sessions.List(ctx)
	if err != nil {
		return View{}, err
	}
	projects, err := u.projects.List(ctx)
	if err != nil {
		return View{}, err
	}
	auditEvents, err := u.audit.List(ctx, recentAuditLimit)
	if err != nil {
		return View{}, err
	}

	// --- machine index (id → rollup) ---
	type machineEntry struct {
		rollup       MachineRollup
		disconnected int // sessions on this machine with status disconnected
	}
	machineMap := make(map[string]*machineEntry, len(machines))
	totals := Totals{
		MachinesTotal: len(machines),
		ProjectsTotal: len(projects),
	}
	for _, m := range machines {
		online := u.live.IsOnline(m.ID())
		if online {
			totals.MachinesOnline++
		}
		machineMap[m.ID()] = &machineEntry{
			rollup: MachineRollup{
				ID:         m.ID(),
				Name:       m.Name(),
				OS:         m.OS(),
				Online:     online,
				Revoked:    m.Revoked(),
				LastSeenAt: m.LastSeenAt(),
			},
		}
	}

	// --- project index (id → rollup) ---
	projMap := make(map[string]*ProjectRollup, len(projects)+1)
	for i := range projects {
		p := &projects[i]
		projMap[p.ID()] = &ProjectRollup{
			ID:        p.ID(),
			Name:      p.Name(),
			MachineID: p.MachineID(),
		}
	}
	// Synthetic ungrouped bucket.
	const ungroupedID = ""
	ungrouped := &ProjectRollup{ID: "", Name: "Ungrouped", MachineID: ""}
	projMap[ungroupedID] = ungrouped

	// --- single pass over sessions ---
	var attention []AttentionItem
	for _, s := range sessions {
		totals.SessionsTotal++
		switch s.Status() {
		case session.StatusRunning:
			totals.SessionsRunning++
		case session.StatusExited:
			totals.SessionsExited++
		case session.StatusLost:
			totals.SessionsLost++
			// Every lost session is an attention item.
			label := s.Title()
			if label == "" {
				label = s.ID()
			}
			attention = append(attention, AttentionItem{
				Kind:      "lost_session",
				MachineID: s.MachineID(),
				SessionID: s.ID(),
				Label:     label,
			})
		case session.StatusDisconnected:
			totals.SessionsDisconnected++
		}

		// Activity counts — only tally the three named states for running sessions;
		// exited/lost sessions may carry stale activity values that should not inflate totals.
		if s.Status() == session.StatusRunning {
			switch s.Activity() {
			case session.ActivityActive:
				totals.SessionsActive++
			case session.ActivityIdle:
				totals.SessionsIdle++
			case session.ActivityAwaitingInput:
				totals.SessionsAwaitingInput++
				label := s.Title()
				if label == "" {
					label = s.ID()
				}
				attention = append(attention, AttentionItem{
					Kind:      "awaiting_input",
					MachineID: s.MachineID(),
					SessionID: s.ID(),
					Label:     label,
				})
			}
		}

		// Machine rollup.
		// Invariant: sessions are expected to reference a valid machine (FK); orphans are counted
		// in Totals but not in any MachineRollup (a transient data-integrity case, surfaced via the warning).
		if me, ok := machineMap[s.MachineID()]; ok {
			me.rollup.Total++
			switch s.Status() {
			case session.StatusRunning:
				me.rollup.Running++
			case session.StatusDisconnected:
				me.disconnected++
			}
		} else {
			u.log.Warn("dashboard: session references unknown machine", "sessionID", s.ID(), "machineID", s.MachineID())
		}

		// Project rollup.
		pid := s.ProjectID()
		if pid == "" {
			pid = ungroupedID
		}
		pr, ok := projMap[pid]
		if !ok {
			// Session references a project not in the store; treat as ungrouped.
			u.log.Warn("dashboard: session references unknown project", "sessionID", s.ID(), "projectID", pid)
			pr = projMap[ungroupedID]
		}
		pr.Total++
		switch s.Status() {
		case session.StatusRunning:
			pr.Running++
		case session.StatusExited:
			pr.Exited++
		case session.StatusLost:
			pr.Lost++
		case session.StatusDisconnected:
			pr.Disconnected++
		}
	}

	// --- offline machines with disconnected sessions ---
	// An offline machine's running sessions are marked disconnected on the drop,
	// so surface one machine-level item (not per-session) when any remain.
	for _, me := range machineMap {
		if !me.rollup.Online && me.disconnected > 0 {
			attention = append(attention, AttentionItem{
				Kind:      "disconnected_sessions",
				MachineID: me.rollup.ID,
				SessionID: "",
				Label:     me.rollup.Name,
			})
		}
	}

	// --- build sorted machine rollups ---
	machineRollups := make([]MachineRollup, 0, len(machineMap))
	for _, me := range machineMap {
		machineRollups = append(machineRollups, me.rollup)
	}
	sort.Slice(machineRollups, func(i, j int) bool {
		if machineRollups[i].Name != machineRollups[j].Name {
			return machineRollups[i].Name < machineRollups[j].Name
		}
		return machineRollups[i].ID < machineRollups[j].ID
	})

	// --- build sorted project rollups (skip empty ungrouped bucket) ---
	projectRollups := make([]ProjectRollup, 0, len(projMap))
	for _, pr := range projMap {
		if pr.ID == ungroupedID && pr.Total == 0 {
			continue // omit ungrouped when there are no project-less sessions
		}
		projectRollups = append(projectRollups, *pr)
	}
	sort.Slice(projectRollups, func(i, j int) bool {
		// Ungrouped always sorts last.
		if projectRollups[i].ID == ungroupedID {
			return false
		}
		if projectRollups[j].ID == ungroupedID {
			return true
		}
		if projectRollups[i].Name != projectRollups[j].Name {
			return projectRollups[i].Name < projectRollups[j].Name
		}
		return projectRollups[i].ID < projectRollups[j].ID
	})

	// --- sort attention items deterministically ---
	sort.Slice(attention, func(i, j int) bool {
		if attention[i].Kind != attention[j].Kind {
			return attention[i].Kind < attention[j].Kind
		}
		if attention[i].MachineID != attention[j].MachineID {
			return attention[i].MachineID < attention[j].MachineID
		}
		return attention[i].SessionID < attention[j].SessionID
	})

	// --- audit entries ---
	auditEntries := make([]AuditEntry, len(auditEvents))
	for i, e := range auditEvents {
		auditEntries[i] = AuditEntry{
			TS:        e.TS(),
			Actor:     e.Actor(),
			Action:    string(e.Action()),
			MachineID: e.MachineID(),
			SessionID: e.SessionID(),
			Detail:    e.Detail(),
		}
	}

	return View{
		Totals:         totals,
		Machines:       machineRollups,
		Projects:       projectRollups,
		AttentionItems: attention,
		RecentAudit:    auditEntries,
	}, nil
}
