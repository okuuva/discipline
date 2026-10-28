package discipline

import (
	"context"
	"database/sql"
	"errors"
)

var errDiscChanged = errors.New("queued disc no longer loaded")

const driveRequestFilter = "status='queued' AND (action='new-insertion' OR (action='retry' AND phase='rip-failed'))"

// ObserveDisc records the insertion and its job in the same commit. Same-disc
// offline reinsertion is intentionally indistinguishable from a service restart.
func (s *Store) ObserveDisc(ctx context.Context, disc Disc) (string, error) {
	return s.observeInsertion(ctx, disc, false)
}

func (s *Store) observeInsertion(ctx context.Context, disc Disc, changedGeneration bool) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := observeDisc(ctx, tx, disc, changedGeneration)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func observeDisc(ctx context.Context, tx *sql.Tx, disc Disc, force bool) (string, error) {
	if disc.Kind != "absent" && disc.Kind != "audio" && disc.Kind != "data" && disc.Kind != "mixed" {
		return "", errors.New("unknown media state")
	}
	var key, toc string
	var err error
	if disc.Kind == "audio" {
		key, toc, err = disc.Identity()
		if err != nil {
			return "", err
		}
	}
	var oldKind, oldKey, oldJob string
	err = tx.QueryRowContext(ctx, "SELECT kind, disc_key, COALESCE(job_id, '') FROM drive WHERE id=1").Scan(&oldKind, &oldKey, &oldJob)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if !force && oldKind == disc.Kind && oldKey == key {
		return oldJob, nil
	}
	var id any
	jobID := ""
	if disc.Kind == "audio" {
		jobID, err = newJobID()
		if err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO jobs(id, phase, disc_key, toc) VALUES (?, ?, ?, ?)", jobID, Queued, key, toc); err != nil {
			return "", err
		}
		id = jobID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO drive(id, kind, disc_key, job_id) VALUES (1, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, disc_key=excluded.disc_key, job_id=excluded.job_id`, disc.Kind, key, id)
	return jobID, err
}

// applyDriveRequests only changes local intent under the main service's lifetime
// lock. The caller samples the drive with no extraction running before invoking it.
func (s *Store) applyDriveRequests(ctx context.Context, disc Disc) error {
	requests, err := s.requests(ctx, driveRequestFilter)
	if err != nil {
		return err
	}
	if len(requests) == 0 {
		return nil
	}
	return s.applyDriveRequest(ctx, requests[0], disc)
}

func (s *Store) applyDriveRequest(ctx context.Context, r Request, disc Disc) error {
	if r.Action != "new-insertion" && (r.Action != "retry" || r.Phase != RipFailed) {
		return nil
	}
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
	var busy int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE phase=?", Ripping).Scan(&busy); err != nil {
		return err
	}
	if busy != 0 {
		return ErrConflict
	}
	key, _, identityErr := disc.Identity()
	result, state := "", "completed"
	if identityErr != nil {
		result, state = "insert an audio-only CD before requesting this action", "rejected"
	} else if r.Action == "new-insertion" {
		id, err := observeDisc(ctx, tx, disc, true)
		if err != nil {
			return err
		}
		result = "created job " + id
	} else {
		j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=?", r.JobID))
		if err != nil {
			return err
		}
		if j.Phase != RipFailed || j.Revision != r.Revision || key != j.DiscKey {
			result, state = "job changed or loaded disc does not match the failed attempt", "rejected"
		} else {
			if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?, error='', revision=revision+1 WHERE id=?", Queued, j.ID); err != nil {
				return err
			}
			// Explicit retry authorizes this attempt to use the verified loaded disc.
			if _, err := tx.ExecContext(ctx, "UPDATE drive SET kind='audio', disc_key=?, job_id=? WHERE id=1", key, j.ID); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE requests SET status=?, result=? WHERE id=? AND status='queued'", state, result, r.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) interruptedRips(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET phase=?, error=?, revision=revision+1 WHERE phase=?", RipFailed, "extraction interrupted; retain local files and explicitly retry with the intended CD", Ripping)
	return err
}

func (s *Store) nextRip(ctx context.Context, disc Disc) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE phase=? ORDER BY id=(SELECT job_id FROM drive WHERE id=1) DESC, sequence LIMIT 1", Queued))
	if err != nil {
		return Job{}, err
	}
	var owner string
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(job_id, '') FROM drive WHERE id=1").Scan(&owner); err != nil {
		return Job{}, err
	}
	key, _, identityErr := disc.Identity()
	j.Phase, j.Error = Ripping, ""
	if identityErr != nil || key != j.DiscKey || owner != j.ID {
		j.Phase, j.Error = RipFailed, "insertion no longer owns the drive; insert the intended CD and explicitly retry"
	}
	if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?, error=?, revision=revision+1 WHERE id=?", j.Phase, j.Error, j.ID); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	j.Revision++
	if j.Phase == RipFailed {
		return Job{}, errDiscChanged
	}
	return j, nil
}
