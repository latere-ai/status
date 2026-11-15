// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// OutageLabel marks the issues the tool opens, one per service outage.
const OutageLabel = "outage"

// OpenIssue is an open outage issue, as read back from the tracker.
type OpenIssue struct {
	Number  int
	Service string
	// Down is when the outage began, read from the issue body. Zero when
	// the body carries no marker time.
	Down time.Time
}

// ActionKind is what to do about one service.
type ActionKind int

const (
	// OpenIssueAction opens an issue for a new outage.
	OpenIssueAction ActionKind = iota
	// CloseIssueAction comments the recovery on an issue and closes it.
	CloseIssueAction
)

func (k ActionKind) String() string {
	if k == CloseIssueAction {
		return "close"
	}
	return "open"
}

// Action is one thing to do on the issue tracker.
type Action struct {
	Kind    ActionKind
	Service string
	// Number is the issue to close. Zero for an open.
	Number int
	// Title and Body fill a new issue.
	Title string
	Body  string
	// Comment is posted before the issue is closed.
	Comment string
}

// marker is the hidden line in an issue body that ties the issue to a
// service and records when the outage began.
var marker = regexp.MustCompile(`<!-- status-outage service=(\S+) down=(\S*) -->`)

// ParseMarker reads the service and down time from an issue body. ok is
// false when the body has no marker, so a person's own issue with the
// label is left alone.
func ParseMarker(body string) (service string, down time.Time, ok bool) {
	m := marker.FindStringSubmatch(body)
	if m == nil {
		return "", time.Time{}, false
	}
	down, _ = time.Parse(time.RFC3339, m[2])
	return m[1], down, true
}

// Decide returns the issue actions for the newest run cur. prev is every
// earlier run, oldest first, and open is the open outage issues.
//
// A service that failed in both the previous run and cur is down, and gets
// one issue if it has none open. A lone failure is a blip and does nothing.
// A service with an open issue whose check passes in cur gets the issue
// closed with a recovery comment. A slow answer that passes is not an
// outage. A service missing from a run has no result and counts as neither
// failed nor passed.
func Decide(prev []Run, cur Run, open []OpenIssue, services []Service, pageURL string) []Action {
	openBy := map[string]OpenIssue{}
	for _, o := range open {
		if _, dup := openBy[o.Service]; !dup {
			openBy[o.Service] = o
		}
	}
	var acts []Action
	for _, s := range services {
		r, ok := resultOf(cur, s.ID)
		if !ok {
			continue
		}
		issue, hasIssue := openBy[s.ID]
		switch {
		case r.OK && hasIssue:
			acts = append(acts, recovery(s, issue, cur.Time))
		case !r.OK && !hasIssue && len(prev) > 0:
			if p, ok := resultOf(prev[len(prev)-1], s.ID); ok && !p.OK {
				acts = append(acts, outage(s, downSince(prev, s.ID), r, pageURL))
			}
		}
	}
	return acts
}

func resultOf(run Run, id string) (Result, bool) {
	for _, r := range run.Results {
		if r.Service == id {
			return r, true
		}
	}
	return Result{}, false
}

// downSince is the time of the oldest failure in the unbroken streak of
// failures that ends at the last run of prev.
func downSince(prev []Run, id string) time.Time {
	since := time.Time{}
	for i := len(prev) - 1; i >= 0; i-- {
		r, ok := resultOf(prev[i], id)
		if !ok || r.OK {
			break
		}
		since = prev[i].Time
	}
	return since
}

const timeFormat = "2006-01-02 15:04"

func outage(s Service, down time.Time, r Result, pageURL string) Action {
	reason := r.Error
	if reason == "" {
		reason = "check failed"
	}
	code := "no response"
	if r.Code != 0 {
		code = "HTTP " + strconv.Itoa(r.Code)
	}
	return Action{
		Kind:    OpenIssueAction,
		Service: s.ID,
		Title:   fmt.Sprintf("%s is down since %s UTC", s.Name, down.UTC().Format(timeFormat)),
		Body: fmt.Sprintf("%s failed two checks in a row. The first failure was at %s UTC.\n\n"+
			"Latest check: %s, %s, %d ms.\n\nStatus page: %s\n\n"+
			"This issue closes itself when the service answers again.\n\n"+
			"<!-- status-outage service=%s down=%s -->\n",
			s.Name, down.UTC().Format(timeFormat), code, reason, r.Ms, pageURL, s.ID, down.UTC().Format(time.RFC3339)),
	}
}

func recovery(s Service, o OpenIssue, at time.Time) Action {
	c := fmt.Sprintf("%s answers again as of %s UTC.", s.Name, at.UTC().Format(timeFormat))
	if !o.Down.IsZero() {
		c = fmt.Sprintf("%s answers again as of %s UTC. The outage lasted %s.",
			s.Name, at.UTC().Format(timeFormat), Length(at.Sub(o.Down)))
	}
	return Action{Kind: CloseIssueAction, Service: s.ID, Number: o.Number, Comment: c}
}

// Length writes d in whole minutes, or hours and minutes past an hour.
func Length(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	switch {
	case m < 1:
		return "under a minute"
	case m < 60:
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}
