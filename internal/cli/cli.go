package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"doseledger/internal/ledger"
	"doseledger/internal/round"
)

func Run(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("command required: open, record, close, show, or smoke")
	}
	switch args[0] {
	case "open":
		return runOpen(args[1:], output)
	case "record":
		return runRecord(args[1:], output)
	case "close":
		return runClose(args[1:], output)
	case "show":
		return runShow(args[1:], output)
	case "smoke":
		return runSmoke(args[1:], output)
	default:
		return fmt.Errorf("unknown command %q: use open, record, close, show, or smoke", args[0])
	}
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func runOpen(args []string, output io.Writer) error {
	flags := newFlagSet("open")
	path := flags.String("ledger", "doseledger.json", "path to the JSON ledger")
	id := flags.String("id", "", "unique round ID")
	patient := flags.String("patient", "", "patient label")
	var doseSpecs stringList
	flags.Var(&doseSpecs, "dose", "scheduled dose as id|medication|scheduled-time; repeat for each dose")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("open does not accept positional arguments")
	}
	if len(doseSpecs) == 0 {
		return errors.New("open requires at least one --dose id|medication|scheduled-time")
	}
	doses := make([]round.Dose, 0, len(doseSpecs))
	for _, spec := range doseSpecs {
		dose, err := parseDose(spec)
		if err != nil {
			return err
		}
		doses = append(doses, dose)
	}
	newRound, err := round.Open(*id, *patient, doses)
	if err != nil {
		return err
	}
	loaded, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	if err := loaded.Add(newRound); err != nil {
		return err
	}
	if err := ledger.Save(*path, loaded); err != nil {
		return err
	}
	return writeJSON(output, map[string]any{"round": newRound})
}

func runRecord(args []string, output io.Writer) error {
	flags := newFlagSet("record")
	path := flags.String("ledger", "doseledger.json", "path to the JSON ledger")
	roundID := flags.String("round", "", "round ID")
	doseID := flags.String("dose", "", "scheduled dose ID")
	kind := flags.String("outcome", "", "administered or skipped")
	note := flags.String("note", "", "optional note; an explicitly empty value is retained")
	reason := flags.String("reason", "", "required reason when outcome is skipped")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("record does not accept positional arguments")
	}
	normalizedDoseID := strings.TrimSpace(*doseID)
	normalizedRoundID := strings.TrimSpace(*roundID)
	var noteValue *string
	if wasProvided(flags, "note") {
		noteValue = copyString(note)
	}
	var reasonValue *string
	if wasProvided(flags, "reason") {
		reasonValue = copyString(reason)
	}
	newOutcome := round.Outcome{DoseID: normalizedDoseID, Kind: round.OutcomeKind(*kind), Note: noteValue, SkipReason: reasonValue}
	loaded, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	storedRound, exists := loaded.Get(normalizedRoundID)
	if !exists {
		return fmt.Errorf("round ID %q was not found", normalizedRoundID)
	}
	if err := storedRound.Record(newOutcome); err != nil {
		return err
	}
	if err := loaded.Put(storedRound); err != nil {
		return err
	}
	if err := ledger.Save(*path, loaded); err != nil {
		return err
	}
	return writeJSON(output, map[string]any{"outcome": newOutcome, "round_id": normalizedRoundID})
}

func runClose(args []string, output io.Writer) error {
	flags := newFlagSet("close")
	path := flags.String("ledger", "doseledger.json", "path to the JSON ledger")
	roundID := flags.String("round", "", "round ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("close does not accept positional arguments")
	}
	loaded, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	storedRound, exists := loaded.Get(*roundID)
	if !exists {
		return fmt.Errorf("round ID %q was not found", *roundID)
	}
	report, err := storedRound.Close(time.Now().UTC())
	if err != nil {
		return err
	}
	if err := loaded.Put(storedRound); err != nil {
		return err
	}
	if err := ledger.Save(*path, loaded); err != nil {
		return err
	}
	return writeJSON(output, map[string]any{"report": report})
}

func runShow(args []string, output io.Writer) error {
	flags := newFlagSet("show")
	path := flags.String("ledger", "doseledger.json", "path to the JSON ledger")
	roundID := flags.String("round", "", "round ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("show does not accept positional arguments")
	}
	loaded, err := ledger.Load(*path)
	if err != nil {
		return err
	}
	storedRound, exists := loaded.Get(*roundID)
	if !exists {
		return fmt.Errorf("round ID %q was not found", *roundID)
	}
	report, err := storedRound.Report()
	if err != nil {
		return err
	}
	return writeJSON(output, map[string]any{"report": report})
}

func parseDose(spec string) (round.Dose, error) {
	parts := strings.Split(spec, "|")
	if len(parts) < 2 || len(parts) > 3 {
		return round.Dose{}, fmt.Errorf("invalid dose %q: use id|medication|scheduled-time", spec)
	}
	dose := round.Dose{ID: parts[0], Medication: parts[1]}
	if len(parts) == 3 {
		dose.ScheduledFor = parts[2]
	}
	return dose, nil
}

func wasProvided(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			found = true
		}
	})
	return found
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func writeJSON(output io.Writer, value any) error {
	if err := jsonEncoder(output, value); err != nil {
		return fmt.Errorf("write JSON result: %w", err)
	}
	return nil
}

var jsonEncoder = func(output io.Writer, value any) error {
	return json.NewEncoder(output).Encode(value)
}
