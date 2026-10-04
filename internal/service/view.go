package service

import (
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

// VesselRunSummary is the run information attached to a vessel view so a
// vessel can explain the run it is associated with, including why an aborted
// run left it reserved for cleaning or released it.
type VesselRunSummary struct {
	RunID           string                  `json:"run_id"`
	RunCode         string                  `json:"run_code"`
	State           domain.RunState         `json:"state"`
	AbortReasonCode *domain.AbortReasonCode `json:"abort_reason_code,omitempty"`
	AbortDetail     string                  `json:"abort_reason_detail,omitempty"`
	AbortedAt       *time.Time              `json:"aborted_at,omitempty"`
}

// VesselView is the consistent read representation of a vessel. It mirrors
// domain.Vessel and, when the vessel is associated with a run, carries a
// summary of that run.
type VesselView struct {
	ID          string             `json:"id"`
	Code        string             `json:"code"`
	CapacityL   float64            `json:"capacity_liters"`
	State       domain.VesselState `json:"state"`
	ActiveRunID *string            `json:"active_run_id,omitempty"`
	ActiveRun   *VesselRunSummary  `json:"active_run,omitempty"`
	Version     int64              `json:"version"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

func vesselView(snapshot domain.Snapshot, vessel domain.Vessel) VesselView {
	view := VesselView{
		ID:          vessel.ID,
		Code:        vessel.Code,
		CapacityL:   vessel.CapacityL,
		State:       vessel.State,
		ActiveRunID: nil,
		Version:     vessel.Version,
		CreatedAt:   vessel.CreatedAt,
		UpdatedAt:   vessel.UpdatedAt,
	}
	if vessel.ActiveRunID != nil {
		runID := *vessel.ActiveRunID
		view.ActiveRunID = &runID
		if run, ok := snapshot.Runs[runID]; ok {
			view.ActiveRun = vesselRunSummary(run)
		}
	}
	return view
}

func vesselRunSummary(run domain.FermentationRun) *VesselRunSummary {
	summary := &VesselRunSummary{
		RunID:   run.ID,
		RunCode: run.Code,
		State:   run.State,
	}
	if run.Abort != nil {
		reason := run.Abort.ReasonCode
		summary.AbortReasonCode = &reason
		summary.AbortDetail = run.Abort.ReasonDetail
		abortedAt := run.Abort.AbortedAt
		summary.AbortedAt = &abortedAt
	}
	return summary
}
