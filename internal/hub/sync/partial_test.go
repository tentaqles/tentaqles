package sync

import (
	"errors"
	"fmt"
	"testing"
)

func TestPartialCreateError(t *testing.T) {
	cause := &RateLimitError{Provider: "acme"}
	err := fmt.Errorf("wrapped: %w", &PartialCreateError{Item: Item{ID: "acme#1"}, Err: cause})
	it, ok := CreatedItem(err)
	if !ok || it.ID != "acme#1" {
		t.Fatalf("%+v %v", it, ok)
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatal("cause not reachable")
	}
	if _, ok := CreatedItem(errors.New("plain")); ok {
		t.Fatal("plain error is not a partial create")
	}
	if _, ok := CreatedItem(&PartialCreateError{Err: cause}); ok {
		t.Fatal("no id, nothing created")
	}
}
