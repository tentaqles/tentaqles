package readers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Review r1 Major 1: a change set that is not CREATE_COMPLETE and
// AVAILABLE is never read as complete; its Changes may still be empty.
func TestChangeSetNotReady(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtures, "invalid", "changeset-pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := ChangeSet("cs.json", raw, Options{}); !errors.Is(err, ErrIncomplete) || rs != nil {
		t.Fatalf("CREATE_PENDING: rs=%v err=%v", rs, err)
	}
	if _, err := Redact(model.KindChangeSet, raw); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Redact of a pending change set: %v", err)
	}
	if _, _, err := ReadAuto("cs.json", raw, Options{}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("ReadAuto: %v", err)
	}
	for _, s := range []string{
		`{"ChangeSetName":"c","StackName":"s","Status":"CREATE_IN_PROGRESS","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":"FAILED","ExecutionStatus":"UNAVAILABLE","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":"DELETE_COMPLETE","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":"","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":7,"Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":"CREATE_COMPLETE","ExecutionStatus":"UNAVAILABLE","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":"CREATE_COMPLETE","ExecutionStatus":"EXECUTE_IN_PROGRESS","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","ExecutionStatus":"OBSOLETE","Changes":[]}`,
	} {
		if _, err := ChangeSet("cs.json", []byte(s), Options{}); !errors.Is(err, ErrIncomplete) {
			t.Errorf("%s: err = %v", s, err)
		}
	}
	for _, s := range []string{
		`{"ChangeSetName":"c","StackName":"s","Status":"CREATE_COMPLETE","ExecutionStatus":"AVAILABLE","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Status":"create_complete","Changes":[]}`,
		`{"ChangeSetName":"c","StackName":"s","Changes":[]}`,
	} {
		if _, err := ChangeSet("cs.json", []byte(s), Options{}); err != nil {
			t.Errorf("%s: err = %v", s, err)
		}
	}
}
