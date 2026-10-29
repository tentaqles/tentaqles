// Package db is the database pack (spec D): migration lint, the RLS gate, pgTAP tests and the restore drill.
//
// Each workstream registers its checks, config keys, flags, guards, doctor
// probes and commands from its own register_*.go file with init, through
// internal/packs. This file is the framework's stub; nobody edits it.
package db
