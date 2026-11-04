// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

// Package status probes Latere's public endpoints from outside its
// infrastructure, keeps the results as per-day history, and renders the
// public status page.
package status

import "time"

// Service is one public endpoint the page reports on.
type Service struct {
	// ID is the stable key stored in history. Never reuse or rename one:
	// history under the old key would no longer match.
	ID string
	// Name is what the page shows.
	Name string
	// URL is fetched with GET and no credentials. Redirects are not
	// followed, so the URL must answer for itself.
	URL string
	// Expect is the status code a healthy answer carries.
	Expect int
	// Contains, when set, must appear in the first 64 KiB of the body.
	Contains string
}

// Thresholds and windows. Each is stated once here and used everywhere.
const (
	// Timeout bounds one check.
	Timeout = 10 * time.Second
	// SlowAfter is the latency above which an otherwise healthy check is
	// slow. A service is degraded after two slow checks in a row.
	SlowAfter = 3 * time.Second
	// ConfirmAfter is how many checks in a row must be bad before a
	// service is reported as down or degraded.
	ConfirmAfter = 2
	// HistoryDays is how many days of history are kept and shown.
	HistoryDays = 90
	// StaleAfter is the age of the newest check past which the page says
	// checks are delayed. GitHub may run a schedule late.
	StaleAfter = 30 * time.Minute
)

// Services lists what is probed, in the order the page shows it. To add
// one, add a line here. Only use endpoints that answer without credentials.
var Services = []Service{
	{ID: "website", Name: "Website", URL: "https://latere.ai/", Expect: 200},
	{ID: "chat", Name: "Latere Chat", URL: "https://chat.latere.ai/readyz", Expect: 200, Contains: "ok"},
	{ID: "console", Name: "Platform console", URL: "https://platform.latere.ai/healthz", Expect: 200, Contains: "ok"},
	{ID: "api", Name: "API", URL: "https://api.latere.ai/readyz", Expect: 200, Contains: "ok"},
	{ID: "auth", Name: "Sign-in", URL: "https://auth.latere.ai/.well-known/openid-configuration", Expect: 200, Contains: "\"issuer\""},
	{ID: "repos", Name: "Repos", URL: "https://code.latere.ai/readyz", Expect: 200, Contains: "ok"},
	{ID: "apps", Name: "Apps", URL: "https://latere.site/", Expect: 200},
}
