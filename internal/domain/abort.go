package domain

import (
	"time"
)

// AbortReason is the structured operator-selected reason for aborting a run.
type AbortReason string

const (
	AbortContamination AbortReason = "contamination"
	AbortEquipment     AbortReason = "equipment_failure"
	AbortQuality       AbortReason = "quality_deviation"
	AbortTemperature   AbortReason = "temperature_excursion"
	AbortPlanChange    AbortReason = "plan_change"
	AbortOther         AbortReason = "other"
)

// RecoveryDestination records where recovered volume was sent.
type RecoveryDestination string

const (
	RecoveryBlend        RecoveryDestination = "blend_back"
	RecoveryReuse        RecoveryDestination = "reuse_next_batch"
	RecoveryDistillation RecoveryDestination = "distillation"
	RecoveryDrain        RecoveryDestination = "drain"
	RecoveryOther        RecoveryDestination = "other"
)

// VesselDisposition records the automatic vessel routing performed at abort.
type VesselDisposition string

const (
	// VesselReleased is used when a never-started reservation is released
	// straight back to available.
	VesselReleased VesselDisposition = "released_available"
	// VesselScheduledCleaning is used once fermentation started.
	VesselScheduledCleaning VesselDisposition = "scheduled_cleaning"
	// VesselUnassigned is used when the run never reserved a vessel.
	VesselUnassigned VesselDisposition = "none"
)

// CleaningTrigger explains why a vessel entered cleaning.
type CleaningTrigger string

const (
	CleaningFromCompletion CleaningTrigger = "run_completed"
	CleaningFromAbort      CleaningTrigger = "run_aborted"
)

// AbortInput carries the structured information the operator must supply
// before a run can be aborted.
type AbortInput struct {
	Reason              AbortReason         `json:"reason"`
	ReasonDetail        string              `json:"reason_detail,omitempty"`
	LostVolumeL         float64             `json:"lost_volume_liters"`
	RecoveredVolumeL    float64             `json:"recovered_volume_liters"`
	RecoveryDestination RecoveryDestination `json:"recovery_destination,omitempty"`
}

// volumeAccountingTolerance tolerates rounded liter entries when checking that
// lost plus recovered volume accounts for the planned volume.
const volumeAccountingTolerance = 0.01

// Validate verifies the structured abort payload independently of run state.
func (in AbortInput) Validate(plannedVolumeL float64) error {
	if !in.Reason.Valid() {
		return FieldError(CodeInvalidInput, "reason",
			"must be one of contamination, equipment_failure, quality_deviation, "+
				"temperature_excursion, plan_change, other")
	}
	if len([]rune(in.ReasonDetail)) > 1000 {
		return FieldError(CodeInvalidInput, "reason_detail",
			"must not exceed 1000 characters")
	}
	if in.LostVolumeL < 0 {
		return FieldError(CodeInvalidInput, "lost_volume_liters",
			"must not be negative")
	}
	if in.RecoveredVolumeL < 0 {
		return FieldError(CodeInvalidInput, "recovered_volume_liters",
			"must not be negative")
	}
	if in.RecoveredVolumeL > 0 && in.RecoveryDestination == "" {
		return FieldError(CodeInvalidInput, "recovery_destination",
			"is required when recovered volume is greater than zero")
	}
	if in.RecoveredVolumeL == 0 && in.RecoveryDestination != "" {
		return FieldError(CodeInvalidInput, "recovery_destination",
			"is only allowed when recovered volume is greater than zero")
	}
	if in.RecoveryDestination != "" && !in.RecoveryDestination.Valid() {
		return FieldError(CodeInvalidInput, "recovery_destination",
			"must be one of blend_back, reuse_next_batch, distillation, drain, other")
	}
	total := in.LostVolumeL + in.RecoveredVolumeL
	difference := total - plannedVolumeL
	if difference < 0 {
		difference = -difference
	}
	if difference > volumeAccountingTolerance {
		return FieldError(CodeInvalidInput, "recovered_volume_liters",
			"lost and recovered volume must account for the planned volume")
	}
	return nil
}

func (reason AbortReason) Valid() bool {
	switch reason {
	case AbortContamination, AbortEquipment, AbortQuality,
		AbortTemperature, AbortPlanChange, AbortOther:
		return true
	default:
		return false
	}
}

func (destination RecoveryDestination) Valid() bool {
	switch destination {
	case RecoveryBlend, RecoveryReuse, RecoveryDistillation,
		RecoveryDrain, RecoveryOther:
		return true
	default:
		return false
	}
}

// AbortRecord is the fixed, immutable snapshot of an abort. It captures the
// operator-supplied reason and planned-volume handling together with the run
// facts as they were at the abort instant.
type AbortRecord struct {
	Reason              AbortReason         `json:"reason"`
	ReasonDetail        string              `json:"reason_detail,omitempty"`
	LostVolumeL         float64             `json:"lost_volume_liters"`
	RecoveredVolumeL    float64             `json:"recovered_volume_liters"`
	RecoveryDestination RecoveryDestination `json:"recovery_destination,omitempty"`
	PlannedVolumeL      float64             `json:"planned_volume_liters"`
	ObservationCount    int                 `json:"observation_count"`
	HadStarted          bool                `json:"had_started"`
	VesselID            *string             `json:"vessel_id,omitempty"`
	VesselDisposition   VesselDisposition   `json:"vessel_disposition"`
	AbortedAt           time.Time           `json:"aborted_at"`
	RunVersion          int64               `json:"run_version_before_abort"`
}

func (r AbortRecord) Clone() AbortRecord {
	cloned := r
	if r.VesselID != nil {
		value := *r.VesselID
		cloned.VesselID = &value
	}
	return cloned
}
