// Package cloud is the cloud pack (spec D): IaC policy, IAM least privilege and the apply and destroy gate.
//
// Each workstream registers its checks, config keys, flags, guards, doctor
// probes and commands from its own register_*.go file with init, through
// internal/packs. This file is the framework's stub; nobody edits it.
package cloud
