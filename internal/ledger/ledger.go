package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"doseledger/internal/round"
)

const CurrentVersion = 1

type Ledger struct {
	Version int                    `json:"version"`
	Rounds  map[string]round.Round `json:"rounds"`
}

func New() Ledger {
	return Ledger{Version: CurrentVersion, Rounds: make(map[string]round.Round)}
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
	return loaded, nil
}

func Save(path string, loaded Ledger) error {
	return saveWithConcurrencyControl(path, loaded, osFileSystem{})
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

// saveWithConcurrencyControl serializes concurrent saves against the same
// ledger file, re-reads the on-disk ledger while still holding the lock, and
// merges any concurrent updates so neither concurrent record is lost. This
// makes the read-modify-write performed by a CLI process atomic across
// processes: between the time one process loaded the ledger and the time it
// saves, another process may have committed additional outcomes, and those
// outcomes are merged into the saved result.
func saveWithConcurrencyControl(path string, loaded Ledger, fs fileSystem) (saveErr error) {
	if path == "" {
		return errors.New("ledger path is required")
	}
	if err := loaded.Validate(); err != nil {
		return fmt.Errorf("validate ledger before save: %w", err)
	}

	unlock, err := lockLedger(path)
	if err != nil {
		return fmt.Errorf("lock ledger %q: %w", path, err)
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil && saveErr == nil {
			saveErr = unlockErr
		}
	}()

	merged := loaded
	if existing, err := loadForMerge(path); err == nil {
		merged = mergeLedgers(loaded, existing)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("re-read ledger %q: %w", path, err)
	}

	return saveWithFileSystem(path, merged, fs)
}

func loadForMerge(path string) (Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Ledger{}, err
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
	return loaded, nil
}

// mergeLedgers combines a pending ledger with the current on-disk state so
// concurrent updates from other processes are preserved. Rounds present only
// on disk are retained unchanged; rounds present in both are merged at the
// round level so concurrent outcomes for different doses are all kept.
func mergeLedgers(pending, current Ledger) Ledger {
	merged := pending
	if merged.Version == 0 {
		merged.Version = CurrentVersion
	}
	if merged.Rounds == nil {
		merged.Rounds = make(map[string]round.Round)
	}
	for id, currentRound := range current.Rounds {
		pendingRound, exists := merged.Rounds[id]
		if !exists {
			merged.Rounds[id] = currentRound.Copy()
			continue
		}
		merged.Rounds[id] = pendingRound.Merge(currentRound)
	}
	return merged
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
