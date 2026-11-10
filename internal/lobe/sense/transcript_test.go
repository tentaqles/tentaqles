package sense

import (
	"fmt"
	"strings"
	"testing"
)

// TestLobeRejectedParse runs Rejected on made-up transcript chunks, one per case.
func TestLobeRejectedParse(t *testing.T) {
	editUse := `{"type":"assistant","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_a1","name":"Edit","input":{"file_path":"C:\\x\\notes.txt","old_string":"ZZZ","new_string":"x"}}]}}`
	editFail := `{"type":"user","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>String to replace not found in file.\nString: ZZZ</tool_use_error>","is_error":true,"tool_use_id":"toolu_a1"}]}}`
	const editInput = `{"file_path":"C:\\x\\notes.txt","old_string":"ZZZ","new_string":"x"}`

	cases := []struct {
		name string
		data string
		want []Call
	}{
		{
			name: "rejected Edit with string content",
			data: editUse + "\n" + editFail + "\n",
			want: []Call{{Tool: "Edit", CallID: "toolu_a1", Input: []byte(editInput), Error: "String to replace not found in file.\nString: ZZZ"}},
		},
		{
			name: "rejected call with list content",
			data: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_b2","name":"Edit","input":{"file_path":"C:\\x\\todo.txt","old_string":"ab","new_string":"cd"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"<tool_use_error>Found 2 matches of the string to replace"},{"type":"text","text":"Give more context.</tool_use_error>"}],"is_error":true,"tool_use_id":"toolu_b2"}]}}`,
			want: []Call{{Tool: "Edit", CallID: "toolu_b2", Input: []byte(`{"file_path":"C:\\x\\todo.txt","old_string":"ab","new_string":"cd"}`), Error: "Found 2 matches of the string to replace\nGive more context."}},
		},
		{
			name: "shell failure without the wrapper is not counted",
			data: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_c3","name":"Bash","input":{"command":"make boom"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"Exit code 1\nboom","is_error":true,"tool_use_id":"toolu_c3"}]}}`,
			want: nil,
		},
		{
			name: "successful results are not counted",
			data: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"1\tline","is_error":false,"tool_use_id":"toolu_d4"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>looks like an error</tool_use_error>","tool_use_id":"toolu_d5"}]}}`,
			want: nil,
		},
		{
			name: "a repeated block in an api-request-blob line is counted once",
			data: editUse + "\n" + editFail + "\n" +
				`{"type":"api-request-blob","message":{"role":"user","content":[{"type":"tool_use","id":"toolu_a1","name":"Edit","input":{"file_path":"C:\\x\\notes.txt","old_string":"ZZZ","new_string":"x"}},{"type":"tool_result","content":"<tool_use_error>String to replace not found in file.\nString: ZZZ</tool_use_error>","is_error":true,"tool_use_id":"toolu_a1"}]}}` + "\n",
			want: []Call{{Tool: "Edit", CallID: "toolu_a1", Input: []byte(editInput), Error: "String to replace not found in file.\nString: ZZZ"}},
		},
		{
			name: "no tool_use in the chunk gives unknown and {}",
			data: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>File does not exist.</tool_use_error>","is_error":true,"tool_use_id":"toolu_f7"}]}}`,
			want: []Call{{Tool: "unknown", CallID: "toolu_f7", Input: []byte("{}"), Error: "File does not exist."}},
		},
		{
			name: "a broken line is skipped",
			data: editUse + "\n" + `{"type":"user","message":{"role":"user","content":[` + "\n" + editFail + "\n",
			want: []Call{{Tool: "Edit", CallID: "toolu_a1", Input: []byte(editInput), Error: "String to replace not found in file.\nString: ZZZ"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Rejected([]byte(tc.data))
			if len(got) != len(tc.want) {
				t.Fatalf("got %d calls, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				g := got[i]
				if g.Tool != w.Tool || g.CallID != w.CallID || g.Error != w.Error || string(g.Input) != string(w.Input) {
					t.Errorf("call %d = {Tool:%q CallID:%q Input:%s Error:%q}, want {Tool:%q CallID:%q Input:%s Error:%q}",
						i, g.Tool, g.CallID, g.Input, g.Error, w.Tool, w.CallID, w.Input, w.Error)
				}
				if g.Session != "" || g.Cwd != "" || g.IsInterrupt {
					t.Errorf("call %d: Session/Cwd/IsInterrupt not zero: %+v", i, g)
				}
			}
		})
	}
}

// TestLobeRejectedCap checks that 250 distinct rejected results give the first 200 calls.
func TestLobeRejectedCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%03d","name":"Edit","input":{"file_path":"f.txt"}}]}}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>nope</tool_use_error>","is_error":true,"tool_use_id":"toolu_%03d"}]}}`+"\n", i)
	}
	got := Rejected([]byte(b.String()))
	if len(got) != 200 {
		t.Fatalf("got %d calls, want 200", len(got))
	}
	if got[0].CallID != "toolu_000" || got[199].CallID != "toolu_199" {
		t.Errorf("first/last = %q/%q, want toolu_000/toolu_199", got[0].CallID, got[199].CallID)
	}
}

// TestLobeRejectedLookBack checks that prev gives the tool_use of a result in chunk, and that a
// rejected result that is only in prev is never counted.
func TestLobeRejectedLookBack(t *testing.T) {
	use := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_p1","name":"Edit","input":{"file_path":"C:\\x\\p.txt","old_string":"ab","new_string":"cd"}}]}}` + "\n"
	result := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>String to replace not found in file.</tool_use_error>","is_error":true,"tool_use_id":"toolu_p1"}]}}` + "\n"

	// prev holds whole lines: a whole filler line first, so the cut keeps the tool_use.
	filler := `{"type":"user","message":{"role":"user","content":[]}}` + "\n"

	// The tool_use is only in prev: the call gets its tool and input from it.
	got, resume := RejectedWithContext([]byte(filler+use), []byte(result))
	if len(got) != 1 || got[0].Tool != "Edit" || got[0].CallID != "toolu_p1" {
		t.Fatalf("tool_use in prev: got %+v, want one Edit call toolu_p1", got)
	}
	if string(got[0].Input) != `{"file_path":"C:\\x\\p.txt","old_string":"ab","new_string":"cd"}` {
		t.Errorf("input = %s, want the tool_use's input", got[0].Input)
	}
	if resume != -1 {
		t.Errorf("resume = %d, want -1", resume)
	}

	// The rejected result is only in prev: nothing is counted, whatever the chunk holds.
	got, _ = RejectedWithContext([]byte(filler+use+result), []byte(`{"type":"user","message":{"role":"user","content":[]}}`+"\n"))
	if len(got) != 0 {
		t.Errorf("rejected result in prev: got %+v, want none", got)
	}

	// A partial first line of prev is dropped, so a cut tool_use cannot name a call.
	got, _ = RejectedWithContext([]byte(`ing"}]}}`+"\n"+use[:len(use)/2]), []byte(result))
	if len(got) != 1 || got[0].Tool != "unknown" {
		t.Errorf("cut prev: got %+v, want one unknown call", got)
	}
}

// TestLobeRejectedResume checks that when more than 200 calls are found, resume is the offset just
// after the line of the 200th, so reading from there gives the rest.
func TestLobeRejectedResume(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%03d","name":"Edit","input":{"file_path":"f.txt"}}]}}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>nope</tool_use_error>","is_error":true,"tool_use_id":"toolu_%03d"}]}}`+"\n", i)
	}
	data := []byte(b.String())
	got, resume := RejectedWithContext(nil, data)
	if len(got) != 200 || resume < 0 {
		t.Fatalf("got %d calls, resume %d; want 200 and a resume offset", len(got), resume)
	}
	rest, again := RejectedWithContext(nil, data[resume:])
	if len(rest) != 50 || again != -1 || rest[0].CallID != "toolu_200" {
		t.Fatalf("rest: %d calls, first %+v, resume %d; want 50 from toolu_200 and -1", len(rest), rest, again)
	}
}
