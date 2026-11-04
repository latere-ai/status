// SPDX-FileCopyrightText: 2026 Latere AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommand(t *testing.T) {
	out := t.TempDir()
	var stdout bytes.Buffer
	err := run([]string{"build", "-history", "../../testdata/history", "-incidents", "../../testdata/incidents",
		"-out", out, "-now", "2026-11-04T12:00:00Z"}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil || !strings.Contains(string(b), "Latere Status") {
		t.Errorf("index.html: %v", err)
	}
	if !strings.Contains(stdout.String(), "index.html") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"build", "-now", "yesterday"}, {"probe", "-bogus"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("run(%v) succeeded", args)
		}
	}
}
