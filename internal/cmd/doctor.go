package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/lobe/health"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

var exitFunc = os.Exit

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the shell hook, git, the clients' folders and their CLIs (never changes anything)",
		RunE: func(c *cobra.Command, _ []string) error {
			return runDoctor(c, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func runDoctor(c *cobra.Command, asJSON bool) error {
	cfg, err := registry.Load()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	fs := doctor.Run(cfg, doctor.Deps{
		Env: os.LookupEnv, Cwd: cwd,
		RunGit: gitcfg.RunGit, RunGitIn: gitcfg.RunGitIn, LookPath: exec.LookPath,
	})
	fs = withLobe(fs, lobeFindings(cfg, time.Now()))
	if asJSON {
		if err := json.NewEncoder(c.OutOrStdout()).Encode(fs); err != nil {
			return err
		}
	} else {
		for _, f := range fs {
			line := fmt.Sprintf("[%s] %s", f.Level, f.Msg)
			if f.Workspace != "" {
				line = fmt.Sprintf("[%s] %s: %s", f.Level, f.Workspace, f.Msg)
			}
			if f.Fix != "" {
				line += "  → " + f.Fix
			}
			fmt.Fprintln(c.OutOrStdout(), line)
		}
	}
	if doctor.Exit(fs) != 0 {
		exitFunc(1)
	}
	return nil
}

// lobeFindings are the lobe's findings for every client, as doctor findings with the client's name. The
// plugin of each client is found through its Claude config dir (guardplugin, which health may not import).
func lobeFindings(cfg *registry.Config, now time.Time) []doctor.Finding {
	wss, _ := resolve.ListWorkspaces(cfg)
	clients := make([]health.Client, 0, len(wss))
	for i := range wss {
		ws := &wss[i]
		data, found := pluginHooks(paths.IdentityDir(ws.Name, "claude"))
		clients = append(clients, health.Client{Name: ws.Name, WS: ws, PluginFound: found, HooksJSON: data})
	}
	var out []doctor.Finding
	for _, f := range health.Findings(clients, now) {
		out = append(out, doctor.Finding{Level: f.Level, Code: f.Code, Workspace: f.Client, Msg: f.Msg, Fix: f.Fix})
	}
	return out
}

// withLobe appends the lobe's findings to doctor's list. doctor ends an all-clear list with its "all checks
// passed" line; that line is dropped when there are lobe findings, since they are not ok lines.
func withLobe(fs, extra []doctor.Finding) []doctor.Finding {
	if len(extra) == 0 {
		return fs
	}
	var out []doctor.Finding
	for _, f := range fs {
		if f.Level == "ok" && f.Code == "ok" {
			continue
		}
		out = append(out, f)
	}
	return append(out, extra...)
}
