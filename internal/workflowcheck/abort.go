package workflowcheck

import (
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

// abortVesselCleaning exercises structured aborts both before and after a
// formal start, verifies the fixed abort snapshot and automatic vessel
// routing, and confirms finish-cleaning semantics stay unchanged.
func abortVesselCleaning(api *client) error {
	formula, err := ensureApprovedFormula(api, "Abort Formula")
	if err != nil {
		return err
	}
	if err := abortBeforeStart(api, formula.ID); err != nil {
		return err
	}
	if err := abortAfterStart(api, formula.ID); err != nil {
		return err
	}
	if err := abortMissingRun(api); err != nil {
		return err
	}
	return nil
}

func abortBeforeStart(api *client, formulaID string) error {
	vessel, err := createCheckVessel(api, "VSL-301", 250)
	if err != nil {
		return err
	}
	runResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-ABORT-BEFORE", formulaID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(runResponse, 201, "create pre-start abort run"); err != nil {
		return err
	}
	var run runView
	if err := runResponse.decode(&run); err != nil {
		return err
	}
	reserveResponse, err := api.call("POST", "/v1/runs/"+run.ID+"/reserve",
		map[string]any{"vessel_id": vessel.ID}, ptrInt64(run.Version))
	if err != nil {
		return err
	}
	if err := requireStatus(reserveResponse, 200, "reserve pre-start abort vessel"); err != nil {
		return err
	}
	var reserved pairView
	if err := reserveResponse.decode(&reserved); err != nil {
		return err
	}

	// An empty, unstructured abort body must be rejected without writes.
	empty, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		map[string]any{}, ptrInt64(reserved.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(empty, 400, string(domain.CodeInvalidInput),
		"reject abort without a reason"); err != nil {
		return err
	}

	// Volume accounting that does not cover the planned volume must fail.
	badAccounts, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		abortPayload(domain.AbortPlanChange, 100, 0, ""),
		ptrInt64(reserved.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(badAccounts, 400, string(domain.CodeInvalidInput),
		"reject unbalanced abort volume"); err != nil {
		return err
	}

	abortResponse, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		abortPayload(domain.AbortPlanChange, 180, 0, ""),
		ptrInt64(reserved.Run.Version))
	if err != nil {
		return err
	}
	if err := requireStatus(abortResponse, 200, "abort before start"); err != nil {
		return err
	}
	var aborted pairView
	if err := abortResponse.decode(&aborted); err != nil {
		return err
	}
	if err := requireRunState(aborted.Run, domain.RunAborted); err != nil {
		return err
	}
	if err := requireVesselState(aborted.Vessel, domain.VesselAvailable); err != nil {
		return err
	}
	if aborted.Run.AbortRecord == nil {
		return errInvalidWorkflow("abort record was not returned")
	}
	if aborted.Run.AbortRecord.Reason != domain.AbortPlanChange ||
		aborted.Run.AbortRecord.LostVolumeL != 180 ||
		aborted.Run.AbortRecord.PlannedVolumeL != 180 ||
		aborted.Run.AbortRecord.HadStarted ||
		aborted.Run.AbortRecord.ObservationCount != 0 ||
		aborted.Run.AbortRecord.VesselDisposition != domain.VesselReleased {
		return errInvalidWorkflow("pre-start abort snapshot is inconsistent")
	}
	if aborted.Vessel.Cleaning != nil || aborted.Vessel.ActiveRunID != nil {
		return errInvalidWorkflow("released vessel retained abort association")
	}

	// Repeating the abort must fail atomically with the state error.
	repeat, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		abortPayload(domain.AbortPlanChange, 180, 0, ""),
		ptrInt64(aborted.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(repeat, 409, string(domain.CodeRunState),
		"abort run twice"); err != nil {
		return err
	}
	return nil
}

func abortAfterStart(api *client, formulaID string) error {
	vessel, err := createCheckVessel(api, "VSL-302", 300)
	if err != nil {
		return err
	}
	started, err := createStartedRun(api, formulaID, vessel.ID, "RUN-ABORT-AFTER")
	if err != nil {
		return err
	}

	// Recovered volume without a destination must be rejected.
	missingDestination, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload(domain.AbortContamination, 120, 60, ""),
		ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(missingDestination, 400, string(domain.CodeInvalidInput),
		"reject recovered volume without destination"); err != nil {
		return err
	}

	// A destination is only meaningful together with recovered volume.
	strayDestination, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload(domain.AbortContamination, 180, 0, string(domain.RecoveryDrain)),
		ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(strayDestination, 400, string(domain.CodeInvalidInput),
		"reject destination without recovered volume"); err != nil {
		return err
	}

	abortResponse, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload(domain.AbortContamination, 120, 60, string(domain.RecoveryBlend)),
		ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireStatus(abortResponse, 200, "abort after start"); err != nil {
		return err
	}
	var aborted pairView
	if err := abortResponse.decode(&aborted); err != nil {
		return err
	}
	if err := requireVesselState(aborted.Vessel, domain.VesselCleaning); err != nil {
		return err
	}
	if aborted.Run.AbortRecord == nil || !aborted.Run.AbortRecord.HadStarted ||
		aborted.Run.AbortRecord.RecoveredVolumeL != 60 ||
		aborted.Run.AbortRecord.RecoveryDestination != domain.RecoveryBlend ||
		aborted.Run.AbortRecord.VesselDisposition != domain.VesselScheduledCleaning {
		return errInvalidWorkflow("post-start abort snapshot is inconsistent")
	}
	if aborted.Vessel.Cleaning == nil ||
		aborted.Vessel.Cleaning.Trigger != domain.CleaningFromAbort ||
		aborted.Vessel.Cleaning.RunID != started.Run.ID ||
		aborted.Vessel.Cleaning.Reason == nil ||
		*aborted.Vessel.Cleaning.Reason != domain.AbortContamination ||
		aborted.Vessel.ActiveRunID == nil ||
		*aborted.Vessel.ActiveRunID != started.Run.ID {
		return errInvalidWorkflow("cleaning vessel does not explain the abort")
	}

	// Cleaning cannot finish while the open-run rule would disagree; here the
	// run is aborted, so finishing cleaning releases the vessel.
	finishResponse, err := api.call("POST",
		"/v1/vessels/"+vessel.ID+"/finish-cleaning", nil,
		ptrInt64(aborted.Vessel.Version))
	if err != nil {
		return err
	}
	if err := requireStatus(finishResponse, 200, "finish abort cleaning"); err != nil {
		return err
	}
	var cleaned vesselView
	if err := finishResponse.decode(&cleaned); err != nil {
		return err
	}
	if err := requireVesselState(cleaned, domain.VesselAvailable); err != nil {
		return err
	}
	if cleaned.ActiveRunID != nil || cleaned.Cleaning != nil {
		return errInvalidWorkflow("cleaned vessel retained run or cleaning context")
	}

	// Run detail and list views must still explain the abort after release.
	readResponse, err := api.call("GET", "/v1/runs/"+started.Run.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(readResponse, 200, "read aborted run"); err != nil {
		return err
	}
	var readBack runView
	if err := readResponse.decode(&readBack); err != nil {
		return err
	}
	if readBack.AbortRecord == nil || readBack.AbortRecord.PlannedVolumeL != 180 ||
		readBack.AbortRecord.VesselDisposition != domain.VesselScheduledCleaning {
		return errInvalidWorkflow("run detail lost the fixed abort snapshot")
	}

	listResponse, err := api.call("GET", "/v1/runs", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(listResponse, 200, "list runs"); err != nil {
		return err
	}
	var listed struct {
		Runs []runView `json:"runs"`
	}
	if err := listResponse.decode(&listed); err != nil {
		return err
	}
	found := false
	for _, entry := range listed.Runs {
		if entry.ID == started.Run.ID {
			found = entry.AbortRecord != nil &&
				entry.AbortRecord.RecoveryDestination == domain.RecoveryBlend
		}
	}
	if !found {
		return errInvalidWorkflow("run list does not explain the abort")
	}

	vesselListResponse, err := api.call("GET", "/v1/vessels", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(vesselListResponse, 200, "list vessels"); err != nil {
		return err
	}
	var vessels struct {
		Vessels []vesselView `json:"vessels"`
	}
	if err := vesselListResponse.decode(&vessels); err != nil {
		return err
	}
	for _, entry := range vessels.Vessels {
		if entry.ID == vessel.ID && (entry.State != domain.VesselAvailable ||
			entry.ActiveRunID != nil || entry.Cleaning != nil) {
			return errInvalidWorkflow("vessel list does not match released vessel")
		}
	}
	return nil
}

func abortMissingRun(api *client) error {
	missing, err := api.call("POST", "/v1/runs/run_missing/abort",
		abortPayload(domain.AbortOther, 1, 0, ""), nil)
	if err != nil {
		return err
	}
	if err := requireCode(missing, 404, string(domain.CodeRunNotFound),
		"abort missing run"); err != nil {
		return err
	}
	return nil
}

func abortPayload(reason domain.AbortReason, lost, recovered float64, destination string) map[string]any {
	payload := map[string]any{
		"reason":                  string(reason),
		"reason_detail":           "workflow check abort",
		"lost_volume_liters":      lost,
		"recovered_volume_liters": recovered,
	}
	if destination != "" {
		payload["recovery_destination"] = destination
	}
	return payload
}
