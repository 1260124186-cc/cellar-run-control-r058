package domain

import (
	"strings"
	"time"
)

// AbortReasonCode is the structured machine-readable reason an operator
// supplies when aborting a run.
type AbortReasonCode string

const (
	AbortContamination    AbortReasonCode = "contamination"
	AbortOffFlavor        AbortReasonCode = "off_flavor"
	AbortStalled          AbortReasonCode = "stalled_fermentation"
	AbortEquipmentFailure AbortReasonCode = "equipment_failure"
	AbortProcessError     AbortReasonCode = "process_error"
	AbortOther            AbortReasonCode = "other"
)

// RecoveryDestination records where the recovered (salvageable) planned
// volume is sent after an abort.
type RecoveryDestination string

const (
	RecoveryReworkedBlend RecoveryDestination = "reworked_to_blend"
	RecoveryDistilling    RecoveryDestination = "separated_for_distilling"
	RecoveryWaste         RecoveryDestination = "diverted_to_waste"
	RecoveryQAReview      RecoveryDestination = "held_for_qa_review"
	RecoverySecondProduct RecoveryDestination = "released_as_second_product"
)

const (
	maxAbortDetailRunes  = 500
	maxRecoveryNoteRunes = 500
)

// AbortDecision carries the structured information the operator must provide
// before a run is aborted. It is a request value and never persisted directly;
// AbortRecord is the fixed snapshot written at abort time.
type AbortDecision struct {
	ReasonCode          AbortReasonCode
	ReasonDetail        string
	LostVolumeL         float64
	RecoveredVolumeL    float64
	RecoveryDestination RecoveryDestination
	RecoveryNote        string
}

// Validate enforces the structured abort entry rules against the run's
// planned volume.
func (d AbortDecision) Validate(plannedVolumeL float64) error {
	if !d.ReasonCode.Valid() {
		return FieldError(CodeInvalidInput, "reason_code",
			"must be one of the supported abort reason codes")
	}
	if d.ReasonCode == AbortOther && strings.TrimSpace(d.ReasonDetail) == "" {
		return FieldError(CodeInvalidInput, "reason_detail",
			"must explain the reason when reason_code is \"other\"")
	}
	if len([]rune(d.ReasonDetail)) > maxAbortDetailRunes {
		return FieldError(CodeInvalidInput, "reason_detail",
			"must not exceed 500 characters")
	}
	if d.LostVolumeL < 0 {
		return FieldError(CodeInvalidInput, "lost_volume_liters",
			"must be zero or greater")
	}
	if d.RecoveredVolumeL < 0 {
		return FieldError(CodeInvalidInput, "recovered_volume_liters",
			"must be zero or greater")
	}
	if d.LostVolumeL+d.RecoveredVolumeL > plannedVolumeL {
		return FieldError(CodeInvalidInput, "volume_disposition",
			"lost and recovered volumes cannot exceed the planned volume")
	}
	if d.RecoveredVolumeL > 0 {
		if !d.RecoveryDestination.Valid() {
			return FieldError(CodeInvalidInput, "recovery_destination",
				"must name where the recovered volume is sent")
		}
	} else if d.RecoveryDestination != "" {
		return FieldError(CodeInvalidInput, "recovery_destination",
			"must be empty when no volume was recovered")
	}
	if len([]rune(d.RecoveryNote)) > maxRecoveryNoteRunes {
		return FieldError(CodeInvalidInput, "recovery_note",
			"must not exceed 500 characters")
	}
	return nil
}

func (c AbortReasonCode) Valid() bool {
	switch c {
	case AbortContamination,
		AbortOffFlavor,
		AbortStalled,
		AbortEquipmentFailure,
		AbortProcessError,
		AbortOther:
		return true
	default:
		return false
	}
}

func (d RecoveryDestination) Valid() bool {
	switch d {
	case RecoveryReworkedBlend,
		RecoveryDistilling,
		RecoveryWaste,
		RecoveryQAReview,
		RecoverySecondProduct:
		return true
	default:
		return false
	}
}

// AbortRecord is the immutable snapshot fixed at the moment a run is aborted.
// It records why the run stopped, how the planned volume was handled, and how
// the associated vessel was routed, so run and vessel views stay consistent.
type AbortRecord struct {
	ReasonCode          AbortReasonCode     `json:"reason_code"`
	ReasonDetail        string              `json:"reason_detail,omitempty"`
	PlannedVolumeL      float64             `json:"planned_volume_liters"`
	LostVolumeL         float64             `json:"lost_volume_liters"`
	RecoveredVolumeL    float64             `json:"recovered_volume_liters"`
	UnaccountedVolumeL  float64             `json:"unaccounted_volume_liters"`
	RecoveryDestination RecoveryDestination `json:"recovery_destination,omitempty"`
	RecoveryNote        string              `json:"recovery_note,omitempty"`
	RunStateBefore      RunState            `json:"run_state_before"`
	HadStarted          bool                `json:"had_started"`
	VesselID            *string             `json:"vessel_id,omitempty"`
	VesselStateBefore   *VesselState        `json:"vessel_state_before,omitempty"`
	VesselStateAfter    *VesselState        `json:"vessel_state_after,omitempty"`
	AbortedAt           time.Time           `json:"aborted_at"`
}

func (r AbortRecord) clone() AbortRecord {
	cloned := r
	if r.VesselID != nil {
		value := *r.VesselID
		cloned.VesselID = &value
	}
	if r.VesselStateBefore != nil {
		value := *r.VesselStateBefore
		cloned.VesselStateBefore = &value
	}
	if r.VesselStateAfter != nil {
		value := *r.VesselStateAfter
		cloned.VesselStateAfter = &value
	}
	return cloned
}
