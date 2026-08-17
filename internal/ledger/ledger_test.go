package ledger

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doseledger/internal/round"
)

func TestSaveAndLoadRoundTripPreservesOptionalPointers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	storedRound, err := round.Open("r-1", "Patient", []round.Dose{{ID: "dose-1"}, {ID: "dose-2"}})
	if err != nil {
		t.Fatal(err)
	}
	note := ""
	if err := storedRound.Record(round.Outcome{DoseID: "dose-1", Kind: round.OutcomeAdministered, Note: &note}); err != nil {
		t.Fatal(err)
	}
	reason := "not in stock"
	if err := storedRound.Record(round.Outcome{DoseID: "dose-2", Kind: round.OutcomeSkipped, SkipReason: &reason}); err != nil {
		t.Fatal(err)
	}
	if _, err := storedRound.Close(testTime()); err != nil {
		t.Fatal(err)
	}
	document := New()
	if err := document.Add(storedRound); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, document); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loadedRound, ok := loaded.Get("r-1")
	if !ok || loadedRound.ClosedReport == nil {
		t.Fatalf("round was not loaded: %#v", loaded)
	}
	if loadedRound.ClosedReport.Outcomes[0].Note == nil || *loadedRound.ClosedReport.Outcomes[0].Note != "" {
		t.Fatalf("supplied empty note was not retained: %#v", loadedRound.ClosedReport.Outcomes[0])
	}
	if loadedRound.ClosedReport.Outcomes[1].SkipReason == nil || *loadedRound.ClosedReport.Outcomes[1].SkipReason != reason {
		t.Fatalf("skip reason was not retained: %#v", loadedRound.ClosedReport.Outcomes[1])
	}
}

func TestLoadRejectsMissingParentAndMalformedLedger(t *testing.T) {
	missingParent := filepath.Join(t.TempDir(), "missing", "ledger.json")
	if err := Save(missingParent, New()); err == nil {
		t.Fatal("Save() with missing parent returned nil error")
	}
	path := filepath.Join(t.TempDir(), "ledger.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "decode ledger") {
		t.Fatalf("malformed ledger error = %v", err)
	}
}

func TestFailedAtomicSaveStagesPropagateAndRemoveTemporaryFiles(t *testing.T) {
	valid := New()
	for _, test := range []struct {
		name       string
		writeError error
		syncError  error
		closeError error
		renameErr  error
		wantText   string
	}{
		{name: "encode", writeError: errors.New("write failed"), wantText: "encode ledger"},
		{name: "sync", syncError: errors.New("sync failed"), wantText: "sync temporary ledger"},
		{name: "close", closeError: errors.New("close failed"), wantText: "close temporary ledger"},
		{name: "rename", renameErr: errors.New("rename failed"), wantText: "rename temporary ledger"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := &fakeTempFile{name: ".ledger.tmp-1", writeError: test.writeError, syncError: test.syncError, closeError: test.closeError}
			fs := &fakeFileSystem{file: file, renameError: test.renameErr}
			err := saveWithFileSystem(filepath.Join("state", "ledger.json"), valid, fs)
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("save error = %v, want text %q", err, test.wantText)
			}
			if len(fs.removed) != 1 || fs.removed[0] != file.name {
				t.Fatalf("temporary files removed = %#v, want %q", fs.removed, file.name)
			}
		})
	}
}

func TestRenameFailureDoesNotReplaceExistingState(t *testing.T) {
	valid := New()
	file := &fakeTempFile{name: ".ledger.tmp-1"}
	fs := &fakeFileSystem{file: file, renameError: errors.New("rename failed")}
	path := filepath.Join("state", "ledger.json")
	if err := saveWithFileSystem(path, valid, fs); err == nil {
		t.Fatal("rename failure returned nil error")
	}
	if fs.renamedTarget != path || len(fs.removed) != 1 {
		t.Fatalf("rename cleanup was not attempted: %#v", fs)
	}
}

func testTime() (value time.Time) {
	return time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
}

type fakeFileSystem struct {
	file          *fakeTempFile
	renameError   error
	removed       []string
	renamedTarget string
}

func (fs *fakeFileSystem) CreateTemp(_ string, _ string) (tempFile, error) {
	return fs.file, nil
}

func (fs *fakeFileSystem) Rename(_, newPath string) error {
	fs.renamedTarget = newPath
	return fs.renameError
}

func (fs *fakeFileSystem) Remove(name string) error {
	fs.removed = append(fs.removed, name)
	return nil
}

type fakeTempFile struct {
	bytes.Buffer
	name       string
	writeError error
	syncError  error
	closeError error
}

func (file *fakeTempFile) Name() string {
	return file.name
}

func (file *fakeTempFile) Write(value []byte) (int, error) {
	if file.writeError != nil {
		return 0, file.writeError
	}
	return file.Buffer.Write(value)
}

func (file *fakeTempFile) Sync() error {
	return file.syncError
}

func (file *fakeTempFile) Close() error {
	return file.closeError
}
