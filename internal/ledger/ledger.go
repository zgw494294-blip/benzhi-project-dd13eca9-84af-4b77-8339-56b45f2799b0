package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"doseledger/internal/round"
)

const CurrentVersion = 1

type Ledger struct {
	Version         int                    `json:"version"`
	Rounds          map[string]round.Round `json:"rounds"`
	baselineVersion int
	baselineRounds  map[string]round.Round
	hasBaseline     bool
}

func New() Ledger {
	loaded := Ledger{Version: CurrentVersion, Rounds: make(map[string]round.Round)}
	loaded.captureBaseline()
	return loaded
}

func (l Ledger) Validate() error {
	if l.Version != CurrentVersion {
		return fmt.Errorf("unsupported ledger version %d", l.Version)
	}
	for id, storedRound := range l.Rounds {
		if id != storedRound.ID {
			return fmt.Errorf("ledger key %q does not match round ID %q", id, storedRound.ID)
		}
		if err := storedRound.Validate(); err != nil {
			return fmt.Errorf("round %q: %w", id, err)
		}
	}
	return nil
}

func (l *Ledger) Add(storedRound round.Round) error {
	if l == nil {
		return errors.New("nil ledger")
	}
	if err := storedRound.Validate(); err != nil {
		return err
	}
	if l.Version == 0 {
		l.Version = CurrentVersion
	}
	if l.Version != CurrentVersion {
		return fmt.Errorf("unsupported ledger version %d", l.Version)
	}
	if l.Rounds == nil {
		l.Rounds = make(map[string]round.Round)
	}
	if _, exists := l.Rounds[storedRound.ID]; exists {
		return fmt.Errorf("round ID %q already exists", storedRound.ID)
	}
	l.Rounds[storedRound.ID] = storedRound.Copy()
	return nil
}

func (l Ledger) Get(id string) (round.Round, bool) {
	storedRound, ok := l.Rounds[id]
	if !ok {
		return round.Round{}, false
	}
	return storedRound.Copy(), true
}

func (l *Ledger) Put(storedRound round.Round) error {
	if l == nil {
		return errors.New("nil ledger")
	}
	if err := storedRound.Validate(); err != nil {
		return err
	}
	if l.Version != CurrentVersion {
		return fmt.Errorf("unsupported ledger version %d", l.Version)
	}
	if l.Rounds == nil {
		l.Rounds = make(map[string]round.Round)
	}
	if _, exists := l.Rounds[storedRound.ID]; !exists {
		return fmt.Errorf("round ID %q does not exist", storedRound.ID)
	}
	l.Rounds[storedRound.ID] = storedRound.Copy()
	return nil
}

func Load(path string) (Ledger, error) {
	if path == "" {
		return Ledger{}, errors.New("ledger path is required")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return Ledger{}, fmt.Errorf("read ledger %q: %w", path, err)
	}
	var loaded Ledger
	if err := json.Unmarshal(data, &loaded); err != nil {
		return Ledger{}, fmt.Errorf("decode ledger %q: %w", path, err)
	}
	if loaded.Rounds == nil {
		loaded.Rounds = make(map[string]round.Round)
	}
	if err := loaded.Validate(); err != nil {
		return Ledger{}, fmt.Errorf("validate ledger %q: %w", path, err)
	}
	loaded.captureBaseline()
	return loaded, nil
}

func Save(path string, loaded Ledger) (saveErr error) {
	if path == "" {
		return errors.New("ledger path is required")
	}
	if err := loaded.Validate(); err != nil {
		return fmt.Errorf("validate ledger before save: %w", err)
	}
	lock, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.release(); err != nil {
			saveErr = errors.Join(saveErr, err)
		}
	}()

	current, err := Load(path)
	if err != nil {
		return err
	}
	merged, err := mergeLedgers(current, loaded)
	if err != nil {
		return err
	}
	return saveWithFileSystem(path, merged, osFileSystem{})
}

func (l *Ledger) captureBaseline() {
	l.baselineVersion = l.Version
	l.baselineRounds = copyRounds(l.Rounds)
	l.hasBaseline = true
}

func copyRounds(source map[string]round.Round) map[string]round.Round {
	copied := make(map[string]round.Round, len(source))
	for id, storedRound := range source {
		copied[id] = storedRound.Copy()
	}
	return copied
}

func mergeLedgers(current, desired Ledger) (Ledger, error) {
	if !desired.hasBaseline {
		return desired, nil
	}
	if desired.baselineVersion != current.Version {
		return Ledger{}, fmt.Errorf("ledger changed from version %d to %d", desired.baselineVersion, current.Version)
	}

	merged := Ledger{Version: current.Version, Rounds: copyRounds(current.Rounds)}
	for id, baselineRound := range desired.baselineRounds {
		if _, exists := desired.Rounds[id]; exists {
			continue
		}
		currentRound, exists := current.Rounds[id]
		if !exists {
			continue
		}
		if !reflect.DeepEqual(currentRound, baselineRound) {
			return Ledger{}, fmt.Errorf("round %q changed concurrently", id)
		}
		delete(merged.Rounds, id)
	}

	for id, desiredRound := range desired.Rounds {
		baselineRound, existedAtLoad := desired.baselineRounds[id]
		if existedAtLoad && reflect.DeepEqual(desiredRound, baselineRound) {
			continue
		}
		currentRound, exists := current.Rounds[id]
		if !existedAtLoad {
			if exists && !reflect.DeepEqual(currentRound, desiredRound) {
				return Ledger{}, fmt.Errorf("round %q was added concurrently", id)
			}
			if !exists {
				merged.Rounds[id] = desiredRound.Copy()
			}
			continue
		}
		if !exists {
			return Ledger{}, fmt.Errorf("round %q was removed concurrently", id)
		}
		mergedRound, err := mergeRound(baselineRound, desiredRound, currentRound)
		if err != nil {
			return Ledger{}, fmt.Errorf("round %q: %w", id, err)
		}
		merged.Rounds[id] = mergedRound
	}
	return merged, nil
}

