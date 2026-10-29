package web

import "errors"

// A pack approval (spec C §2.11) is not answered on the board for now: a
// browser session proves nothing a same-user agent cannot script. Every web
// route refuses it (403), and the runner refuses it without the terminal
// path's human-presence marker. The board shows the decision, its evidence
// and the exact terminal command (tq hub answer, with its TTY and binding
// check and the signed ledger line). WebAuthn with user verification (a
// platform passkey) is the planned way to answer from the board.
var errPackAnswerPath = errors.New("a pack approval is answered in your own terminal with tq hub answer (the board cannot answer it)")
