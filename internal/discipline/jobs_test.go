package discipline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var ctx = context.Background()

func testStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	root := t.TempDir()
	state, spool := filepath.Join(root, "state"), filepath.Join(root, "spool")
	s, err := OpenStore(state, spool, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, state, spool
}

func job(t *testing.T, s *Store) Job {
	t.Helper()
	j, err := s.CreateJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func step(t *testing.T, s *Store, j Job, next Phase) Job {
	t.Helper()
	j, err := s.Transition(ctx, j, next, "")
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestDurableJobsAndRequests(t *testing.T) {
	s, state, spool := testStore(t)
	j := job(t, s)
	other := job(t, s)
	if j.ID == other.ID || j.Sequence >= other.Sequence || !uuidPattern.MatchString(j.ID) {
		t.Fatalf("attempts are not independently identified: %+v %+v", j, other)
	}
	j = step(t, s, step(t, s, j, Ripping), RipFailed)
	r, err := s.Enqueue(ctx, Request{Action: "retry", JobID: j.ID})
	if err != nil || r.Status != "queued" || r.Revision != j.Revision {
		t.Fatalf("queue: %+v %v", r, err)
	}
	if _, err := s.Enqueue(ctx, Request{Action: "retry", JobID: j.ID}); err == nil {
		t.Fatal("accepted duplicate pending request")
	}
	s.Close()
	s, err = OpenStore(state, spool, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Job(ctx, j.ID)
	if err != nil || got != j {
		t.Fatalf("lost job: %+v %v", got, err)
	}
	requests, err := s.Requests(ctx)
	if err != nil || len(requests) != 1 || requests[0] != r {
		t.Fatalf("lost request: %+v %v", requests, err)
	}
}

func TestClaimsAcrossConnections(t *testing.T) {
	s, state, spool := testStore(t)
	other, err := OpenStore(state, spool, false)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	j := job(t, s)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, store := range []*Store{s, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Transition(ctx, j, Ripping, "")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("claims: %d successes, %d conflicts", success, conflict)
	}
	for _, action := range []Request{{Action: "retry"}, {Action: "cleanup"}, {Action: "reconcile", Outcome: "abandoned", Note: "test"}} {
		action.JobID = j.ID
		if _, err := s.Enqueue(ctx, action); !errors.Is(err, ErrConflict) {
			t.Fatalf("busy worker accepted request %+v: %v", action, err)
		}
	}
}

func TestPublishedReceiptCannotBeReset(t *testing.T) {
	s, _, _ := testStore(t)
	j := job(t, s)
	for _, phase := range []Phase{Ripping, AwaitingReview, Approved, Finalizing, Ready, Copying, Published} {
		j = step(t, s, j, phase)
	}
	for _, phase := range []Phase{Queued, Ripping, Ready, Copying, Abandoned} {
		if _, err := s.Transition(ctx, j, phase, ""); err == nil {
			t.Fatalf("reset published receipt to %s", phase)
		}
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "reconcile", Outcome: "abandoned", Note: "discard"}); err == nil {
		t.Fatal("abandoned published receipt")
	}
	r, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "cleanup"})
	if err != nil || r.Status != "queued" {
		t.Fatalf("cleanup request: %+v %v", r, err)
	}
	got, _ := s.Job(ctx, j.ID)
	if got != j {
		t.Fatal("enqueuing a command changed a job")
	}
}

func TestCleanupClaimRetainsReceiptAcrossRestart(t *testing.T) {
	for _, terminal := range []Phase{Published, Abandoned} {
		t.Run(string(terminal), func(t *testing.T) {
			s, state, spool := testStore(t)
			j := job(t, s)
			if terminal == Published {
				for _, phase := range []Phase{Ripping, AwaitingReview, Approved, Finalizing, Ready, Copying} {
					j = step(t, s, j, phase)
				}
			}
			j = step(t, s, step(t, s, j, terminal), Cleaning)
			s.Close()
			s, err := OpenStore(state, spool, false)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.Job(ctx, j.ID)
			if err != nil || got.Phase != Cleaning || got.Outcome != terminal {
				t.Fatalf("lost receipt during cleanup: %+v %v", got, err)
			}
		})
	}
}

func TestUncertainCopyRequiresAuditedReconciliation(t *testing.T) {
	s, _, _ := testStore(t)
	j := job(t, s)
	for _, phase := range []Phase{Ripping, AwaitingReview, Approved, Finalizing, Ready, Copying, Uncertain} {
		j = step(t, s, j, phase)
	}
	for _, r := range []Request{
		{Action: "retry"}, {Action: "cleanup"},
		{Action: "reconcile", Outcome: "not-published"},
		{Action: "reconcile", Outcome: "published", Note: "  "},
		{Action: "reconcile", Outcome: "typo", Note: "checked"},
	} {
		r.JobID = j.ID
		if _, err := s.Enqueue(ctx, r); err == nil {
			t.Fatalf("accepted unsafe recovery: %+v", r)
		}
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "reconcile", Outcome: "not-published", Note: "publisher stopped; importer has not consumed files"}); err != nil {
		t.Fatal(err)
	}
}

func TestStateLossFailsClosed(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt", "orphan", "symlink", "incomplete", "unknown-schema"} {
		t.Run(damage, func(t *testing.T) {
			s, state, spool := testStore(t)
			if damage == "unknown-schema" {
				if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			dbPath := filepath.Join(state, "state.sqlite3")
			var err error
			switch damage {
			case "missing":
				err = os.Remove(dbPath)
			case "corrupt":
				err = os.WriteFile(dbPath, []byte("not a database"), 0600)
			case "incomplete":
				err = os.Truncate(dbPath, 0)
			case "orphan":
				err = os.Mkdir(filepath.Join(spool, "11111111-1111-4111-8111-111111111111"), 0700)
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside")
				if err := os.Rename(dbPath, target); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(target, dbPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			if reopened, err := OpenStore(state, spool, true); err == nil {
				reopened.Close()
				t.Fatalf("accepted %s state", damage)
			}
		})
	}
}

func TestNoImplicitInitialization(t *testing.T) {
	root := t.TempDir()
	state, spool := filepath.Join(root, "state"), filepath.Join(root, "spool")
	if _, err := OpenStore(state, spool, false); err == nil {
		t.Fatal("status command initialized state")
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status command created a directory")
	}
	if err := os.MkdirAll(spool, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spool, "existing.mp3"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(state, spool, true); err == nil {
		t.Fatal("initialized over existing spool data")
	}
}

func TestSeparateProcessDurability(t *testing.T) {
	if state := os.Getenv("DISCIPLINE_TEST_STATE"); state != "" {
		s, err := OpenStore(state, os.Getenv("DISCIPLINE_TEST_SPOOL"), false)
		if err != nil {
			t.Fatal(err)
		}
		j, err := s.Job(ctx, os.Getenv("DISCIPLINE_TEST_JOB"))
		if err != nil {
			t.Fatal(err)
		}
		step(t, s, j, Ripping)
		// Exit without Close to exercise WAL recovery after a committed claim.
		os.Exit(0)
	}
	s, state, spool := testStore(t)
	j := job(t, s)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSeparateProcessDurability$")
	cmd.Env = append(os.Environ(), "DISCIPLINE_TEST_STATE="+state, "DISCIPLINE_TEST_SPOOL="+spool, "DISCIPLINE_TEST_JOB="+j.ID)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil || got.Phase != Ripping || got.Revision != j.Revision+1 {
		t.Fatalf("missing committed claim: %+v %v", got, err)
	}
	if _, err := s.Transition(ctx, j, Ripping, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale process stole a claim: %v", err)
	}
}
