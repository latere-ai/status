// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

var page = template.Must(template.ParseFS(webFS, "web/index.html.tmpl"))

type barView struct{ Class, Tip string }

type serviceView struct {
	Name, Class, Label, Uptime string
	Bars                       []barView
}

type updateView struct{ Status, Time, ISO, Text string }

type incidentView struct {
	Title, Started, StartedISO, Resolved, ResolvedISO, Services string
	Open                                                        bool
	Paragraphs                                                  []string
	Updates                                                     []updateView
}

type pageView struct {
	Headline, Class             string
	Stale                       bool
	LastChecked, LastCheckedISO string
	Services                    []serviceView
	Open, Past                  []incidentView
}

const (
	stampLayout = "Jan 2, 2006 15:04 UTC"
	dateLayout  = "Jan 2, 2006"
)

func stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Render writes the status page for the given inputs. It is a pure function
// of its arguments: now is passed in, never read.
func Render(w io.Writer, services []Service, days []Day, incidents []Incident, now time.Time) error {
	var statuses []ServiceStatus
	for _, s := range services {
		statuses = append(statuses, Summarize(days, s, now))
	}
	v := pageView{}
	overall := Overall(statuses)
	v.Class = overall.String()
	switch overall {
	case Up:
		v.Headline = "All services are running"
	case Degraded:
		v.Headline = "Some services are degraded"
	case Down:
		v.Headline = "Some services are down"
		if allDown(statuses) {
			v.Headline = "All services are down"
		}
	default:
		v.Headline = "Waiting for the first check"
	}
	if last := LastChecked(days); !last.IsZero() {
		v.LastChecked, v.LastCheckedISO = stamp(last), iso(last)
		v.Stale = now.Sub(last) > StaleAfter
	}
	for _, st := range statuses {
		sv := serviceView{Name: st.Service.Name, Class: st.Current.String(), Label: label(st.Current)}
		if st.Checks > 0 {
			sv.Uptime = Percent(st.Uptime) + " uptime"
		}
		for _, b := range st.Bars {
			sv.Bars = append(sv.Bars, barView{Class: b.State.String(), Tip: tip(b)})
		}
		v.Services = append(v.Services, sv)
	}
	open, past := SplitIncidents(incidents, now)
	for _, i := range open {
		v.Open = append(v.Open, incidentOf(i, services))
	}
	for _, i := range past {
		v.Past = append(v.Past, incidentOf(i, services))
	}
	return page.Execute(w, v)
}

func allDown(ss []ServiceStatus) bool {
	for _, s := range ss {
		if s.Current != Down {
			return false
		}
	}
	return true
}

func label(s State) string {
	switch s {
	case Up:
		return "Running"
	case Degraded:
		return "Degraded"
	case Down:
		return "Down"
	}
	return "No data"
}

func tip(b Bar) string {
	d, err := time.Parse(dayLayout, b.Date)
	date := b.Date
	if err == nil {
		date = d.Format(dateLayout)
	}
	if b.Runs == 0 {
		return date + ": no data"
	}
	return fmt.Sprintf("%s: %s uptime", date, Percent(b.Uptime))
}

func incidentOf(i Incident, all []Service) incidentView {
	v := incidentView{
		Title: i.Title, Open: i.Open(),
		Started: stamp(i.Started), StartedISO: iso(i.Started),
	}
	if !i.Open() {
		v.Resolved, v.ResolvedISO = stamp(i.Resolved), iso(i.Resolved)
	}
	var names []string
	for _, id := range i.Services {
		for _, s := range all {
			if s.ID == id {
				names = append(names, s.Name)
			}
		}
	}
	v.Services = strings.Join(names, ", ")
	for p := range strings.SplitSeq(i.Summary, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			v.Paragraphs = append(v.Paragraphs, p)
		}
	}
	for _, u := range i.Updates {
		v.Updates = append(v.Updates, updateView{Status: u.Status, Time: stamp(u.Time), ISO: iso(u.Time), Text: strings.TrimSpace(u.Text)})
	}
	return v
}

// Build renders the site into out: index.html, style.css and the fonts. It
// overwrites those files and leaves anything else in out, such as CNAME.
func Build(historyDir, incidentsDir, out string, services []Service, now time.Time) error {
	days, err := Store{Dir: historyDir}.Load(now, HistoryDays)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}
	incidents, err := LoadIncidents(incidentsDir, services)
	if err != nil {
		return fmt.Errorf("load incidents: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(out, "index.html"))
	if err != nil {
		return err
	}
	if err := Render(f, services, days, incidents, now); err != nil {
		f.Close()
		return fmt.Errorf("render: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return copyAssets(out)
}

func copyAssets(out string) error {
	return fs.WalkDir(webFS, "web", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "web" || strings.HasSuffix(p, ".tmpl") {
			return err
		}
		dst := filepath.Join(out, strings.TrimPrefix(p, "web/"))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := webFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}
