package httpapi

import (
	"context"
	"net/http"

	"github.com/rizquuula/Constellate/internal/hub/app/dashboard"
)

// DashboardService is the consumer-side port for the dashboard use case.
// *dashboard.UseCase satisfies this interface.
type DashboardService interface {
	Overview(ctx context.Context) (dashboard.View, error)
}

// DashboardDTO is the top-level JSON response for GET /api/dashboard.
type DashboardDTO struct {
	Totals         DashboardTotalsDTO        `json:"totals"`
	Machines       []DashboardMachineDTO     `json:"machines"`
	Projects       []DashboardProjectDTO     `json:"projects"`
	AttentionItems []DashboardAttentionDTO   `json:"attentionItems"`
	RecentAudit    []DashboardAuditEntryDTO  `json:"recentAudit"`
}

// DashboardTotalsDTO carries fleet-wide aggregate counts.
type DashboardTotalsDTO struct {
	MachinesOnline        int `json:"machinesOnline"`
	MachinesTotal         int `json:"machinesTotal"`
	SessionsRunning       int `json:"sessionsRunning"`
	SessionsExited        int `json:"sessionsExited"`
	SessionsLost          int `json:"sessionsLost"`
	SessionsDisconnected  int `json:"sessionsDisconnected"`
	SessionsTotal         int `json:"sessionsTotal"`
	ProjectsTotal         int `json:"projectsTotal"`
	SessionsActive        int `json:"sessionsActive"`
	SessionsIdle          int `json:"sessionsIdle"`
	SessionsAwaitingInput int `json:"sessionsAwaitingInput"`
}

// DashboardMachineDTO carries per-machine status and session counts.
type DashboardMachineDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Online     bool   `json:"online"`
	Revoked    bool   `json:"revoked"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Running    int    `json:"running"`
	Total      int    `json:"total"`
}

// DashboardProjectDTO carries per-project session status counts.
type DashboardProjectDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MachineID    string `json:"machineID"`
	Running      int    `json:"running"`
	Exited       int    `json:"exited"`
	Lost         int    `json:"lost"`
	Disconnected int    `json:"disconnected"`
	Total        int    `json:"total"`
}

// DashboardAttentionDTO surfaces a condition requiring operator attention.
type DashboardAttentionDTO struct {
	Kind      string `json:"kind"`
	MachineID string `json:"machineID"`
	SessionID string `json:"sessionID"`
	Label     string `json:"label"`
}

// DashboardAuditEntryDTO is a flattened recent audit event.
type DashboardAuditEntryDTO struct {
	TS        int64  `json:"ts"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	MachineID string `json:"machineID"`
	SessionID string `json:"sessionID"`
	Detail    string `json:"detail"`
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	view, err := s.dashboard.Overview(r.Context())
	if err != nil {
		writeError(w, statusFor(err), "dashboard_failed", err.Error())
		return
	}

	machines := make([]DashboardMachineDTO, len(view.Machines))
	for i, m := range view.Machines {
		machines[i] = DashboardMachineDTO{
			ID:         m.ID,
			Name:       m.Name,
			OS:         m.OS,
			Online:     m.Online,
			Revoked:    m.Revoked,
			LastSeenAt: m.LastSeenAt,
			Running:    m.Running,
			Total:      m.Total,
		}
	}

	projects := make([]DashboardProjectDTO, len(view.Projects))
	for i, p := range view.Projects {
		projects[i] = DashboardProjectDTO{
			ID:           p.ID,
			Name:         p.Name,
			MachineID:    p.MachineID,
			Running:      p.Running,
			Exited:       p.Exited,
			Lost:         p.Lost,
			Disconnected: p.Disconnected,
			Total:        p.Total,
		}
	}

	attentionItems := make([]DashboardAttentionDTO, len(view.AttentionItems))
	for i, a := range view.AttentionItems {
		attentionItems[i] = DashboardAttentionDTO{
			Kind:      a.Kind,
			MachineID: a.MachineID,
			SessionID: a.SessionID,
			Label:     a.Label,
		}
	}

	auditEntries := make([]DashboardAuditEntryDTO, len(view.RecentAudit))
	for i, e := range view.RecentAudit {
		auditEntries[i] = DashboardAuditEntryDTO{
			TS:        e.TS,
			Actor:     e.Actor,
			Action:    e.Action,
			MachineID: e.MachineID,
			SessionID: e.SessionID,
			Detail:    e.Detail,
		}
	}

	dto := DashboardDTO{
		Totals: DashboardTotalsDTO{
			MachinesOnline:        view.Totals.MachinesOnline,
			MachinesTotal:         view.Totals.MachinesTotal,
			SessionsRunning:       view.Totals.SessionsRunning,
			SessionsExited:        view.Totals.SessionsExited,
			SessionsLost:          view.Totals.SessionsLost,
			SessionsDisconnected:  view.Totals.SessionsDisconnected,
			SessionsTotal:         view.Totals.SessionsTotal,
			ProjectsTotal:         view.Totals.ProjectsTotal,
			SessionsActive:        view.Totals.SessionsActive,
			SessionsIdle:          view.Totals.SessionsIdle,
			SessionsAwaitingInput: view.Totals.SessionsAwaitingInput,
		},
		Machines:       machines,
		Projects:       projects,
		AttentionItems: attentionItems,
		RecentAudit:    auditEntries,
	}

	writeJSON(w, http.StatusOK, dto)
}
