package round

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusActive Status = "active"
	StatusClosed Status = "closed"
)

type OutcomeKind string

const (
	OutcomeAdministered OutcomeKind = "administered"
	OutcomeSkipped      OutcomeKind = "skipped"
)

type ReportResult string

const (
	ReportComplete  ReportResult = "complete"
	ReportAttention ReportResult = "attention"
)

var (
	ErrInvalidRound      = errors.New("invalid medication round")
	ErrDuplicateDose     = errors.New("duplicate dose ID")
	ErrRoundClosed       = errors.New("round is already closed")
	ErrDoseNotFound      = errors.New("dose ID is not scheduled")
	ErrOutcomeRecorded   = errors.New("dose already has an outcome")
	ErrInvalidOutcome    = errors.New("invalid dose outcome")
	ErrIncompleteRound   = errors.New("round has unrecorded doses")
	ErrReportUnavailable = errors.New("round does not have a closed report")
)

type Dose struct {
	ID           string `json:"id"`
	Medication   string `json:"medication"`
	ScheduledFor string `json:"scheduled_for,omitempty"`
}

type Outcome struct {
	DoseID     string      `json:"dose_id"`
	Kind       OutcomeKind `json:"kind"`
	Note       *string     `json:"note,omitempty"`
	SkipReason *string     `json:"skip_reason,omitempty"`
}

type Report struct {
	RoundID      string       `json:"round_id"`
	PatientLabel string       `json:"patient_label"`
	Result       ReportResult `json:"result"`
	ClosedAt     time.Time    `json:"closed_at"`
	Doses        []Dose       `json:"doses"`
	Outcomes     []Outcome    `json:"outcomes"`
	SkippedCount int          `json:"skipped_count"`
}

type Round struct {
	ID             string             `json:"id"`
	PatientLabel   string             `json:"patient_label"`
	ScheduledDoses []Dose             `json:"scheduled_doses"`
	Outcomes       map[string]Outcome `json:"outcomes"`
	Status         Status             `json:"status"`
	ClosedReport   *Report            `json:"closed_report,omitempty"`
}

func Open(id, patientLabel string, doses []Dose) (Round, error) {
	id = strings.TrimSpace(id)
	patientLabel = strings.TrimSpace(patientLabel)
	if id == "" || patientLabel == "" {
		return Round{}, fmt.Errorf("%w: round ID and patient label are required", ErrInvalidRound)
	}
	if len(doses) == 0 {
		return Round{}, fmt.Errorf("%w: at least one scheduled dose is required", ErrInvalidRound)
	}

	copied := make([]Dose, len(doses))
	seen := make(map[string]struct{}, len(doses))
	for i, dose := range doses {
		dose.ID = strings.TrimSpace(dose.ID)
		if dose.ID == "" {
			return Round{}, fmt.Errorf("%w: dose %d has an empty ID", ErrInvalidRound, i+1)
		}
		if _, exists := seen[dose.ID]; exists {
			return Round{}, fmt.Errorf("%w: %q", ErrDuplicateDose, dose.ID)
		}
		seen[dose.ID] = struct{}{}
		copied[i] = dose
	}

	return Round{
		ID:             id,
		PatientLabel:   patientLabel,
		ScheduledDoses: copied,
		Outcomes:       make(map[string]Outcome),
		Status:         StatusActive,
	}, nil
}

func (r Round) Copy() Round {
	copyRound := r
	copyRound.ScheduledDoses = append([]Dose(nil), r.ScheduledDoses...)
	copyRound.Outcomes = make(map[string]Outcome, len(r.Outcomes))
	for id, outcome := range r.Outcomes {
		copyRound.Outcomes[id] = copyOutcome(outcome)
	}
	if r.ClosedReport != nil {
		report := copyReport(*r.ClosedReport)
		copyRound.ClosedReport = &report
	}
	return copyRound
}

