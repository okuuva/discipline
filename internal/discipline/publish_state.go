package discipline

import (
	"context"
	"errors"
)

const publishSchema = `
CREATE TABLE destinations (
 category TEXT NOT NULL, disc_key TEXT NOT NULL, sequence INTEGER NOT NULL,
 PRIMARY KEY(category,disc_key)
);
INSERT INTO destinations SELECT reviews.category,jobs.disc_key,MAX(jobs.sequence)
 FROM jobs JOIN reviews ON reviews.job_id=jobs.id WHERE reviews.category!='' AND jobs.disc_key!=''
 GROUP BY reviews.category,jobs.disc_key;
CREATE TABLE publications (
 job_id TEXT PRIMARY KEY REFERENCES jobs(id),
 notified_phase TEXT NOT NULL DEFAULT '', notify_after INTEGER NOT NULL DEFAULT 0
);
PRAGMA user_version=4;
`

// Only the lifetime-locked publisher may recover these claims: its children
// inherit that lock, so a previous copy must be quiescent before this runs.
func (s *Store) recoverPublisher(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error='copy interrupted; inbox may be partial or already imported; reconcile before retry' WHERE phase=?", Uncertain, Copying)
	return err
}

func (s *Store) publisherRequests(ctx context.Context) error {
	requests, err := s.requests(ctx, "status='queued' AND (action IN ('reconcile','cleanup') OR (action='retry' AND phase='copy-failed'))")
	if err != nil {
		return err
	}
	for _, r := range requests {
		if err := s.publisherRequest(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) publisherRequest(ctx context.Context, r Request) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM requests WHERE id=?", r.ID).Scan(&status); err != nil {
		return err
	}
	if status != "queued" {
		return nil
	}
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=?", r.JobID))
	if err != nil {
		return err
	}
	status, result := "completed", "accepted by publisher"
	if j.Revision != r.Revision || j.Phase != r.Phase || !requestAllowed(j, r) {
		status, result = "rejected", "job changed or is busy"
	} else {
		phase, outcome := j.Phase, j.Outcome
		switch r.Action {
		case "retry":
			phase = Ready
		case "cleanup":
			phase = Cleaning
		case "reconcile":
			switch r.Outcome {
			case "not-published":
				phase = Ready
			case "published":
				phase, outcome = Published, Published
			case "abandoned":
				phase, outcome = Abandoned, Abandoned
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,outcome=?,revision=revision+1,error='' WHERE id=?", phase, outcome, j.ID); err != nil {
			return err
		}
		if phase == Published {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO publications(job_id) VALUES(?)", j.ID); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE requests SET status=?,result=? WHERE id=?", status, result, r.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) claimPublication(ctx context.Context) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE phase=? ORDER BY sequence LIMIT 1", Ready))
	if err != nil {
		return Job{}, err
	}
	r, err := scanReview(tx.QueryRowContext(ctx, "SELECT "+reviewColumns+" FROM reviews WHERE job_id=?", j.ID))
	if err != nil {
		return Job{}, err
	}
	if _, err := storedDisc(j); err != nil {
		return Job{}, err
	}
	if (r.Category != "Owned" && r.Category != "Loaned") || len(r.Manifest) == 0 {
		return Job{}, errors.New("ready job has no frozen approval/manifest")
	}
	var newest int64
	if err := tx.QueryRowContext(ctx, "SELECT sequence FROM destinations WHERE category=? AND disc_key=?", r.Category, j.DiscKey).Scan(&newest); err != nil {
		return Job{}, err
	}
	phase, message := Copying, ""
	if newest > j.Sequence {
		phase, message = Superseded, "newer approved attempt owns this inbox destination; do not resend"
	}
	if newest < j.Sequence {
		return Job{}, errors.New("destination approval history is incomplete")
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO publications(job_id) VALUES(?)", j.ID); err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE publications SET notified_phase='',notify_after=0 WHERE job_id=?", j.ID); err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error=? WHERE id=?", phase, message, j.ID); err != nil {
		return Job{}, err
	}
	j.Phase, j.Error, j.Revision = phase, message, j.Revision+1
	return j, tx.Commit()
}

func (s *Store) claimCleanup(ctx context.Context, keep bool) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE cleaned=0 AND
 (phase=? OR (?=0 AND phase=? AND error='' AND EXISTS(SELECT 1 FROM publications WHERE job_id=jobs.id AND notified_phase=?)))
 ORDER BY sequence LIMIT 1`, Cleaning, keep, Published, Published))
	if err != nil {
		return Job{}, err
	}
	if j.Outcome != Published && j.Outcome != Abandoned {
		return Job{}, errors.New("cleanup lacks a terminal receipt")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1 WHERE id=?", Cleaning, j.ID); err != nil {
		return Job{}, err
	}
	j.Phase, j.Revision = Cleaning, j.Revision+1
	return j, tx.Commit()
}

func (s *Store) finishCleanup(ctx context.Context, j Job, workErr error) error {
	message := ""
	if workErr != nil {
		message = workErr.Error()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET phase=outcome,cleaned=?,error=?,revision=revision+1
 WHERE id=? AND phase=? AND revision=?`, workErr == nil, message, j.ID, Cleaning, j.Revision)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.Join(ErrConflict, err)
	}
	return nil
}

// A failed synchronous copy is quiescent; only an unknown/crashed copy requires reconciliation.
func (s *Store) finishPublication(ctx context.Context, j Job, workErr error) error {
	phase, message := Published, ""
	if workErr != nil {
		phase, message = CopyFailed, workErr.Error()+"; inbox is not ready; retain local files and explicitly retry"
	}
	_, err := s.Transition(ctx, j, phase, message)
	return err
}
