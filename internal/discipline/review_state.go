package discipline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"
)

const reviewSchema = `
CREATE TABLE reviews (
 job_id TEXT PRIMARY KEY REFERENCES jobs(id),
 revision INTEGER NOT NULL DEFAULT 0,
 title TEXT NOT NULL DEFAULT 'Unknown metadata',
 cover TEXT NOT NULL DEFAULT '', digest TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '',
 category TEXT NOT NULL DEFAULT '' CHECK(category IN ('', 'Owned', 'Loaned')),
 art_attempted INTEGER NOT NULL DEFAULT 0,
 notified_revision INTEGER NOT NULL DEFAULT -1,
 notify_after INTEGER NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '', manifest TEXT NOT NULL DEFAULT ''
);
CREATE TABLE review_messages (
 chat_id INTEGER NOT NULL, message_id INTEGER NOT NULL,
 job_id TEXT NOT NULL REFERENCES jobs(id), revision INTEGER NOT NULL,
 PRIMARY KEY(chat_id,message_id)
);
CREATE TABLE telegram_updates (event_key TEXT PRIMARY KEY NOT NULL, id INTEGER NOT NULL, job_id TEXT REFERENCES jobs(id), outcome TEXT NOT NULL);
CREATE TABLE telegram_cursor (id INTEGER PRIMARY KEY CHECK(id=1), offset INTEGER NOT NULL CHECK(offset>=0), updated_at INTEGER NOT NULL DEFAULT 0 CHECK(updated_at>=0));
INSERT INTO telegram_cursor(id,offset) VALUES(1,0);
PRAGMA user_version=3;
`

type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Review struct {
	JobID            string      `json:"job_id"`
	Revision         int64       `json:"revision"`
	Title            string      `json:"title"`
	Cover            string      `json:"cover,omitempty"`
	Digest           string      `json:"digest,omitempty"`
	Source           string      `json:"source,omitempty"`
	Category         string      `json:"category,omitempty"`
	ArtAttempted     bool        `json:"art_attempted"`
	NotifiedRevision int64       `json:"notified_revision"`
	Error            string      `json:"error,omitempty"`
	Manifest         []FileEntry `json:"manifest,omitempty"`
}

const reviewColumns = "job_id, revision, title, cover, digest, source, category, art_attempted, notified_revision, error, manifest"

func scanReview(row interface{ Scan(...any) error }) (Review, error) {
	var r Review
	var manifest string
	err := row.Scan(&r.JobID, &r.Revision, &r.Title, &r.Cover, &r.Digest, &r.Source, &r.Category, &r.ArtAttempted, &r.NotifiedRevision, &r.Error, &manifest)
	if err == nil && manifest != "" {
		err = json.Unmarshal([]byte(manifest), &r.Manifest)
	}
	return r, err
}
func (s *Store) Review(ctx context.Context, id string) (Review, error) {
	return scanReview(s.db.QueryRowContext(ctx, "SELECT "+reviewColumns+" FROM reviews WHERE job_id=?", id))
}

var errUpdateApplied = errors.New("Telegram update already applied")

type updateReceipt struct {
	ID  int64
	Key string
}

func (u updateReceipt) valid() bool {
	return u == (updateReceipt{}) || u.ID > 0 && u.ID < math.MaxInt64 && u.Key != "" && len(u.Key) <= 512
}

func (s *Store) pollOffset(ctx context.Context) (int64, error) {
	var offset, updated int64
	if err := s.db.QueryRowContext(ctx, "SELECT offset,updated_at FROM telegram_cursor WHERE id=1").Scan(&offset, &updated); err != nil {
		return 0, err
	}
	// Telegram retains updates for 24h and randomizes numbering after a week idle.
	if time.Now().Unix()-updated >= 24*60*60 {
		return 0, nil
	}
	return offset, nil
}

func advanceCursor(ctx context.Context, tx *sql.Tx, u updateReceipt) error {
	result, err := tx.ExecContext(ctx, "UPDATE telegram_cursor SET offset=?,updated_at=? WHERE id=1", u.ID+1, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Telegram cursor is missing")
	}
	return nil
}

// Replays acknowledge only the cursor; fresh events return an open transaction for their effect.
func (s *Store) beginUpdate(ctx context.Context, u updateReceipt) (*sql.Tx, error) {
	if !u.valid() {
		return nil, errors.New("invalid Telegram update identity")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return tx, nil
	}
	var seen bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM telegram_updates WHERE event_key=?)", u.Key).Scan(&seen); err != nil {
		tx.Rollback()
		return nil, err
	}
	if !seen {
		return tx, nil
	}
	defer tx.Rollback()
	if err := advanceCursor(ctx, tx, u); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nil, errUpdateApplied
}

