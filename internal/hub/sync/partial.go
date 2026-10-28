package sync

import "errors"

// PartialCreateError is the Create contract for a failure after the tracker item exists: Item is
// the created item (at least ID, Key and URL), Err the step that failed (a sub-issue link, a board
// add, a status, a rate limit). The engine records Item.ID as the card's link and must not create
// the item again; it may retry the failed step (Move, SetLabels) separately. An adapter returns
// a plain error, with an empty Item, only when nothing was created.
type PartialCreateError struct {
	Item Item
	Err  error
}

func (e *PartialCreateError) Error() string {
	return "created " + e.Item.ID + ", then: " + e.Err.Error()
}

func (e *PartialCreateError) Unwrap() error { return e.Err }

// ErrCreateUncertain: Create sent the request but cannot tell whether the tracker made the item
// (a timeout, a dropped connection, a 5xx, an unreadable answer) and knows no id. The engine must
// not create again blindly: it looks the item up (by title or marker) first, or asks the human.
var ErrCreateUncertain = errors.New("the tracker may have created the item")

// CreatedItem is the item a failed Create still made, if any.
func CreatedItem(err error) (Item, bool) {
	var pc *PartialCreateError
	if errors.As(err, &pc) && pc.Item.ID != "" {
		return pc.Item, true
	}
	return Item{}, false
}
