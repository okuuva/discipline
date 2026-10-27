package discipline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInsertionPersistence(t *testing.T) {
	s, state, spool := testStore(t)
	ctx := context.Background()
	disc := Disc{Kind: "audio", Offsets: []int{150, 10000}, Leadout: 20000}
	first, err := s.ObserveDisc(ctx, disc)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		id, err := s.ObserveDisc(ctx, disc)
		if err != nil || id != first {
			t.Fatalf("duplicate insertion: %s %v", id, err)
		}
	}
	s.Close()
	s, err = OpenStore(state, spool, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.ObserveDisc(ctx, disc)
	if err != nil || id != first {
		t.Fatalf("restart lost insertion: %s %v", id, err)
	}
	for _, kind := range []string{"absent", "data", "mixed"} {
		id, err := s.ObserveDisc(ctx, Disc{Kind: kind})
		if err != nil || id != "" {
			t.Fatalf("non-audio created job: %s %v", id, err)
		}
	}
	second, err := s.ObserveDisc(ctx, disc)
	if err != nil || second == first {
		t.Fatalf("reinsertion reused attempt: %s %v", second, err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 2 || jobs[0].DiscKey != jobs[1].DiscKey || jobs[0].TOC != "2 150 10000 20000" {
		t.Fatalf("disc/attempt isolation: %+v %v", jobs, err)
	}
}

func TestDriveRequestsAreAtomicAndDiscSpecific(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
	id, err := s.ObserveDisc(ctx, disc)
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Job(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	j = step(t, s, j, Ripping)
	r, err := s.Enqueue(ctx, Request{Action: "new-insertion"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyDriveRequest(ctx, r, disc); err == nil {
		t.Fatal("overrode active extraction")
	}
	j = step(t, s, j, RipFailed)
	if err := s.applyDriveRequest(ctx, r, disc); err != nil {
		t.Fatal(err)
	}
	if err := s.applyDriveRequest(ctx, r, disc); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.Jobs(ctx)
	if len(jobs) != 2 {
		t.Fatalf("replayed new-insertion created %d jobs", len(jobs))
	}
	r, err = s.Enqueue(ctx, Request{JobID: j.ID, Action: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	wrong := disc
	wrong.Leadout++
	if err := s.applyDriveRequest(ctx, r, wrong); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Job(ctx, j.ID)
	if got.Phase != RipFailed {
		t.Fatal("retried with wrong disc")
	}
	r, err = s.Enqueue(ctx, Request{JobID: j.ID, Action: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyDriveRequest(ctx, r, disc); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Job(ctx, j.ID)
	if got.Phase != Queued || got.Revision != j.Revision+1 {
		t.Fatalf("retry lost attempt: %+v", got)
	}
	if err := s.applyDriveRequest(ctx, r, disc); err != nil {
		t.Fatal(err)
	}
	latest, _ := s.Job(ctx, j.ID)
	if latest != got {
		t.Fatal("replayed retry had another effect")
	}
}

func TestQueuedAttemptMustOwnCurrentInsertion(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
	old, err := s.ObserveDisc(ctx, disc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ObserveDisc(ctx, Disc{Kind: "absent"}); err != nil {
		t.Fatal(err)
	}
	current, err := s.ObserveDisc(ctx, disc)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.nextRip(ctx, disc)
	if err != nil || claimed.ID != current {
		t.Fatalf("current insertion not claimed: %+v %v", claimed, err)
	}
	step(t, s, claimed, RipFailed)
	if _, err := s.nextRip(ctx, disc); !errors.Is(err, errDiscChanged) {
		t.Fatalf("stale insertion claimed identical TOC: %v", err)
	}
	request, err := s.Enqueue(ctx, Request{JobID: old, Action: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyDriveRequest(ctx, request, disc); err != nil {
		t.Fatal(err)
	}
	claimed, err = s.nextRip(ctx, disc)
	if err != nil || claimed.ID != old {
		t.Fatalf("explicit retry did not acquire loaded disc: %+v %v", claimed, err)
	}
}

func TestDriveAcceptsOneRetryAtATime(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
	var ids []string
	for range 2 {
		id, err := s.observeInsertion(ctx, disc, true)
		if err != nil {
			t.Fatal(err)
		}
		j, err := s.Job(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		step(t, s, step(t, s, j, Ripping), RipFailed)
		ids = append(ids, id)
	}
	for _, id := range ids {
		if _, err := s.Enqueue(ctx, Request{JobID: id, Action: "retry"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.applyDriveRequests(ctx, disc); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.nextRip(ctx, disc)
	if err != nil || claimed.ID != ids[0] {
		t.Fatalf("first accepted retry lost ownership: %+v %v", claimed, err)
	}
	requests, err := s.Requests(ctx)
	if err != nil || requests[0].Status != "completed" || requests[1].Status != "queued" {
		t.Fatalf("batch accepted conflicting retries: %+v %v", requests, err)
	}
	if err := s.applyDriveRequests(ctx, disc); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted retry while busy: %v", err)
	}
	step(t, s, claimed, RipFailed)
	if err := s.applyDriveRequests(ctx, disc); err != nil {
		t.Fatal(err)
	}
	claimed, err = s.nextRip(ctx, disc)
	if err != nil || claimed.ID != ids[1] {
		t.Fatalf("second retry not dispatched after quiescence: %+v %v", claimed, err)
	}
}

func TestDriveLeavesReviewRequestsPending(t *testing.T) {
	s, _, _ := testStore(t)
	j := job(t, s)
	for _, phase := range []Phase{Ripping, AwaitingReview, Finalizing, FinalizeFailed} {
		j = step(t, s, j, phase)
	}
	r, err := s.Enqueue(context.Background(), Request{JobID: j.ID, Action: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyDriveRequests(context.Background(), Disc{Kind: "absent"}); err != nil {
		t.Fatal(err)
	}
	requests, err := s.Requests(context.Background())
	if err != nil || len(requests) != 1 || requests[0] != r {
		t.Fatalf("drive consumed review request: %+v %v", requests, err)
	}
}

func TestMediaGenerationCreatesNewSameTOCAttempt(t *testing.T) {
	s, _, _ := testStore(t)
	disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
	first, err := s.ObserveDisc(context.Background(), disc)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.observeInsertion(context.Background(), disc, true)
	if err != nil || first == second {
		t.Fatalf("media generation reused insertion: %s %v", second, err)
	}
	repeated, err := s.ObserveDisc(context.Background(), disc)
	if err != nil || repeated != second {
		t.Fatalf("repeated observation duplicated insertion: %s %v", repeated, err)
	}
}

func TestQueuedJobLookupUsesIndex(t *testing.T) {
	s, _, _ := testStore(t)
	var id, parent, unused int
	var detail string
	if err := s.db.QueryRow("EXPLAIN QUERY PLAN SELECT 1 FROM jobs WHERE phase='queued' LIMIT 1").Scan(&id, &parent, &unused, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "jobs_phase") {
		t.Fatalf("history scan: %s", detail)
	}
}

func TestInterruptedRipNeverAutomaticallyRetries(t *testing.T) {
	s, _, _ := testStore(t)
	j := step(t, s, job(t, s), Ripping)
	if err := s.interruptedRips(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(context.Background(), j.ID)
	if err != nil || got.Phase != RipFailed || got.Error == "" {
		t.Fatalf("interrupted attempt: %+v %v", got, err)
	}
}
