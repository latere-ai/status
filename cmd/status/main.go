// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

// Command status probes Latere's public endpoints and builds the status page.
//
//	status probe [-history dir]
//	status notify [-history dir] [-repo owner/name] [-replay duration]
//	status build [-history dir] [-incidents dir] [-out dir] [-now time]
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
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
		return fmt.Errorf("usage: status probe|build|notify [flags]")
	}
	switch args[0] {
	case "probe":
		return probe(args[1:], stdout)
	case "build":
		return build(args[1:], stdout)
	case "notify":
		return notify(args[1:], stdout)
	}
	return fmt.Errorf("unknown command %q, want probe, build or notify", args[0])
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

// pageURL is where an outage issue points readers.
const pageURL = "https://status.latere.ai"

// notify opens an issue when a service has failed two probes in a row and
// closes it when the service answers again. With -replay it instead walks
// the history of the given duration against a simulated tracker and prints
// what it would have done, touching no network.
func notify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	dir := fs.String("history", "history", "directory of per-day history files")
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "owner/name of the repository whose issues to use")
	replay := fs.Duration("replay", 0, "print what would have happened over this much history, without any API calls")
	if err := fs.Parse(args); err != nil {
		return err
	}
	now := time.Now()
	days, err := status.Store{Dir: *dir}.Load(now, status.HistoryDays)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}
	var runs []status.Run
	for _, d := range days {
		runs = append(runs, d.Runs...)
	}
	if *replay > 0 {
		from := 0
		for i, r := range runs {
			if r.Time.Before(now.Add(-*replay)) {
				from = i + 1
			}
		}
		return printReplay(stdout, runs, from)
	}
	token := os.Getenv("GITHUB_TOKEN")
	if *repo == "" || token == "" {
		return fmt.Errorf("notify needs -repo (or GITHUB_REPOSITORY) and GITHUB_TOKEN")
	}
	gh := status.GitHub{HTTP: &http.Client{Timeout: 30 * time.Second}, Repo: *repo, Token: token}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	acts, err := status.Notify(ctx, gh, runs, status.Services, pageURL)
	for _, a := range acts {
		fmt.Fprintf(stdout, "%s issue for %s\n", a.Kind, a.Service)
	}
	if len(acts) == 0 {
		fmt.Fprintln(stdout, "no issue changes")
	}
	return err
}

// printReplay simulates the notifier over every run, so that an outage that
// began before from still has its issue, and prints the actions that fire
// at runs[from:].
func printReplay(w io.Writer, runs []status.Run, from int) error {
	n := 0
	for _, r := range status.Replay(runs, status.Services, pageURL) {
		if from < len(runs) && r.At.Before(runs[from].Time) {
			continue
		}
		n++
		line := r.Title
		if r.Kind == status.CloseIssueAction {
			line = r.Comment
		}
		fmt.Fprintf(w, "%s  %-5s %s\n", r.At.Format(time.RFC3339), r.Kind, line)
	}
	if from < len(runs) {
		fmt.Fprintf(w, "%d runs from %s to %s, %d actions\n", len(runs)-from,
			runs[from].Time.Format(time.RFC3339), runs[len(runs)-1].Time.Format(time.RFC3339), n)
	}
	return nil
}
