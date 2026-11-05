package cmd

import "github.com/tentaqles/tentaqles/internal/protect"

// ensureProtectedHome creates $TQ_HOME private to the user (internal/protect:
// 0700, and on Windows the one icacls run, recorded for tq doctor). The
// commands that create the state folder call it: tq init, tq add and
// tq migrate manifests --apply. It never fails because icacls failed. Tests
// replace it so no test changes a real folder's ACL.
var ensureProtectedHome = protect.EnsureHome
