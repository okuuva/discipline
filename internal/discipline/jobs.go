package discipline

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrConflict = errors.New("job changed or is owned by a worker")
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type Phase string

const (
	Queued         Phase = "queued"
	Ripping        Phase = "ripping"
	RipFailed      Phase = "rip-failed"
	AwaitingReview Phase = "awaiting-review"
	Finalizing     Phase = "finalizing"
	FinalizeFailed Phase = "finalize-failed"
	Ready          Phase = "ready"
	Copying        Phase = "copying"
	Uncertain      Phase = "uncertain"
	Published      Phase = "published"
	Abandoned      Phase = "abandoned"
	Cleaning       Phase = "cleaning"
)

type Job struct {
	ID       string `json:"id"`
	Sequence int64  `json:"sequence"`
	Revision int64  `json:"revision"`
	Phase    Phase  `json:"phase"`
	Outcome  Phase  `json:"outcome,omitempty"`
	Error    string `json:"error,omitempty"`
	DiscKey  string `json:"disc_key,omitempty"`
	TOC      string `json:"toc,omitempty"`
	Cleaned  bool   `json:"cleaned"`
}

type Request struct {
	ID       int64  `json:"id"`
	JobID    string `json:"job_id,omitempty"`
	Revision int64  `json:"revision"`
	Phase    Phase  `json:"expected_phase,omitempty"`
	Action   string `json:"action"`
	Outcome  string `json:"outcome,omitempty"`
	Note     string `json:"note,omitempty"`
	Status   string `json:"status"`
	Result   string `json:"result,omitempty"`
}

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE jobs (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 revision INTEGER NOT NULL DEFAULT 0,
 phase TEXT NOT NULL,
 outcome TEXT NOT NULL DEFAULT '' CHECK (outcome IN ('', 'published', 'abandoned')),
 error TEXT NOT NULL DEFAULT '',
 cleaned INTEGER NOT NULL DEFAULT 0 CHECK (cleaned IN (0, 1))
);
CREATE TABLE requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 job_id TEXT REFERENCES jobs(id),
 revision INTEGER NOT NULL,
 action TEXT NOT NULL,
 outcome TEXT NOT NULL,
 note TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued',
 result TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX pending_request ON requests(COALESCE(job_id, '')) WHERE status = 'queued';
PRAGMA user_version = 1;
`

// OpenStore only initializes empty, private local directories when create is true.
// An interrupted initialization, lost database or orphan spool requires operator repair.
func OpenStore(state, spool string, create bool) (*Store, error) {
	for _, path := range []string{state, spool} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
			return nil, fmt.Errorf("expected an absolute local directory: %q", path)
		}
		if create {
			if err := os.MkdirAll(path, 0700); err != nil {
				return nil, err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("directory must be private and not a symlink: %s", path)
		}
	}
	if state == spool || strings.HasPrefix(state+"/", spool+"/") || strings.HasPrefix(spool+"/", state+"/") {
		return nil, errors.New("state and spool must be separate directories")
	}
	path := filepath.Join(state, "state.sqlite3")
	_, err := os.Lstat(path)
	initialize := errors.Is(err, os.ErrNotExist)
	if err != nil && !initialize {
		return nil, err
	}
	if initialize {
		if !create {
			return nil, errors.New("state database is missing; refuse to reconstruct jobs")
		}
		for _, dir := range []string{state, spool} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return nil, err
			}
			if len(entries) != 0 {
				return nil, errors.New("state database is missing but local data exists; reconciliation required")
			}
		}
		// Keep this sentinel after cleanup, so losing an empty database is detectable.
		if err := exclusiveFile(filepath.Join(state, "initialized"), []byte("discipline state v1\n")); err != nil {
			return nil, err
		}
		if err := exclusiveFile(path, nil); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"initialized", "state.sqlite3", "state.sqlite3-wal", "state.sqlite3-shm"} {
		info, err := os.Lstat(filepath.Join(state, name))
		if errors.Is(err, os.ErrNotExist) && strings.HasPrefix(name, "state.sqlite3-") {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("state file must be private and regular: %s", name)
		}
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = "mode=rw&_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_txlock=immediate"
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.check(initialize, spool); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func exclusiveFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (s *Store) check(initialize bool, spool string) error {
	if initialize {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == 1 {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version == 1 {
			if _, err := tx.Exec(`
