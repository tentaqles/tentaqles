package data

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func TestPipelineChecksRegistered(t *testing.T) {
	want := map[string]packs.Kind{
		"data/airflow-dag": packs.Builtin, "data/pipeline-backfill": packs.Builtin, "data/pipeline-idempotency": packs.Builtin,
		"data/pipeline-pii-output": packs.Builtin, "data/fabric-artifacts": packs.Builtin, "data/synapse-artifacts": packs.Builtin,
		"data/notebook-outputs": packs.Builtin, "data/dagster-validate": packs.External,
	}
	got := map[string]packs.Check{}
	for _, c := range packs.Checks("data") {
		got[c.ID] = c
	}
	for id, k := range want {
		c, ok := got[id]
		if !ok || c.Kind != k {
			t.Fatalf("%s: %+v", id, c)
		}
	}
	if c := got["data/dagster-validate"]; c.Tool != "dagster" || c.Timeout.Seconds() != 120 {
		t.Fatalf("dagster check: %+v", c)
	}
	found := false
	for _, g := range packs.Guards() {
		if g.ID == "tq/pipeline-run" && g.Pack == "data" {
			found = true
		}
	}
	if !found {
		t.Fatal("tq/pipeline-run not registered")
	}
}

func node(t *testing.T, body string) yaml.Node {
	t.Helper()
	var d yaml.Node
	if err := yaml.Unmarshal([]byte(body), &d); err != nil {
		t.Fatal(err)
	}
	return *d.Content[0]
}

func TestValidatePipelines(t *testing.T) {
	for body, wantErr := range map[string]string{
		"paths: [dags/**, etl/]\nmasking: [acme.mask_email]\n": "",
		"paths: dags/**\n":          "must be a list",
		"masking: ['drop table']\n": "not a function name",
		"paths: ['../elsewhere']\n": "inside the repo",
		"retries: 3\n":              `unknown key "retries"`,
		"paths: ['']\n":             "non-empty",
	} {
		err := validatePipelines(node(t, body))
		if wantErr == "" && err != nil || wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)) {
			t.Errorf("%q: %v (want %q)", body, err, wantErr)
		}
	}
	if err := validatePipelines(node(t, "[a]\n")); err == nil {
		t.Error("a list is not a mapping")
	}
}

// The manifest validator composed by RegisterPack knows the pipelines key.
func TestManifestKnowsPipelinesKey(t *testing.T) {
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte("data:\n  pipelines: { paths: [dags/**] }\n"), &m); err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidatePacks(m); err != nil {
		t.Fatalf("pipelines rejected: %v", err)
	}
	if err := yaml.Unmarshal([]byte("data:\n  pipelines: { pathz: [dags/**] }\n"), &m); err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidatePacks(m); err == nil {
		t.Fatal("a typo under pipelines is accepted")
	}
}

func TestPipelineMergeRules(t *testing.T) {
	trusted := map[string]manifest.PackConfig{"data": mustPack(t, "pipelines: { paths: [dags/**], masking: [acme_mask] }\n")}
	repo := map[string]manifest.PackConfig{"data": mustPack(t, "pipelines: { paths: [etl/**], masking: [lower] }\n")}
	s := packs.Merge(trusted, repo).Pack("data")
	if p := s.Strings("pipelines.paths"); len(p) != 2 {
		t.Fatalf("paths are a union: %v", p)
	}
	if m := s.Strings("pipelines.masking"); len(m) != 1 || m[0] != "acme_mask" {
		t.Fatalf("masking is trusted-layer only: %v", m)
	}
}

func TestPipelineFlag(t *testing.T) {
	var flag packs.Flag
	for _, f := range packs.Flags("data") {
		if f.Name == "pipeline" {
			flag = f
		}
	}
	if flag.Raise == nil || flag.Question == "" || len(flag.Reviewers) != 1 || flag.Reviewers[0] != "pipeline-reviewer" {
		t.Fatalf("pipeline flag: %+v", flag)
	}
	cfg := packs.Merge(map[string]manifest.PackConfig{"data": mustPack(t, "dbt: { project: analytics }\n")}, nil)
	for p, want := range map[string]bool{
		"dags/orders.py":                             true,
		"analytics/models/marts/orders.sql":          true,
		"models/orders.sql":                          false, // dbt.project is analytics
		"ws/Orders.Notebook/notebook-content.py":     true,
		"ws/Load.DataPipeline/pipeline-content.json": true,
		"synapse/linkedService/AcmeSql.json":         true,
		"src/app/main.py":                            false,
		"README.md":                                  false,
	} {
		in := &packs.Input{Config: cfg, Changed: []packs.ChangedFile{{Path: p, Status: 'M'}}}
		raised := false
		for _, f := range packs.RaiseFlags(in) {
			if f == "pipeline" {
				raised = true
			}
		}
		if raised != want {
			t.Errorf("%s: raised %v, want %v", p, raised, want)
		}
	}
	if crew := packs.Crew(cfg, []string{"pipeline"}); len(crew) != 1 || crew[0] != "pipeline-reviewer" {
		t.Fatalf("crew %v", crew)
	}
	if _, ok := packs.Questions(cfg)["pipeline"]; !ok {
		t.Fatal("no Jev question while the pack is enabled")
	}
}

func TestPipelineHints(t *testing.T) {
	isolatePipeline(t)
	root := t.TempDir()
	in := &packs.Input{Root: root, Config: packs.Merge(map[string]manifest.PackConfig{}, nil)}
	for _, c := range []struct{ path, body, why string }{
		{"dags/orders.py", "from airflow import DAG\n", "Airflow DAGs"},
		{"etl/defs.py", "import dagster\n", "Dagster code"},
		{"flows/load.py", "from prefect import flow\n", "Prefect flows"},
		{"ws/Orders.Notebook/.platform", `{"metadata": {"type": "Notebook"}}`, "Microsoft Fabric"},
		{"syn/notebook/Profile.json", `{"properties": {"nbformat": 4, "cells": []}}`, "Synapse notebooks"},
		{"syn/pipeline/Load.json", `{"properties": {"activities": []}}`, "Synapse or Data Factory pipelines"},
		{"src/app.py", "import flask\n", ""},
	} {
		writeFixture(t, root, c.path, c.body)
		got := strings.Join(packs.Suggest(in, []string{c.path}), "\n")
		if c.why == "" && got != "" || !strings.Contains(got, c.why) {
			t.Errorf("%s: suggest %q, want %q", c.path, got, c.why)
		}
	}
}
