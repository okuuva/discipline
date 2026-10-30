package discipline

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func readyPublication(t *testing.T, s *Store, spool string, disc Disc) Job {
	t.Helper()
	j := job(t, s)
	key, toc, err := disc.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE jobs SET disc_key=?,toc=? WHERE id=?", key, toc, j.ID); err != nil {
		t.Fatal(err)
	}
	j, err = s.Job(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	j = step(t, s, step(t, s, j, Ripping), AwaitingReview)
	if _, err := s.db.Exec("INSERT INTO reviews(job_id) VALUES(?)", j.ID); err != nil {
		t.Fatal(err)
	}
	r := fakeCandidate(t, s, j)
	if yes, err := s.approveCover(context.Background(), j.ID, r.Revision, testUpdate(j.Sequence), "Owned"); err != nil || !yes {
		t.Fatal(yes, err)
	}
	j, err = s.claimFinalization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixtureMedia(t, filepath.Join(spool, j.ID, "media"), "Album")
	if err := s.finishFinalization(context.Background(), j, manifest, nil); err != nil {
		t.Fatal(err)
	}
	j, err = s.Job(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestNewestCreatedApprovalOwnsDestination(t *testing.T) {
	s, _, spool := testStore(t)
	disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
	old := readyPublication(t, s, spool, disc)
	newer := readyPublication(t, s, spool, disc)
	claim, err := s.claimPublication(context.Background())
	if err != nil || claim.ID != old.ID || claim.Phase != Superseded {
		t.Fatal("older approval was allowed to overwrite newer rip", claim, err)
	}
	claim, err = s.claimPublication(context.Background())
	if err != nil || claim.ID != newer.ID || claim.Phase != Copying {
		t.Fatal("newer attempt not claimed", claim, err)
	}
	// Even after cleanup or abandonment of a newer attempt its approval fence remains.
	if _, err := s.db.Exec("UPDATE jobs SET phase='ready' WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	claim, err = s.claimPublication(context.Background())
	if err != nil || claim.Phase != Superseded {
		t.Fatal("approval history was lost", claim, err)
	}
}

func TestCopyRecoveryRetryAndCleanupExclusion(t *testing.T) {
	s, state, spool := testStore(t)
	ctx := context.Background()
	j := readyPublication(t, s, spool, Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450})
	j, err := s.claimPublication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Request{{Action: "retry"}, {Action: "cleanup"}, {Action: "reconcile", Outcome: "abandoned", Note: "test"}} {
		r.JobID = j.ID
		if _, err := s.Enqueue(ctx, r); !errors.Is(err, ErrConflict) {
			t.Fatal("active copy accepted competing request", r, err)
		}
	}
	if err := s.finishPublication(ctx, j, errors.New("copy ended with partial inbox")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "retry"}); err != nil {
		t.Fatal(err)
	}
	if err := s.publisherRequests(ctx); err != nil {
		t.Fatal(err)
	}
	j, err = s.claimPublication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Model a crash after the bytes were verified but before the local receipt commit.
	s.Close()
	s, err = OpenStore(state, spool, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.recoverPublisher(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil || got.Phase != Uncertain {
		t.Fatal(got, err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "retry"}); err == nil {
		t.Fatal("uncertain outcome was automatically retried")
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "reconcile", Outcome: "published", Note: "publisher/importer quiesced; verified complete destination"}); err != nil {
		t.Fatal(err)
	}
	if err := s.publisherRequests(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimCleanup(ctx, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cleanup preceded transfer notification", err)
	}
	if _, err := s.db.Exec("UPDATE publications SET notified_phase='published' WHERE job_id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimCleanup(ctx, true); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("keep mode discarded local audio", err)
	}
	cleaned, err := s.claimCleanup(ctx, false)
	if err != nil || cleaned.ID != j.ID {
		t.Fatal(cleaned, err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "cleanup"}); err == nil {
		t.Fatal("shared cleanup ownership")
	}
	if err := s.finishCleanup(ctx, cleaned, errors.New("fixture filesystem failure")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimCleanup(ctx, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed cleanup busy-looped", err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "cleanup"}); err != nil {
		t.Fatal(err)
	}
	if err := s.publisherRequests(ctx); err != nil {
		t.Fatal(err)
	}
	cleaned, err = s.claimCleanup(ctx, true)
	if err != nil {
		t.Fatal("explicit cleanup ignored in keep mode", err)
	}
	if err := cleanupAttempt(spool, cleaned); err != nil {
		t.Fatal(err)
	}
	if err := s.finishCleanup(ctx, cleaned, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimPublication(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("completed attempt was resent", err)
	}
}

func TestAbandonRequestCannotStealWorkerClaim(t *testing.T) {
	s, _, spool := testStore(t)
	j := readyPublication(t, s, spool, Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450})
	if _, err := s.Enqueue(context.Background(), Request{JobID: j.ID, Action: "reconcile", Outcome: "abandoned", Note: "unwanted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimPublication(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.publisherRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests, err := s.Requests(context.Background())
	if err != nil || len(requests) != 1 || requests[0].Status != "rejected" {
		t.Fatal("queued abandonment stole publisher files", requests, err)
	}
}
