package round

import (
	"errors"
	"testing"
	"time"
)

func TestOpenRejectsInvalidDoseCollectionsWithoutSharingThem(t *testing.T) {
	if _, err := Open("r-1", "Patient", nil); !errors.Is(err, ErrInvalidRound) {
		t.Fatalf("Open(nil) error = %v, want invalid round", err)
	}
	if _, err := Open("r-1", "Patient", []Dose{{ID: "dose-1", Medication: "ointment"}, {ID: "dose-1", Medication: "ointment"}}); !errors.Is(err, ErrDuplicateDose) {
		t.Fatalf("duplicate dose error = %v, want duplicate dose", err)
	}

	doses := []Dose{{ID: "dose-1", Medication: "ointment"}}
	opened, err := Open("r-1", "Patient", doses)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	doses[0].ID = "changed"
	doses[0].Medication = "changed"
	if opened.ScheduledDoses[0].ID != "dose-1" || opened.ScheduledDoses[0].Medication != "ointment" {
		t.Fatalf("round shares caller-owned dose collection: %#v", opened.ScheduledDoses)
	}
}

func TestRecordPreservesOptionalValuesAndLeavesUnknownAttemptsAlone(t *testing.T) {
	opened, err := Open("r-1", "Patient", []Dose{{ID: "dose-1", Medication: "ointment"}, {ID: "dose-2", Medication: "tablet"}, {ID: "dose-3", Medication: "drops"}})
	if err != nil {
		t.Fatal(err)
	}
	note := ""
	if err := opened.Record(Outcome{DoseID: "dose-1", Kind: OutcomeAdministered, Note: &note}); err != nil {
		t.Fatal(err)
	}
	if opened.Outcomes["dose-1"].Note == nil || *opened.Outcomes["dose-1"].Note != "" {
		t.Fatalf("empty supplied note was not retained: %#v", opened.Outcomes["dose-1"])
	}
	if err := opened.Record(Outcome{DoseID: "dose-2", Kind: OutcomeAdministered}); err != nil {
		t.Fatal(err)
	}
	if opened.Outcomes["dose-2"].Note != nil {
		t.Fatalf("omitted note was not nil: %#v", opened.Outcomes["dose-2"])
	}
	if err := opened.Record(Outcome{DoseID: "dose-3", Kind: OutcomeSkipped, SkipReason: stringPointer("  ")}); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("blank skip reason error = %v, want invalid outcome", err)
	}
	if len(opened.Outcomes) != 2 {
		t.Fatalf("invalid skip changed outcomes: %#v", opened.Outcomes)
	}
	if err := opened.Record(Outcome{DoseID: "missing", Kind: OutcomeAdministered}); !errors.Is(err, ErrDoseNotFound) {
		t.Fatalf("unknown dose error = %v, want dose not found", err)
	}
	if len(opened.Outcomes) != 2 {
		t.Fatalf("unknown dose changed outcomes: %#v", opened.Outcomes)
	}
	if err := opened.Record(Outcome{DoseID: "dose-1", Kind: OutcomeAdministered}); !errors.Is(err, ErrOutcomeRecorded) {
		t.Fatalf("duplicate outcome error = %v, want outcome recorded", err)
	}
	if len(opened.Outcomes) != 2 {
		t.Fatalf("duplicate outcome changed outcomes: %#v", opened.Outcomes)
	}
}

func TestCloseDerivesOneWayReports(t *testing.T) {
	opened, err := Open("r-1", "Patient", []Dose{{ID: "dose-1", Medication: "ointment"}, {ID: "dose-2", Medication: "tablet"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Close(time.Now()); !errors.Is(err, ErrIncompleteRound) {
		t.Fatalf("incomplete close error = %v, want incomplete round", err)
	}
	if opened.Status != StatusActive || opened.ClosedReport != nil {
		t.Fatalf("failed close changed state: %#v", opened)
	}
	if err := opened.Record(Outcome{DoseID: "dose-1", Kind: OutcomeAdministered}); err != nil {
		t.Fatal(err)
	}
	reason := "not available"
	if err := opened.Record(Outcome{DoseID: "dose-2", Kind: OutcomeSkipped, SkipReason: &reason}); err != nil {
		t.Fatal(err)
	}
	closedAt := time.Date(2026, time.August, 17, 10, 30, 0, 0, time.UTC)
	report, err := opened.Close(closedAt)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Status != StatusClosed || opened.ClosedReport == nil {
		t.Fatalf("close did not persist state: %#v", opened)
	}
	if report.Result != ReportAttention || report.SkippedCount != 1 || !report.ClosedAt.Equal(closedAt) {
		t.Fatalf("unexpected attention report: %#v", report)
	}
	if _, err := opened.Close(closedAt.Add(time.Hour)); !errors.Is(err, ErrRoundClosed) {
		t.Fatalf("second close error = %v, want closed error", err)
	}
	if err := opened.Record(Outcome{DoseID: "dose-1", Kind: OutcomeAdministered}); !errors.Is(err, ErrRoundClosed) {
		t.Fatalf("record after close error = %v, want closed error", err)
	}

	retrieved, err := opened.Report()
	if err != nil {
		t.Fatal(err)
	}
	retrieved.Outcomes[0].DoseID = "changed"
	again, err := opened.Report()
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcomes[0].DoseID != "dose-1" {
		t.Fatalf("report accessor exposed mutable state: %#v", again.Outcomes)
	}
}

func TestCloseAllAdministeredProducesCompleteReport(t *testing.T) {
	opened, err := Open("r-complete", "Patient", []Dose{{ID: "dose-1", Medication: "ointment"}, {ID: "dose-2", Medication: "tablet"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, doseID := range []string{"dose-1", "dose-2"} {
		if err := opened.Record(Outcome{DoseID: doseID, Kind: OutcomeAdministered}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := opened.Close(time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Result != ReportComplete || report.SkippedCount != 0 {
		t.Fatalf("unexpected complete report: %#v", report)
	}
}

func stringPointer(value string) *string {
	return &value
}
