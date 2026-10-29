// Package data is the data engineering pack (spec C §3): data contracts, dbt gates, pipeline review and the warehouse cost guardrail.
//
// Each workstream registers its checks, config keys, flags, guards, doctor
// probes and commands from its own register_*.go file with init, through
// internal/packs. This file is the framework's stub; nobody edits it.
package data