func (r Round) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.PatientLabel) == "" {
		return fmt.Errorf("%w: round ID and patient label are required", ErrInvalidRound)
	}
	if len(r.ScheduledDoses) == 0 {
		return fmt.Errorf("%w: no scheduled doses", ErrInvalidRound)
	}
	seen := make(map[string]struct{}, len(r.ScheduledDoses))
	for _, dose := range r.ScheduledDoses {
		if strings.TrimSpace(dose.ID) == "" {
			return fmt.Errorf("%w: scheduled dose has an empty ID", ErrInvalidRound)
		}
		if _, exists := seen[dose.ID]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicateDose, dose.ID)
		}
		seen[dose.ID] = struct{}{}
	}
	if r.Status != StatusActive && r.Status != StatusClosed {
		return fmt.Errorf("%w: unknown round status %q", ErrInvalidRound, r.Status)
	}
	for doseID, outcome := range r.Outcomes {
		if _, exists := seen[doseID]; !exists || outcome.DoseID != doseID {
			return fmt.Errorf("%w: outcome references %q", ErrInvalidRound, doseID)
		}
		if err := validateOutcome(outcome); err != nil {
			return err
		}
	}
	if r.Status == StatusActive && r.ClosedReport != nil {
		return fmt.Errorf("%w: active round has a closed report", ErrInvalidRound)
	}
	if r.Status == StatusClosed {
		if r.ClosedReport == nil {
			return fmt.Errorf("%w: closed round has no report", ErrInvalidRound)
		}
		if len(r.Outcomes) != len(r.ScheduledDoses) {
			return fmt.Errorf("%w: closed round is incomplete", ErrInvalidRound)
		}
		if err := validateReport(*r.ClosedReport, r); err != nil {
			return err
		}
	}
	return nil
}

func (r *Round) Record(outcome Outcome) error {
	if r == nil {
		return fmt.Errorf("%w: nil round", ErrInvalidRound)
	}
	if r.Status != StatusActive {
		return ErrRoundClosed
	}
	outcome.DoseID = strings.TrimSpace(outcome.DoseID)
	if _, exists := scheduledDose(r.ScheduledDoses, outcome.DoseID); !exists {
		return ErrDoseNotFound
	}
	if _, exists := r.Outcomes[outcome.DoseID]; exists {
		return ErrOutcomeRecorded
	}
	if err := validateOutcome(outcome); err != nil {
		return err
	}
	if r.Outcomes == nil {
		r.Outcomes = make(map[string]Outcome)
	}
	r.Outcomes[outcome.DoseID] = copyOutcome(outcome)
	return nil
}

func (r *Round) Close(closedAt time.Time) (Report, error) {
	if r == nil {
		return Report{}, fmt.Errorf("%w: nil round", ErrInvalidRound)
	}
	if r.Status != StatusActive {
		return Report{}, ErrRoundClosed
	}
	if len(r.Outcomes) != len(r.ScheduledDoses) {
		return Report{}, ErrIncompleteRound
	}
	for _, dose := range r.ScheduledDoses {
		if _, exists := r.Outcomes[dose.ID]; !exists {
			return Report{}, ErrIncompleteRound
		}
	}
	if closedAt.IsZero() {
		closedAt = time.Now().UTC()
	} else {
		closedAt = closedAt.UTC()
	}

	result := ReportComplete
	skipped := 0
	outcomes := make([]Outcome, 0, len(r.ScheduledDoses))
	for _, dose := range r.ScheduledDoses {
		outcome := copyOutcome(r.Outcomes[dose.ID])
		if outcome.Kind == OutcomeSkipped {
			result = ReportAttention
			skipped++
		}
		outcomes = append(outcomes, outcome)
	}
	report := Report{
		RoundID:      r.ID,
		PatientLabel: r.PatientLabel,
		Result:       result,
		ClosedAt:     closedAt,
		Doses:        append([]Dose(nil), r.ScheduledDoses...),
		Outcomes:     outcomes,
		SkippedCount: skipped,
	}
	r.Status = StatusClosed
	r.ClosedReport = reportPointer(report)
	return copyReport(report), nil
}

// Merge returns a copy of r combined with other so that concurrent updates to
// the same round are not silently lost. A closed round is preferred over an
// active one, which preserves a concurrent close. When both rounds are active,
// outcomes from other that r does not already record are added; existing
// outcomes are kept unchanged.
func (r Round) Merge(other Round) Round {
	if r.Status == StatusClosed {
		return r.Copy()
	}
	if other.Status == StatusClosed {
		return other.Copy()
	}
	merged := r.Copy()
	if merged.Outcomes == nil {
		merged.Outcomes = make(map[string]Outcome)
	}
	for doseID, outcome := range other.Outcomes {
		if _, exists := merged.Outcomes[doseID]; !exists {
			merged.Outcomes[doseID] = copyOutcome(outcome)
		}
	}
	return merged
}