func mergeRound(baseline, desired, current round.Round) (round.Round, error) {
	desired = desired.Copy()
	current = current.Copy()
	merged := current.Copy()
	var err error
	if merged.ID, err = mergeValue("ID", baseline.ID, desired.ID, current.ID); err != nil {
		return round.Round{}, err
	}
	if merged.PatientLabel, err = mergeValue("patient label", baseline.PatientLabel, desired.PatientLabel, current.PatientLabel); err != nil {
		return round.Round{}, err
	}
	if merged.ScheduledDoses, err = mergeValue("scheduled doses", baseline.ScheduledDoses, desired.ScheduledDoses, current.ScheduledDoses); err != nil {
		return round.Round{}, err
	}
	if merged.Status, err = mergeValue("status", baseline.Status, desired.Status, current.Status); err != nil {
		return round.Round{}, err
	}
	if merged.ClosedReport, err = mergeValue("closed report", baseline.ClosedReport, desired.ClosedReport, current.ClosedReport); err != nil {
		return round.Round{}, err
	}

	touchedOutcomes := make(map[string]struct{}, len(baseline.Outcomes)+len(desired.Outcomes))
	for doseID := range baseline.Outcomes {
		touchedOutcomes[doseID] = struct{}{}
	}
	for doseID := range desired.Outcomes {
		touchedOutcomes[doseID] = struct{}{}
	}
	for doseID := range touchedOutcomes {
		baselineOutcome, existedAtLoad := baseline.Outcomes[doseID]
		desiredOutcome, existsInDesired := desired.Outcomes[doseID]
		if sameOutcomeState(baselineOutcome, existedAtLoad, desiredOutcome, existsInDesired) {
			continue
		}
		currentOutcome, existsInCurrent := current.Outcomes[doseID]
		if sameOutcomeState(baselineOutcome, existedAtLoad, currentOutcome, existsInCurrent) {
			if existsInDesired {
				merged.Outcomes[doseID] = desiredOutcome
			} else {
				delete(merged.Outcomes, doseID)
			}
			continue
		}
		if !sameOutcomeState(desiredOutcome, existsInDesired, currentOutcome, existsInCurrent) {
			return round.Round{}, fmt.Errorf("outcome for dose %q changed concurrently", doseID)
		}
	}
	if err := merged.Validate(); err != nil {
		return round.Round{}, err
	}
	return merged, nil
}

func mergeValue[T any](name string, baseline, desired, current T) (T, error) {
	if reflect.DeepEqual(desired, baseline) || reflect.DeepEqual(desired, current) {
		return current, nil
	}
	if reflect.DeepEqual(current, baseline) {
		return desired, nil
	}
	var zero T
	return zero, fmt.Errorf("%s changed concurrently", name)
}

func sameOutcomeState(left round.Outcome, leftExists bool, right round.Outcome, rightExists bool) bool {
	return leftExists == rightExists && (!leftExists || reflect.DeepEqual(left, right))
}

type ledgerLock struct {
	file *os.File
}

func acquireLock(path string) (*ledgerLock, error) {
	directory := filepath.Dir(path)
	lockPath := filepath.Join(directory, "."+filepath.Base(path)+".lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open ledger lock %q: %w", lockPath, err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock ledger %q: %w", path, err)
	}
	return &ledgerLock{file: file}, nil
}

func (lock *ledgerLock) release() error {
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock ledger: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close ledger lock: %w", closeErr)
	}
	return errors.Join(unlockErr, closeErr)
}

type tempFile interface {
	io.Writer
	Name() string
	Sync() error
	Close() error
}

type fileSystem interface {
	CreateTemp(dir, pattern string) (tempFile, error)
	Rename(oldPath, newPath string) error
	Remove(name string) error
}

type osFileSystem struct{}

func (osFileSystem) CreateTemp(dir, pattern string) (tempFile, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (osFileSystem) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (osFileSystem) Remove(name string) error {
	return os.Remove(name)
}

func saveWithFileSystem(path string, loaded Ledger, fs fileSystem) (saveErr error) {
	if path == "" {
		return errors.New("ledger path is required")
	}
	if err := loaded.Validate(); err != nil {
		return fmt.Errorf("validate ledger before save: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := fs.CreateTemp(directory, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		if temporary != nil {
			temporaryName := temporary.Name()
			_ = temporary.Close()
			_ = fs.Remove(temporaryName)
		}
		return fmt.Errorf("create temporary ledger in %q: %w", directory, err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		if committed {
			return
		}
		if removeErr := fs.Remove(temporaryName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			saveErr = errors.Join(saveErr, fmt.Errorf("remove temporary ledger %q: %w", temporaryName, removeErr))
		}
	}()

	if err := json.NewEncoder(temporary).Encode(loaded); err != nil {
		return finishSaveFailure(temporary, fmt.Errorf("encode ledger: %w", err))
	}
	if err := temporary.Sync(); err != nil {
		return finishSaveFailure(temporary, fmt.Errorf("sync temporary ledger: %w", err))
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary ledger: %w", err)
	}
	if err := fs.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("rename temporary ledger into place: %w", err)
	}
	committed = true
	return nil
}

func finishSaveFailure(temporary tempFile, primary error) error {
	if closeErr := temporary.Close(); closeErr != nil {
		return errors.Join(primary, fmt.Errorf("close temporary ledger after failure: %w", closeErr))
	}
	return primary
}
