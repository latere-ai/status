// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Tracker is the issue tracker the notifier talks to.
type Tracker interface {
	// OpenOutages lists the open issues with OutageLabel.
	OpenOutages(ctx context.Context) ([]OpenIssue, error)
	Open(ctx context.Context, title, body string) error
	Comment(ctx context.Context, number int, body string) error
	Close(ctx context.Context, number int) error
}

// GitHub is a Tracker over the GitHub REST API. HTTP is injectable for tests.
type GitHub struct {
	HTTP  *http.Client
	Base  string // API root, https://api.github.com when empty
	Repo  string // owner/name
	Token string
}

func (g GitHub) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	base := g.Base
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/repos/"+g.Repo+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "latere-status/1 (+https://status.latere.ai)")
	c := g.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("github %s %s: %s: %.200s", method, path, resp.Status, b)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (g GitHub) OpenOutages(ctx context.Context) ([]OpenIssue, error) {
	var out []OpenIssue
	for page := 1; ; page++ {
		var items []struct {
			Number      int             `json:"number"`
			Body        string          `json:"body"`
			PullRequest json.RawMessage `json:"pull_request"`
		}
		q := url.Values{"labels": {OutageLabel}, "state": {"open"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := g.do(ctx, http.MethodGet, "/issues?"+q.Encode(), nil, &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.PullRequest != nil {
				continue
			}
			if svc, down, ok := ParseMarker(it.Body); ok {
				out = append(out, OpenIssue{Number: it.Number, Service: svc, Down: down})
			}
		}
		if len(items) < 100 {
			return out, nil
		}
	}
}

func (g GitHub) Open(ctx context.Context, title, body string) error {
	return g.do(ctx, http.MethodPost, "/issues", map[string]any{
		"title": title, "body": body, "labels": []string{OutageLabel},
	}, nil)
}

func (g GitHub) Comment(ctx context.Context, n int, body string) error {
	return g.do(ctx, http.MethodPost, "/issues/"+strconv.Itoa(n)+"/comments", map[string]string{"body": body}, nil)
}

func (g GitHub) Close(ctx context.Context, n int) error {
	return g.do(ctx, http.MethodPatch, "/issues/"+strconv.Itoa(n),
		map[string]string{"state": "closed", "state_reason": "completed"}, nil)
}

// Notify decides on the newest run in runs and applies the actions to t.
// It tries every action and returns all the errors together, so one failed
// call does not hide the others.
func Notify(ctx context.Context, t Tracker, runs []Run, services []Service, pageURL string) ([]Action, error) {
	if len(runs) == 0 {
		return nil, nil
	}
	open, err := t.OpenOutages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open outages: %w", err)
	}
	acts := Decide(runs[:len(runs)-1], runs[len(runs)-1], open, services, pageURL)
	var errs []error
	for _, a := range acts {
		switch a.Kind {
		case OpenIssueAction:
			errs = append(errs, t.Open(ctx, a.Title, a.Body))
		case CloseIssueAction:
			// Close only after the comment is in, so a failed comment is retried next run.
			if err := t.Comment(ctx, a.Number, a.Comment); err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, t.Close(ctx, a.Number))
		}
	}
	return acts, errors.Join(errs...)
}

// Replay runs Decide over every run in runs as if the notifier had run
// after each one, against a simulated tracker. It touches no network. The
// report is one entry per action with the run time it fired at.
func Replay(runs []Run, services []Service, pageURL string) []Replayed {
	var open []OpenIssue
	var out []Replayed
	next := 1
	for i, cur := range runs {
		for _, a := range Decide(runs[:i], cur, open, services, pageURL) {
			out = append(out, Replayed{At: cur.Time, Action: a})
			switch a.Kind {
			case OpenIssueAction:
				_, down, _ := ParseMarker(a.Body)
				open = append(open, OpenIssue{Number: next, Service: a.Service, Down: down})
				next++
			case CloseIssueAction:
				for j, o := range open {
					if o.Number == a.Number {
						open = append(open[:j], open[j+1:]...)
						break
					}
				}
			}
		}
	}
	return out
}

// Replayed is an action and the run it would have fired at.
type Replayed struct {
	At time.Time
	Action
}
