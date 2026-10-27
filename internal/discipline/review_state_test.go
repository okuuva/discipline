package discipline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testUpdate(id int64) updateReceipt {
	if id == 0 {
		return updateReceipt{}
	}
	return updateReceipt{ID: id, Key: fmt.Sprintf("test:%d", id)}
}

func pendingReview(t *testing.T, s *Store) Job {
	t.Helper()
	j := step(t, s, step(t, s, job(t, s), Ripping), AwaitingReview)
	if _, err := s.db.Exec("INSERT INTO reviews(job_id) VALUES(?)", j.ID); err != nil {
		t.Fatal(err)
	}
	return j
}

func fakeCandidate(t *testing.T, s *Store, j Job) Review {
	t.Helper()
	claimed, err := s.claimCover(context.Background(), j.ID, 0, testUpdate(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishCover(context.Background(), claimed, testUpdate(0), "review/cover-"+j.ID+".jpg", strings.Repeat("a", 64), "https://example.com/cover", "Fixture album", ""); err != nil {
		t.Fatal(err)
	}
	r, err := s.Review(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReviewClaimsAndAtomicUpdateCursor(t *testing.T) {
	s, state, spool := testStore(t)
	ctx := context.Background()
	j := pendingReview(t, s)
	old := fakeCandidate(t, s, j)
	claimed, err := s.claimCover(ctx, j.ID, old.Revision, testUpdate(77))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimCover(ctx, j.ID, old.Revision, testUpdate(78)); !errors.Is(err, ErrConflict) {
		t.Fatalf("shared cover ownership: %v", err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: j.ID, Action: "reconcile", Outcome: "abandoned", Note: "test"}); !errors.Is(err, ErrConflict) {
		t.Fatal("abandoned an active cover worker")
	}
	if _, err := s.db.Exec("CREATE TRIGGER fail_update BEFORE INSERT ON telegram_updates WHEN NEW.id=77 BEGIN SELECT RAISE(ABORT,'fixture commit failure'); END"); err != nil {
		t.Fatal(err)
	}
	finish := func() error {
		return s.finishCover(ctx, claimed, testUpdate(77), "review/cover-"+j.ID+".jpg", strings.Repeat("b", 64), "", "", "")
	}
	if err := finish(); err == nil {
		t.Fatal("injected update commit did not fail")
	}
	got, err := s.Review(ctx, j.ID)
	if err != nil || got.Revision != old.Revision || got.Digest != old.Digest {
		t.Fatal("effect escaped a failed update transaction", got, err)
	}
	var offset int64
	if err := s.db.QueryRow("SELECT offset FROM telegram_cursor").Scan(&offset); err != nil || offset != 0 {
		t.Fatal("offset advanced without its effect", offset, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_update"); err != nil {
		t.Fatal(err)
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimCover(ctx, j.ID, old.Revision+1, testUpdate(77)); !errors.Is(err, errUpdateApplied) {
		t.Fatalf("duplicate replacement accepted: %v", err)
	}
	if approved, err := s.approveCover(ctx, j.ID, old.Revision, testUpdate(78), "Owned"); err != nil || approved {
		t.Fatal("stale image approved", err)
	}
	if approved, err := s.approveCover(ctx, j.ID, old.Revision+1, testUpdate(79), "Loaned"); err != nil || !approved {
		t.Fatal("fresh image not approved", err)
	}
	if _, err := s.claimCover(ctx, j.ID, old.Revision+1, testUpdate(80)); !errors.Is(err, ErrConflict) {
		t.Fatal("edited a frozen approval", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(state, spool, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	r, err := reopened.Review(ctx, j.ID)
	if err != nil || r.Category != "Loaned" || r.Revision != old.Revision+1 {
		t.Fatal("lost approval after restart", r, err)
	}
	if err := reopened.db.QueryRow("SELECT offset FROM telegram_cursor").Scan(&offset); err != nil || offset != 80 {
		t.Fatal("lost durable polling offset", offset, err)
	}
	if approved, err := reopened.approveCover(ctx, j.ID, r.Revision, testUpdate(79), "Owned"); err != nil || approved {
		t.Fatal("replayed approval after restart", err)
	}
}

func TestApprovalRequiresAnImageAndSingleUpdateEffect(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	first := pendingReview(t, s)
	second := pendingReview(t, s)
	if yes, err := s.approveCover(ctx, first.ID, 0, testUpdate(1), "Owned"); err != nil || yes {
		t.Fatal("approved missing artwork", err)
	}
	a := fakeCandidate(t, s, first)
	b := fakeCandidate(t, s, second)
	if yes, err := s.approveCover(ctx, first.ID, a.Revision, testUpdate(2), "Owned"); err != nil || !yes {
		t.Fatal(err)
	}
	if yes, err := s.approveCover(ctx, second.ID, b.Revision, testUpdate(2), "Loaned"); err != nil || yes {
		t.Fatal("one update changed two jobs", err)
	}
	unchanged, err := s.Review(ctx, second.ID)
	if err != nil || unchanged.Category != "" {
		t.Fatal("second job acquired a category", unchanged, err)
	}
}

func TestIdlePollingAndPermanentEventIdentity(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	first, second, third := pendingReview(t, s), pendingReview(t, s), pendingReview(t, s)
	a, b, c := fakeCandidate(t, s, first), fakeCandidate(t, s, second), fakeCandidate(t, s, third)
	if yes, err := s.approveCover(ctx, first.ID, a.Revision, testUpdate(100000), "Owned"); err != nil || !yes {
		t.Fatal(yes, err)
	}
	if offset, err := s.pollOffset(ctx); err != nil || offset != 100001 {
		t.Fatal("active polling position", offset, err)
	}
	if _, err := s.db.Exec("UPDATE telegram_cursor SET updated_at=?", time.Now().Add(-8*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if offset, err := s.pollOffset(ctx); err != nil || offset != 0 {
		t.Fatal("idle polling still acknowledges the old sequence", offset, err)
	}
	if yes, err := s.approveCover(ctx, second.ID, b.Revision, testUpdate(50), "Loaned"); err != nil || !yes {
		t.Fatal("new lower ID lost", yes, err)
	}
	replay := testUpdate(100000)
	replay.ID = 51
	if yes, err := s.approveCover(ctx, third.ID, c.Revision, replay, "Loaned"); err != nil || yes {
		t.Fatal("old event repeated under a different transport ID", yes, err)
	}
	fresh := updateReceipt{ID: 50, Key: "new-event-with-reused-update-number"}
	if yes, err := s.approveCover(ctx, third.ID, c.Revision, fresh, "Owned"); err != nil || !yes {
		t.Fatal("reused transport ID hid a different event", yes, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM telegram_updates WHERE id=50").Scan(&count); err != nil || count != 2 {
		t.Fatal("event receipts collapsed distinct updates", count, err)
	}
	if offset, err := s.pollOffset(ctx); err != nil || offset != 51 {
		t.Fatal("new polling position did not commit", offset, err)
	}
}

func TestFailedReplacementPreservesCandidate(t *testing.T) {
	s, _, _ := testStore(t)
	j := pendingReview(t, s)
	old := fakeCandidate(t, s, j)
	claim, err := s.claimCover(context.Background(), j.ID, old.Revision, testUpdate(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishCover(context.Background(), claim, testUpdate(1), "", "", "", "", "malformed image"); err != nil {
		t.Fatal(err)
	}
	current, err := s.Review(context.Background(), j.ID)
	if err != nil || current.Cover != old.Cover || current.Digest != old.Digest || current.Revision != old.Revision || current.Source != old.Source {
		t.Fatal("failed image replaced candidate", current, err)
	}
	if yes, err := s.approveCover(context.Background(), j.ID, current.Revision, testUpdate(2), "Owned"); err != nil || !yes {
		t.Fatal("retained displayed image could not be approved", err)
	}
}
