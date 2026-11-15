// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var (
	svcs = []Service{{ID: "chat", Name: "Latere Chat"}, {ID: "api", Name: "API"}}
	t0   = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
)

// run builds a run at minute m where chat is ok or not and api is ok.
func run(m int, chatOK bool) Run {
	chat := Result{Service: "chat", OK: chatOK, Ms: 40, Code: 200}
	if !chatOK {
		chat = Result{Service: "chat", Code: 503, Ms: 120, Error: "expected status 200, got 503"}
	}
	return Run{Time: t0.Add(time.Duration(m) * 5 * time.Minute), Results: []Result{chat, {Service: "api", OK: true, Ms: 30}}}
}

func TestDecide(t *testing.T) {
	issue := OpenIssue{Number: 7, Service: "chat", Down: t0.Add(5 * time.Minute)}
	tests := []struct {
		name string
		prev []Run
		cur  Run
		open []OpenIssue
		want []ActionKind
	}{
		{"all up", []Run{run(0, true)}, run(1, true), nil, nil},
		{"first failure", []Run{run(0, true)}, run(1, false), nil, nil},
		{"no history", nil, run(0, false), nil, nil},
		{"second failure opens", []Run{run(0, true), run(1, false)}, run(2, false), nil, []ActionKind{OpenIssueAction}},
		{"still down with issue", []Run{run(1, false), run(2, false)}, run(3, false), []OpenIssue{issue}, nil},
		{"recovery closes", []Run{run(1, false), run(2, false)}, run(3, true), []OpenIssue{issue}, []ActionKind{CloseIssueAction}},
		{"blip after recovery", []Run{run(2, false), run(3, true)}, run(4, false), nil, nil},
		{"flap fail ok fail", []Run{run(0, false), run(1, true)}, run(2, false), nil, nil},
		{"each service on its own issue", []Run{run(0, false)}, run(1, false), []OpenIssue{{Number: 3, Service: "api"}}, []ActionKind{OpenIssueAction, CloseIssueAction}},
	}
	for _, tc := range tests {
		got := Decide(tc.prev, tc.cur, tc.open, svcs, "https://status.latere.ai")
		if len(got) != len(tc.want) {
			t.Errorf("%s: %d actions, want %d: %+v", tc.name, len(got), len(tc.want), got)
			continue
		}
		for i, a := range got {
			if a.Kind != tc.want[i] {
				t.Errorf("%s: action %d = %v, want %v", tc.name, i, a.Kind, tc.want[i])
			}
		}
	}
}

func TestOutageText(t *testing.T) {
	a := Decide([]Run{run(0, true), run(1, false), run(2, false)}, run(3, false), nil, svcs, "https://status.latere.ai")[0]
	if a.Title != "Latere Chat is down since 2026-11-10 12:05 UTC" {
		t.Errorf("title = %q", a.Title)
	}
	for _, s := range []string{"HTTP 503", "expected status 200, got 503", "120 ms", "https://status.latere.ai"} {
		if !strings.Contains(a.Body, s) {
			t.Errorf("body missing %q:\n%s", s, a.Body)
		}
	}
	svc, down, ok := ParseMarker(a.Body)
	if !ok || svc != "chat" || !down.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("marker = %q %v %v", svc, down, ok)
	}
}

func TestRecoveryText(t *testing.T) {
	issue := OpenIssue{Number: 7, Service: "chat", Down: t0.Add(5 * time.Minute)}
	a := Decide(nil, run(15, true), []OpenIssue{issue}, svcs, "")[0]
	want := "Latere Chat answers again as of 2026-11-10 13:15 UTC. The outage lasted 1 h 10 min."
	if a.Number != 7 || a.Comment != want {
		t.Errorf("got %d %q", a.Number, a.Comment)
	}
}

func TestReplayFlapping(t *testing.T) {
	// fail, fail (open), ok (close), fail, ok (nothing), fail, fail (open again)
	ok := []bool{true, false, false, true, false, true, false, false}
	var runs []Run
	for i, b := range ok {
		runs = append(runs, run(i, b))
	}
	got := Replay(runs, svcs, "")
	var kinds []ActionKind
	for _, g := range got {
		kinds = append(kinds, g.Kind)
	}
	want := []ActionKind{OpenIssueAction, CloseIssueAction, OpenIssueAction}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Errorf("kinds = %v", kinds)
	}
}

type fakeAPI struct {
	issues []map[string]any
	calls  []string
	fail   string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	call := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, call)
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no", 401)
		return
	}
	if f.fail != "" && strings.HasPrefix(call, f.fail) {
		http.Error(w, "boom", 500)
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(f.issues)
		return
	}
	b, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, string(b))
	_, _ = w.Write([]byte("{}"))
}

func TestNotifyOpensThenCloses(t *testing.T) {
	f := &fakeAPI{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	gh := GitHub{HTTP: srv.Client(), Base: srv.URL, Repo: "o/r", Token: "tok"}
	runs := []Run{run(0, false), run(1, false)}
	acts, err := Notify(context.Background(), gh, runs, svcs, "https://status.latere.ai")
	if err != nil || len(acts) != 1 {
		t.Fatalf("acts=%v err=%v", acts, err)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), `POST /repos/o/r/issues`) || !strings.Contains(strings.Join(f.calls, "\n"), `"labels":["outage"]`) {
		t.Errorf("calls = %v", f.calls)
	}

	// An issue is now open; a person's unmarked issue and a PR are ignored.
	f.calls = nil
	f.issues = []map[string]any{
		{"number": 9, "body": acts[0].Body},
		{"number": 10, "body": "mine"},
		{"number": 11, "body": acts[0].Body, "pull_request": map[string]any{}},
	}
	acts, err = Notify(context.Background(), gh, []Run{run(1, false), run(2, true)}, svcs, "")
	if err != nil || len(acts) != 1 || acts[0].Number != 9 {
		t.Fatalf("acts=%v err=%v", acts, err)
	}
	joined := strings.Join(f.calls, "\n")
	if !strings.Contains(joined, "POST /repos/o/r/issues/9/comments") || !strings.Contains(joined, "PATCH /repos/o/r/issues/9") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestNotifyErrors(t *testing.T) {
	f := &fakeAPI{fail: "POST"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	gh := GitHub{HTTP: srv.Client(), Base: srv.URL, Repo: "o/r", Token: "tok"}
	if _, err := Notify(context.Background(), gh, []Run{run(0, false), run(1, false)}, svcs, ""); err == nil {
		t.Error("failed open reported no error")
	}
	bad := GitHub{HTTP: srv.Client(), Base: srv.URL, Repo: "o/r", Token: "wrong"}
	if _, err := Notify(context.Background(), bad, []Run{run(0, true)}, svcs, ""); err == nil {
		t.Error("failed list reported no error")
	}
}

func TestLength(t *testing.T) {
	for d, want := range map[time.Duration]string{20 * time.Second: "under a minute", 10 * time.Minute: "10 min", 125 * time.Minute: "2 h 5 min"} {
		if got := Length(d); got != want {
			t.Errorf("Length(%v) = %q, want %q", d, got, want)
		}
	}
}
