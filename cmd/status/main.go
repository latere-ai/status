// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

// Command status probes Latere's public endpoints and builds the status page.
//
//	status probe [-history dir]
//	status build [-history dir] [-incidents dir] [-out dir] [-now time]
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/latere-ai/status/internal/status"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: status probe|build [flags]")
	}
	switch args[0] {
	case "probe":
		return probe(args[1:], stdout)
	case "build":
		return build(args[1:], stdout)
	}
	return fmt.Errorf("unknown command %q, want probe or build", args[0])
}

// probe checks every service, appends the run to history and trims history
// to the retention window. A failing service is a recorded result, not an
// error: only a failure to record makes probe fail.
func probe(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	dir := fs.String("history", "history", "directory of per-day history files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*status.Timeout)
	defer cancel()
	run := status.Probe(ctx, status.NewClient(), status.Services, now)
	for _, r := range run.Results {
		verdict := "ok"
		if !r.OK {
			verdict = "FAIL " + r.Error
		}
		fmt.Fprintf(stdout, "%-8s %4d ms  %s\n", r.Service, r.Ms, verdict)
	}
	store := status.Store{Dir: *dir}
	if err := store.Append(run); err != nil {
		return fmt.Errorf("record run: %w", err)
	}
	gone, err := store.Trim(now, status.HistoryDays)
	if err != nil {
		return fmt.Errorf("trim history: %w", err)
	}
	if len(gone) > 0 {
		fmt.Fprintf(stdout, "trimmed %d old day files\n", len(gone))
	}
	return nil
}

// build renders the site from history and incidents.
func build(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	hist := fs.String("history", "history", "directory of per-day history files")
	inc := fs.String("incidents", "incidents", "directory of incident files")
	out := fs.String("out", "site", "output directory")
	at := fs.String("now", "", "render as of this RFC 3339 time (default: now)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	now := time.Now()
	if *at != "" {
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			return fmt.Errorf("-now: %w", err)
		}
		now = t
	}
	if err := status.Build(*hist, *inc, *out, status.Services, now); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s/index.html\n", *out)
	return nil
}