ALTER TABLE jobs ADD COLUMN disc_key TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN toc TEXT NOT NULL DEFAULT '';
CREATE INDEX jobs_phase ON jobs(phase);
ALTER TABLE requests ADD COLUMN phase TEXT NOT NULL DEFAULT '';
UPDATE requests SET phase=COALESCE((SELECT phase FROM jobs WHERE jobs.id=requests.job_id), '');
CREATE TABLE drive (id INTEGER PRIMARY KEY CHECK(id=1), kind TEXT NOT NULL, disc_key TEXT NOT NULL, job_id TEXT REFERENCES jobs(id));
PRAGMA user_version = 2;`); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		version = 2
	}
	if version != 2 {
		return fmt.Errorf("unsupported or incomplete state schema %d", version)
	}
	var integrity string
	if err := s.db.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("state database is corrupt: %s", integrity)
	}
	entries, err := os.ReadDir(spool)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !uuidPattern.MatchString(entry.Name()) {
			return fmt.Errorf("unexpected spool entry %q; reconciliation required", entry.Name())
		}
		if _, err := s.Job(context.Background(), entry.Name()); err != nil {
			return fmt.Errorf("orphan spool %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func newJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func (s *Store) CreateJob(ctx context.Context) (Job, error) {
	id, err := newJobID()
	if err != nil {
		return Job{}, err
	}
	result, err := s.db.ExecContext(ctx, "INSERT INTO jobs(id, phase) VALUES (?, ?)", id, Queued)
	if err != nil {
		return Job{}, err
	}
	seq, err := result.LastInsertId()
	return Job{ID: id, Sequence: seq, Phase: Queued}, err
}

const jobColumns = "id, sequence, revision, phase, outcome, error, disc_key, toc, cleaned"

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Sequence, &j.Revision, &j.Phase, &j.Outcome, &j.Error, &j.DiscKey, &j.TOC, &j.Cleaned)
	return j, err
}

func (s *Store) Job(ctx context.Context, id string) (Job, error) {
	if !uuidPattern.MatchString(id) {
		return Job{}, errors.New("expected a job UUID")
	}
	return scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", id))
}

func (s *Store) Jobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+jobColumns+" FROM jobs ORDER BY sequence")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// Transition claims a phase using its revision. Callers commit before doing I/O.
// Worker completion must use the returned revision, never reread and steal a claim.
func (s *Store) Transition(ctx context.Context, j Job, next Phase, message string) (Job, error) {
	allowed := map[Phase][]Phase{
		Queued: {Ripping, Abandoned}, Ripping: {AwaitingReview, RipFailed},
		RipFailed: {Ripping, Abandoned}, AwaitingReview: {Finalizing, Abandoned},
		Finalizing: {Ready, FinalizeFailed}, FinalizeFailed: {Finalizing, Abandoned},
		Ready: {Copying, Abandoned}, Copying: {Published, Uncertain},
		Published: {Cleaning}, Abandoned: {Cleaning},
	}
	valid := false
	for _, phase := range allowed[j.Phase] {
		valid = valid || phase == next
	}
	if !valid || j.Cleaned {
		return Job{}, fmt.Errorf("invalid transition %s -> %s", j.Phase, next)
	}
	outcome := j.Outcome
	if next == Published || next == Abandoned {
		outcome = next
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET phase = ?, outcome = ?, error = ?, revision = revision + 1
WHERE id = ? AND revision = ? AND phase = ? AND cleaned = 0`, next, outcome, message, j.ID, j.Revision, j.Phase)
	if err != nil {
		return Job{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return Job{}, errors.Join(ErrConflict, err)
	}
	j.Phase, j.Outcome, j.Error, j.Revision = next, outcome, message, j.Revision+1
	return j, nil
}

func requestAllowed(j Job, r Request) bool {
	switch r.Action {
	case "retry":
		return j.Phase == RipFailed || j.Phase == FinalizeFailed
	case "cleanup":
		return !j.Cleaned && (j.Phase == Published || j.Phase == Abandoned)
	case "reconcile":
		if strings.TrimSpace(r.Note) == "" {
			return false
		}
		if r.Outcome == "abandoned" {
			return j.Phase == Queued || j.Phase == RipFailed || j.Phase == AwaitingReview || j.Phase == FinalizeFailed || j.Phase == Ready || j.Phase == Uncertain
		}
		return j.Phase == Uncertain && (r.Outcome == "published" || r.Outcome == "not-published")
	}
	return false
}

// Enqueue records intent only. The owning worker must recheck the revision and
// quiescence before applying it; a CLI request never grants filesystem ownership.
func (s *Store) Enqueue(ctx context.Context, r Request) (Request, error) {
	if len(r.Note) > 4096 || (r.Action != "reconcile" && (r.Note != "" || r.Outcome != "")) {
		return Request{}, errors.New("invalid request arguments")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, err
	}
	defer tx.Rollback()
	var jobID any
	if r.Action == "new-insertion" {
		if r.JobID != "" {
			return Request{}, errors.New("new-insertion does not accept a job")
		}
		r.Revision, r.Phase = 0, ""
	} else {
		j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", r.JobID))
		if err != nil {
			return Request{}, err
		}
		if !requestAllowed(j, r) {
			return Request{}, ErrConflict
		}
		jobID, r.Revision, r.Phase = j.ID, j.Revision, j.Phase
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO requests(job_id, revision, phase, action, outcome, note) VALUES (?, ?, ?, ?, ?, ?)", jobID, r.Revision, r.Phase, r.Action, r.Outcome, r.Note)
	if err != nil {
		return Request{}, err
	}
	r.ID, err = result.LastInsertId()
	if err != nil {
		return Request{}, err
	}
	r.Status, r.Result = "queued", ""
	return r, tx.Commit()
}

func (s *Store) Requests(ctx context.Context) ([]Request, error) {
	return s.requests(ctx, "")
}

func (s *Store) requests(ctx context.Context, predicate string) ([]Request, error) {
	query := `SELECT id, COALESCE(job_id, ''), revision, phase, action, outcome, note, status, result FROM requests`
	if predicate != "" {
		query += " WHERE " + predicate
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := []Request{}
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.JobID, &r.Revision, &r.Phase, &r.Action, &r.Outcome, &r.Note, &r.Status, &r.Result); err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

// Actions are local recovery requests, not automatic retries or remote cleanup.
func (j Job) Actions() []string {
	actions := []string{}
	for _, r := range []Request{{Action: "retry"}, {Action: "cleanup"}, {Action: "reconcile", Outcome: "abandoned", Note: "operator decision"}} {
		if requestAllowed(j, r) {
			actions = append(actions, r.Action)
		}
	}
	return actions
}

func (j Job) MarshalJSON() ([]byte, error) {
	type plain Job
	return json.Marshal(struct {
		plain
		Actions []string `json:"actions"`
	}{plain(j), j.Actions()})
}
