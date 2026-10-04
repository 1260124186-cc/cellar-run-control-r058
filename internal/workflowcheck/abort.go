package workflowcheck

import (
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

// abortCleaning exercises the structured abort flow: the operator records why
// a run stops and how its planned volume is handled, the snapshot is fixed at
// abort time, and the vessel routing rules on either side of formal start are
// checked together with vessel release through finish-cleaning.
func abortCleaning(api *client) error {
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

	missingResponse, err := api.call("POST", "/v1/runs/run_deadbeef00/abort",
		abortPayload("process_error", "nonexistent", 0, 0, "", ""), nil)
	if err != nil {
		return err
	}
	if err := requireCode(missingResponse, 404, string(domain.CodeRunNotFound),
		"abort a missing run"); err != nil {
		return err
	}
	return nil
}

func abortBeforeStart(api *client, formulaID string) error {
	vessel, err := createCheckVessel(api, "VSL-301", 240)
	if err != nil {
		return err
	}
	runResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-ABORT-EARLY", formulaID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(runResponse, 201, "create early-abort run"); err != nil {
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
	if err := requireStatus(reserveResponse, 200, "reserve early-abort vessel"); err != nil {
		return err
	}
	var reserved pairView
	if err := reserveResponse.decode(&reserved); err != nil {
		return err
	}

	withoutBody, err := api.call("POST", "/v1/runs/"+run.ID+"/abort", nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(withoutBody, 400, string(domain.CodeInvalidInput),
		"abort without a structured body"); err != nil {
		return err
	}
	badReason, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		abortPayload("exploded", "", 0, 0, "", ""), ptrInt64(reserved.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(badReason, 400, string(domain.CodeInvalidInput),
		"abort with an unknown reason code"); err != nil {
		return err
	}

	abortResponse, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		abortPayload("process_error", "wrong grain bill pitched", 0, 0, "", ""),
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
	if aborted.Run.Abort == nil {
		return errInvalidWorkflow("aborted run did not fix an abort record")
	}
	record := aborted.Run.Abort
	if record.ReasonCode != domain.AbortProcessError ||
		record.HadStarted ||
		record.PlannedVolumeL != 180 ||
		record.LostVolumeL != 0 ||
		record.RecoveredVolumeL != 0 ||
		record.UnaccountedVolumeL != 180 {
		return errInvalidWorkflow("early abort record did not capture the entry or volumes")
	}
	if record.VesselStateAfter == nil || *record.VesselStateAfter != domain.VesselAvailable {
		return errInvalidWorkflow("abort snapshot did not fix the released vessel state")
	}
	if aborted.Vessel.ActiveRunID != nil || aborted.Vessel.ActiveRun != nil {
		return errInvalidWorkflow("released vessel still references the aborted run")
	}
	if record.AbortedAt.IsZero() {
		return errInvalidWorkflow("abort snapshot was not fixed to an abort time")
	}

	repeatResponse, err := api.call("POST", "/v1/runs/"+run.ID+"/abort",
		abortPayload("process_error", "again", 0, 0, "", ""),
		ptrInt64(aborted.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(repeatResponse, 409, string(domain.CodeRunState),
		"abort the same run twice"); err != nil {
		return err
	}

	readResponse, err := api.call("GET", "/v1/vessels/"+vessel.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(readResponse, 200, "read released vessel"); err != nil {
		return err
	}
	var released vesselView
	if err := readResponse.decode(&released); err != nil {
		return err
	}
	if err := requireVesselState(released, domain.VesselAvailable); err != nil {
		return err
	}
	if released.ActiveRunID != nil || released.ActiveRun != nil {
		return errInvalidWorkflow("vessel detail kept a stale run association")
	}

	// A released vessel must be immediately usable by a new run.
	reuseRun, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-ABORT-REUSE", formulaID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(reuseRun, 201, "create reuse run"); err != nil {
		return err
	}
	var reuse runView
	if err := reuseRun.decode(&reuse); err != nil {
		return err
	}
	reuseResponse, err := api.call("POST", "/v1/runs/"+reuse.ID+"/reserve",
		map[string]any{"vessel_id": vessel.ID}, ptrInt64(reuse.Version))
	if err != nil {
		return err
	}
	if err := requireStatus(reuseResponse, 200, "reserve the released vessel again"); err != nil {
		return err
	}
	return nil
}

func abortAfterStart(api *client, formulaID string) error {
	vessel, err := createCheckVessel(api, "VSL-302", 300)
	if err != nil {
		return err
	}
	started, err := createStartedRun(api, formulaID, vessel.ID, "RUN-ABORT-LATE")
	if err != nil {
		return err
	}

	staleResponse, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload("contamination", "stale revision", 0, 0, "", ""),
		ptrInt64(started.Run.Version-1))
	if err != nil {
		return err
	}
	if err := requireCode(staleResponse, 412, string(domain.CodeRevisionConflict),
		"abort with a stale version"); err != nil {
		return err
	}
	unknownField, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		map[string]any{
			"reason":      "contamination",
			"reason_code": "contamination",
		}, ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(unknownField, 400, string(domain.CodeInvalidInput),
		"abort with an unknown field"); err != nil {
		return err
	}
	otherWithoutDetail, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload("other", "", 0, 0, "", ""), ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(otherWithoutDetail, 400, string(domain.CodeInvalidInput),
		"abort with reason other and no detail"); err != nil {
		return err
	}
	tooMuchVolume, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload("contamination", "too much", 100, 90, "diverted_to_waste", ""),
		ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(tooMuchVolume, 400, string(domain.CodeInvalidInput),
		"abort with volumes above the planned volume"); err != nil {
		return err
	}
	recoveredWithoutDestination, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload("contamination", "salvage", 0, 40, "", ""),
		ptrInt64(started.Run.Version))
	if err != nil {
		return err
	}
	if err := requireCode(recoveredWithoutDestination, 400, string(domain.CodeInvalidInput),
		"abort with recovered volume but no destination"); err != nil {
		return err
	}

	readBefore, err := api.call("GET", "/v1/runs/"+started.Run.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(readBefore, 200, "read run after failed aborts"); err != nil {
		return err
	}
	var unchanged runView
	if err := readBefore.decode(&unchanged); err != nil {
		return err
	}
	if err := requireRunState(unchanged, domain.RunFermenting); err != nil {
		return err
	}
	if unchanged.Version != started.Run.Version || unchanged.Abort != nil {
		return errInvalidWorkflow("failed abort attempts changed run state")
	}

	abortResponse, err := api.call("POST", "/v1/runs/"+started.Run.ID+"/abort",
		abortPayload("contamination", "film on the surface", 60, 100,
			"separated_for_distilling", "sent to the stripping still"),
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
	if err := requireRunState(aborted.Run, domain.RunAborted); err != nil {
		return err
	}
	if err := requireVesselState(aborted.Vessel, domain.VesselCleaning); err != nil {
		return err
	}
	record := aborted.Run.Abort
	if record == nil {
		return errInvalidWorkflow("started run abort did not fix a record")
	}
	if !record.HadStarted ||
		record.ReasonCode != domain.AbortContamination ||
		record.PlannedVolumeL != 180 ||
		record.LostVolumeL != 60 ||
		record.RecoveredVolumeL != 100 ||
		record.UnaccountedVolumeL != 20 ||
		record.RecoveryDestination != domain.RecoveryDistilling {
		return errInvalidWorkflow("late abort record captured the wrong disposition")
	}
	if record.VesselStateAfter == nil || *record.VesselStateAfter != domain.VesselCleaning {
		return errInvalidWorkflow("abort snapshot did not fix the cleaning vessel state")
	}
	if aborted.Vessel.ActiveRunID == nil || *aborted.Vessel.ActiveRunID != started.Run.ID {
		return errInvalidWorkflow("cleaning vessel lost its run association")
	}
	if aborted.Vessel.ActiveRun == nil ||
		aborted.Vessel.ActiveRun.State != domain.RunAborted ||
		aborted.Vessel.ActiveRun.AbortReasonCode == nil ||
		*aborted.Vessel.ActiveRun.AbortReasonCode != domain.AbortContamination {
		return errInvalidWorkflow("vessel detail cannot explain why its run was aborted")
	}

	if err := requireConsistentLists(api, started.Run.ID, vessel.ID); err != nil {
		return err
	}

	cleanResponse, err := api.call("POST", "/v1/vessels/"+vessel.ID+"/finish-cleaning",
		nil, ptrInt64(aborted.Vessel.Version))
	if err != nil {
		return err
	}
	if err := requireStatus(cleanResponse, 200, "finish cleaning after abort"); err != nil {
		return err
	}
	var clean vesselView
	if err := cleanResponse.decode(&clean); err != nil {
		return err
	}
	if err := requireVesselState(clean, domain.VesselAvailable); err != nil {
		return err
	}
	if clean.ActiveRunID != nil {
		return errInvalidWorkflow("finishing cleaning did not release the vessel")
	}
	repeatClean, err := api.call("POST", "/v1/vessels/"+vessel.ID+"/finish-cleaning",
		nil, ptrInt64(clean.Version))
	if err != nil {
		return err
	}
	if err := requireCode(repeatClean, 409, string(domain.CodeVesselState),
		"finish cleaning an available vessel"); err != nil {
		return err
	}

	// Aborted runs are immutable: observations are rejected on state before
	// timestamps are even considered.
	postAbortObservation, err := api.call("POST",
		"/v1/runs/"+started.Run.ID+"/observations",
		map[string]any{
			"sequence":      1,
			"gravity":       1.02,
			"temperature_c": 20,
			"observed_at":   record.AbortedAt,
		}, nil)
	if err != nil {
		return err
	}
	if err := requireCode(postAbortObservation, 409, string(domain.CodeRunState),
		"append an observation after abort"); err != nil {
		return err
	}
	return nil
}

func requireConsistentLists(api *client, runID, vesselID string) error {
	runListResponse, err := api.call("GET", "/v1/runs", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(runListResponse, 200, "list runs"); err != nil {
		return err
	}
	var runList struct {
		Runs []runView `json:"runs"`
	}
	if err := runListResponse.decode(&runList); err != nil {
		return err
	}
	var listedRun *runView
	for index := range runList.Runs {
		if runList.Runs[index].ID == runID {
			listedRun = &runList.Runs[index]
		}
	}
	if listedRun == nil {
		return errInvalidWorkflow("aborted run missing from run list")
	}
	if listedRun.State != domain.RunAborted || listedRun.Abort == nil ||
		listedRun.Abort.ReasonCode != domain.AbortContamination {
		return errInvalidWorkflow("run list cannot explain the abort")
	}

	vesselListResponse, err := api.call("GET", "/v1/vessels", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(vesselListResponse, 200, "list vessels"); err != nil {
		return err
	}
	var vesselList struct {
		Vessels []vesselView `json:"vessels"`
	}
	if err := vesselListResponse.decode(&vesselList); err != nil {
		return err
	}
	var listedVessel *vesselView
	for index := range vesselList.Vessels {
		if vesselList.Vessels[index].ID == vesselID {
			listedVessel = &vesselList.Vessels[index]
		}
	}
	if listedVessel == nil {
		return errInvalidWorkflow("cleaning vessel missing from vessel list")
	}
	if listedVessel.State != domain.VesselCleaning ||
		listedVessel.ActiveRun == nil ||
		listedVessel.ActiveRun.RunID != runID ||
		listedVessel.ActiveRun.State != domain.RunAborted ||
		listedVessel.ActiveRun.AbortReasonCode == nil ||
		*listedVessel.ActiveRun.AbortReasonCode != domain.AbortContamination {
		return errInvalidWorkflow("vessel list cannot explain the abort routing")
	}
	return nil
}

func abortPayload(
	reason string,
	detail string,
	lost float64,
	recovered float64,
	destination string,
	note string,
) map[string]any {
	return map[string]any{
		"reason_code":             reason,
		"reason_detail":           detail,
		"lost_volume_liters":      lost,
		"recovered_volume_liters": recovered,
		"recovery_destination":    destination,
		"recovery_note":           note,
	}
}