func recordUpdate(ctx context.Context, tx *sql.Tx, u updateReceipt, jobID, outcome string) error {
	if !u.valid() {
		return errors.New("invalid Telegram update identity")
	}
	if u.ID == 0 {
		return nil
	}
	var job any
	if jobID != "" {
		job = jobID
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO telegram_updates(event_key,id,job_id,outcome) VALUES(?,?,?,?)", u.Key, u.ID, job, outcome); err != nil {
		return err
	}
	return advanceCursor(ctx, tx, u)
}

func (s *Store) ignoreUpdate(ctx context.Context, u updateReceipt, reason string) error {
	tx, err := s.beginUpdate(ctx, u)
	if errors.Is(err, errUpdateApplied) {
		return nil
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordUpdate(ctx, tx, u, "", reason); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) claimCover(ctx context.Context, id string, revision int64, u updateReceipt) (Job, error) {
	tx, err := s.beginUpdate(ctx, u)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=?", id))
	if err != nil {
		return Job{}, err
	}
	r, err := scanReview(tx.QueryRowContext(ctx, "SELECT "+reviewColumns+" FROM reviews WHERE job_id=?", id))
	if err != nil {
		return Job{}, err
	}
	if j.Phase != AwaitingReview || r.Revision != revision || r.Category != "" {
		return Job{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1 WHERE id=?", EditingCover, id); err != nil {
		return Job{}, err
	}
	j.Phase, j.Revision = EditingCover, j.Revision+1
	return j, tx.Commit()
}

func (s *Store) finishCover(ctx context.Context, j Job, update updateReceipt, cover, digest, source, title, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error=? WHERE id=? AND phase=? AND revision=?", AwaitingReview, message, j.ID, EditingCover, j.Revision)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.Join(ErrConflict, err)
	}
	if cover != "" {
		if _, err := tx.ExecContext(ctx, "UPDATE reviews SET revision=revision+1,cover=?,digest=?,source=? WHERE job_id=?", cover, digest, source, j.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE reviews SET art_attempted=1,notified_revision=-1,notify_after=0,error=?,title=CASE WHEN ?='' THEN title ELSE ? END WHERE job_id=?", message, title, title, j.ID); err != nil {
		return err
	}
	outcome := "cover replaced"
	if cover == "" {
		outcome = "cover unchanged"
	}
	if err := recordUpdate(ctx, tx, update, j.ID, outcome); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) approveCover(ctx context.Context, id string, revision int64, update updateReceipt, category string) (bool, error) {
	if category != "Owned" && category != "Loaned" {
		return false, errors.New("invalid category")
	}
	tx, err := s.beginUpdate(ctx, update)
	if errors.Is(err, errUpdateApplied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET phase=?,revision=revision+1,error='' WHERE id=? AND phase=? AND EXISTS(
SELECT 1 FROM reviews WHERE job_id=? AND revision=? AND cover!='' AND digest!='' AND category='')`, Approved, id, AwaitingReview, id, revision)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	outcome := "stale or unavailable approval"
	if n == 1 {
		if _, err := tx.ExecContext(ctx, "UPDATE reviews SET category=? WHERE job_id=?", category, id); err != nil {
			return false, err
		}
		var key string
		var sequence int64
		if err := tx.QueryRowContext(ctx, "SELECT disc_key,sequence FROM jobs WHERE id=?", id).Scan(&key, &sequence); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO destinations(category,disc_key,sequence) VALUES(?,?,?)
ON CONFLICT(category,disc_key) DO UPDATE SET sequence=MAX(sequence,excluded.sequence)`, category, key, sequence); err != nil {
			return false, err
		}
		outcome = "approved " + category
	}
	if err := recordUpdate(ctx, tx, update, id, outcome); err != nil {
		return false, err
	}
	return n == 1, tx.Commit()
}

func (s *Store) rememberPreview(ctx context.Context, chat, message int64, r Review) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO review_messages(chat_id,message_id,job_id,revision) VALUES(?,?,?,?)", chat, message, r.JobID, r.Revision); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE reviews SET notified_revision=? WHERE job_id=? AND revision=?", r.Revision, r.JobID, r.Revision); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) finishFinalization(ctx context.Context, j Job, manifest []FileEntry, workErr error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	phase, message := Ready, ""
	if workErr != nil {
		phase, message = FinalizeFailed, workErr.Error()
	} else {
		data, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE reviews SET manifest=? WHERE job_id=?", string(data), j.ID); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error=? WHERE id=? AND phase=? AND revision=?", phase, message, j.ID, Finalizing, j.Revision)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.Join(ErrConflict, err)
	}
	return tx.Commit()
}
