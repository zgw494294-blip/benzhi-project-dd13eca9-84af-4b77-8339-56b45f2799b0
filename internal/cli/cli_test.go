package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRunCompletesJSONCommandWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	var output bytes.Buffer
	if err := Run([]string{"open", "--ledger", path, "--id", "r-1", "--patient", "Moss", "--dose", "dose-1|amoxicillin|08:00"}, &output); err != nil {
		t.Fatal(err)
	}
	if !validJSONLine(t, output.Bytes()) {
		t.Fatalf("open output is not JSON: %q", output.String())
	}
	output.Reset()
	if err := Run([]string{"record", "--ledger", path, "--round", "r-1", "--dose", "dose-1", "--outcome", "administered", "--note", ""}, &output); err != nil {
		t.Fatal(err)
	}
	if !validJSONLine(t, output.Bytes()) {
		t.Fatalf("record output is not JSON: %q", output.String())
	}
	output.Reset()
	if err := Run([]string{"close", "--ledger", path, "--round", "r-1"}, &output); err != nil {
		t.Fatal(err)
	}
	if !validJSONLine(t, output.Bytes()) {
		t.Fatalf("close output is not JSON: %q", output.String())
	}
	output.Reset()
	if err := Run([]string{"show", "--ledger", path, "--round", "r-1"}, &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Report struct {
			Result string `json:"result"`
		} `json:"report"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Report.Result != "complete" {
		t.Fatalf("show result = %q, want complete", response.Report.Result)
	}
}

func validJSONLine(t *testing.T, data []byte) bool {
	t.Helper()
	var value any
	return json.Unmarshal(data, &value) == nil
}
