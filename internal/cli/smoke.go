package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"doseledger/internal/ledger"
	"doseledger/internal/round"
)

func runSmoke(args []string, output io.Writer) error {
	if len(args) != 0 {
		return errors.New("smoke does not accept arguments")
	}
	directory, err := os.MkdirTemp("", "doseledger-smoke-")
	if err != nil {
		return fmt.Errorf("create smoke workspace: %w", err)
	}
	defer os.RemoveAll(directory)

	path := filepath.Join(directory, "ledger.json")
	newRound, err := round.Open("smoke-round", "Moss", []round.Dose{
		{ID: "morning", Medication: "amoxicillin", ScheduledFor: "08:00"},
		{ID: "evening", Medication: "amoxicillin", ScheduledFor: "20:00"},
	})
	if err != nil {
		return err
	}
	loaded, err := ledger.Load(path)
	if err != nil {
		return err
	}
	if err := loaded.Add(newRound); err != nil {
		return err
	}
	if err := ledger.Save(path, loaded); err != nil {
		return err
	}

	loaded, err = ledger.Load(path)
	if err != nil {
		return err
	}
	active, exists := loaded.Get("smoke-round")
	if !exists {
		return errors.New("smoke round was not persisted")
	}
	note := "given with food"
	if err := active.Record(round.Outcome{DoseID: "morning", Kind: round.OutcomeAdministered, Note: &note}); err != nil {
		return err
	}
	if err := loaded.Put(active); err != nil {
		return err
	}
	if err := ledger.Save(path, loaded); err != nil {
		return err
	}

	loaded, err = ledger.Load(path)
	if err != nil {
		return err
	}
	active, exists = loaded.Get("smoke-round")
	if !exists {
		return errors.New("smoke round disappeared after recording")
	}
	reason := "patient was already resting"
	if err := active.Record(round.Outcome{DoseID: "evening", Kind: round.OutcomeSkipped, SkipReason: &reason}); err != nil {
		return err
	}
	if err := loaded.Put(active); err != nil {
		return err
	}
	if err := ledger.Save(path, loaded); err != nil {
		return err
	}

	loaded, err = ledger.Load(path)
	if err != nil {
		return err
	}
	active, exists = loaded.Get("smoke-round")
	if !exists {
		return errors.New("smoke round disappeared before closing")
	}
	report, err := active.Close(time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	if err := loaded.Put(active); err != nil {
		return err
	}
	if err := ledger.Save(path, loaded); err != nil {
		return err
	}

	loaded, err = ledger.Load(path)
	if err != nil {
		return err
	}
	closed, exists := loaded.Get("smoke-round")
	if !exists {
		return errors.New("smoke round disappeared after closing")
	}
	retrieved, err := closed.Report()
	if err != nil {
		return err
	}
	if retrieved.Result != round.ReportAttention || retrieved.SkippedCount != 1 || len(retrieved.Outcomes) != 2 {
		return errors.New("smoke report did not match the completed workflow")
	}
	return writeJSON(output, map[string]any{"status": "ok", "report": report})
}
