package store

import "strings"

// batchChunk keeps every IN (...) list well under SQLite's bound-parameter limit.
const batchChunk = 500

// inChunks calls fn with "?,?,..." and the matching args for each chunk of ids.
func inChunks(ids []string, fn func(marks string, args []any) error) error {
	for start := 0; start < len(ids); start += batchChunk {
		chunk := ids[start:min(start+batchChunk, len(ids))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		if err := fn(strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ","), args); err != nil {
			return err
		}
	}
	return nil
}

// RunsForCards is RunsForCard for many cards in one query (one per 500 ids): each card's runs
// newest first (queued_at DESC, then rowid DESC), exactly as RunsForCard orders them. A card
// without runs, or an unknown id, has no entry.
func (s *Store) RunsForCards(ids []string) (map[string][]Run, error) {
	out := map[string][]Run{}
	err := inChunks(ids, func(marks string, args []any) error {
		rs, err := s.runs(`SELECT `+runCols+` FROM runs WHERE card_id IN (`+marks+`) ORDER BY queued_at DESC, rowid DESC`, args...)
		for _, r := range rs {
			out[r.CardID] = append(out[r.CardID], r)
		}
		return err
	})
	return out, err
}

// PendingCounts is PendingCount for many cards in one query; a card with none has no entry.
func (s *Store) PendingCounts(ids []string) (map[string]int, error) {
	out := map[string]int{}
	err := inChunks(ids, func(marks string, args []any) error {
		rows, err := s.DB.Query(`SELECT card_id, COUNT(*) FROM decisions WHERE status = 'pending' AND card_id IN (`+marks+`)
			GROUP BY card_id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			out[id] = n
		}
		return rows.Err()
	})
	return out, err
}

// CommentsContaining is the comments of these cards whose body contains substr (case-sensitive),
// newest first (created_at DESC, then rowid DESC: the reverse of Comments' order).
func (s *Store) CommentsContaining(ids []string, substr string) ([]Comment, error) {
	var out []Comment
	err := inChunks(ids, func(marks string, args []any) error {
		rows, err := s.DB.Query(`SELECT id, card_id, author_kind, author, body, COALESCE(run_id, ''), created_at
			FROM comments WHERE card_id IN (`+marks+`) AND instr(body, ?) > 0 ORDER BY created_at DESC, rowid DESC`, append(args, substr)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Comment
			if err := rows.Scan(&c.ID, &c.CardID, &c.AuthorKind, &c.Author, &c.Body, &c.RunID, &c.CreatedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}
