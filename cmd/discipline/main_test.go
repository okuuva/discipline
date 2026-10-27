//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/okuuva/discipline/internal/discipline"
)

func TestCommandsOnlyQueueRecovery(t *testing.T) {
	root := t.TempDir()
	state, spool := filepath.Join(root, "state"), filepath.Join(root, "spool")
	store, err := discipline.OpenStore(state, spool, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	j, err := store.CreateJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	runCommand := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		return run(ctx, append([]string{"--state", state, "--spool", spool}, args...), &out, &stderr)
	}
	if err := runCommand("jobs"); err != nil {
		t.Fatal(err)
	}
	var jobs []discipline.Job
	if err := json.Unmarshal(out.Bytes(), &jobs); err != nil || len(jobs) != 1 || jobs[0].ID != j.ID {
		t.Fatalf("jobs: %s %v", &out, err)
	}
	if err := runCommand("show", j.ID); err != nil {
		t.Fatal(err)
	}
	var view struct {
		Job       discipline.Job `json:"job"`
		LocalPath string         `json:"local_path"`
	}
	if err := json.Unmarshal(out.Bytes(), &view); err != nil || view.Job.ID != j.ID || view.LocalPath != filepath.Join(spool, j.ID) {
		t.Fatalf("show: %s %v", &out, err)
	}
	if err := runCommand("reconcile", j.ID, "--outcome", "abandoned", "--note", "unwanted disc"); err == nil {
		t.Fatal("reconciled without confirmation")
	}
	if err := runCommand("reconcile", j.ID, "--outcome", "abandoned", "--note", "unwanted disc", "--confirm"); err != nil {
		t.Fatal(err)
	}
	var request discipline.Request
	if err := json.Unmarshal(out.Bytes(), &request); err != nil || request.Status != "queued" {
		t.Fatalf("request: %s %v", &out, err)
	}
	got, err := store.Job(ctx, j.ID)
	if err != nil || got.Phase != discipline.Queued {
		t.Fatalf("CLI applied a queued request: %+v %v", got, err)
	}
	if err := runCommand("requests"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"show"}, {"jobs", "extra"}, {"wat"}, {"new-insertion", j.ID}, {"show", "../../etc/passwd"}, {"--telegram-user", "0", "jobs"}, {"--telegram-user", "invalid", "jobs"}} {
		if err := runCommand(args...); err == nil {
			t.Fatalf("accepted invalid command: %v", args)
		}
	}
}

func TestHelpAndMissingState(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "missing")
	if err := run(context.Background(), []string{"--state", dir, "jobs"}, &out, &out); err == nil {
		t.Fatal("missing state accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("status created state")
	}
}
