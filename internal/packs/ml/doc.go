// Package ml is the ML pack (spec C §4): the leakage lint, reproducibility, the model promotion gate, model cards and drift.
//
// Each workstream registers its checks, config keys, flags, guards, doctor
// probes and commands from its own register_*.go file with init, through
// internal/packs. This file is the framework's stub; nobody edits it.
package ml
