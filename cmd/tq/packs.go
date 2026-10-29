package main

// The packs, linked into tq so their init functions register with the pack
// framework (spec C §2.4). Pack workstreams never edit this file.
import (
	_ "github.com/tentaqles/tentaqles/internal/packs/cloud"
	_ "github.com/tentaqles/tentaqles/internal/packs/data"
	_ "github.com/tentaqles/tentaqles/internal/packs/db"
	_ "github.com/tentaqles/tentaqles/internal/packs/ml"
)