func (r Round) Report() (Report, error) {
	if r.Status != StatusClosed || r.ClosedReport == nil {
		return Report{}, ErrReportUnavailable
	}
	return copyReport(*r.ClosedReport), nil
}

func validateOutcome(outcome Outcome) error {
	if strings.TrimSpace(outcome.DoseID) == "" {
		return fmt.Errorf("%w: dose ID is required", ErrInvalidOutcome)
	}
	switch outcome.Kind {
	case OutcomeAdministered:
		if outcome.SkipReason != nil {
			return fmt.Errorf("%w: administered dose cannot have a skip reason", ErrInvalidOutcome)
		}
	case OutcomeSkipped:
		if outcome.SkipReason == nil || strings.TrimSpace(*outcome.SkipReason) == "" {
			return fmt.Errorf("%w: skipped dose requires a non-blank reason", ErrInvalidOutcome)
		}
	default:
		return fmt.Errorf("%w: outcome kind must be administered or skipped", ErrInvalidOutcome)
	}
	return nil
}

func validateReport(report Report, r Round) error {
	if report.RoundID != r.ID || report.PatientLabel != r.PatientLabel {
		return fmt.Errorf("%w: report identity does not match round", ErrInvalidRound)
	}
	if report.Result != ReportComplete && report.Result != ReportAttention {
		return fmt.Errorf("%w: unknown report result", ErrInvalidRound)
	}
	if len(report.Doses) != len(r.ScheduledDoses) || len(report.Outcomes) != len(r.ScheduledDoses) {
		return fmt.Errorf("%w: report does not contain every dose", ErrInvalidRound)
	}
	skipped := 0
	for i, dose := range report.Doses {
		if dose.ID != r.ScheduledDoses[i].ID {
			return fmt.Errorf("%w: report dose order does not match round", ErrInvalidRound)
		}
		outcome := report.Outcomes[i]
		if outcome.DoseID != dose.ID {
			return fmt.Errorf("%w: report outcome does not match dose", ErrInvalidRound)
		}
		if err := validateOutcome(outcome); err != nil {
			return err
		}
		if outcome.Kind == OutcomeSkipped {
			skipped++
		}
		if stored, ok := r.Outcomes[dose.ID]; !ok || !sameOutcome(stored, outcome) {
			return fmt.Errorf("%w: report outcome differs from round", ErrInvalidRound)
		}
	}
	if skipped != report.SkippedCount {
		return fmt.Errorf("%w: report skipped count is incorrect", ErrInvalidRound)
	}
	wantResult := ReportComplete
	if skipped > 0 {
		wantResult = ReportAttention
	}
	if report.Result != wantResult {
		return fmt.Errorf("%w: report result is incorrect", ErrInvalidRound)
	}
	return nil
}

func scheduledDose(doses []Dose, id string) (Dose, bool) {
	for _, dose := range doses {
		if dose.ID == id {
			return dose, true
		}
	}
	return Dose{}, false
}

func copyOutcome(outcome Outcome) Outcome {
	copyValue := outcome
	if outcome.Note != nil {
		value := *outcome.Note
		copyValue.Note = &value
	}
	if outcome.SkipReason != nil {
		value := *outcome.SkipReason
		copyValue.SkipReason = &value
	}
	return copyValue
}

func copyReport(report Report) Report {
	copyValue := report
	copyValue.Doses = append([]Dose(nil), report.Doses...)
	copyValue.Outcomes = make([]Outcome, len(report.Outcomes))
	for i, outcome := range report.Outcomes {
		copyValue.Outcomes[i] = copyOutcome(outcome)
	}
	return copyValue
}

func reportPointer(report Report) *Report {
	copyValue := copyReport(report)
	return &copyValue
}

func sameOutcome(left, right Outcome) bool {
	return left.DoseID == right.DoseID && left.Kind == right.Kind &&
		equalStringPointer(left.Note, right.Note) && equalStringPointer(left.SkipReason, right.SkipReason)
}

func equalStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
