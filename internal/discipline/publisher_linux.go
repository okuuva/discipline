package discipline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type PublisherConfig struct {
	State, Spool string
	Storage      StorageConfig
	KeepLocal    bool
}

func Publish(ctx context.Context, c PublisherConfig) error {
	if os.Geteuid() == 0 {
		return errors.New("run the publisher as the non-root service user")
	}
	if err := c.Storage.validate(); err != nil {
		return err
	}
	s, err := OpenStore(c.State, c.Spool, false)
	if err != nil {
		return err
	}
	defer s.Close()
	fd, err := unix.Open(filepath.Join(c.State, "publisher.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	lease := os.NewFile(uintptr(fd), "publisher.lock")
	defer lease.Close()
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("another publisher or its child still owns transfers: %w", err)
	}
	if err := s.recoverPublisher(ctx); err != nil {
		return err
	}
	for ctx.Err() == nil {
		if err := s.publisherRequests(ctx); err != nil {
			return err
		}
		worked, err := s.publisherStep(ctx, c, lease)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
	return nil
}

func (s *Store) publisherStep(ctx context.Context, c PublisherConfig, lease *os.File) (bool, error) {
	j, err := s.claimCleanup(ctx, c.KeepLocal)
	if err == nil {
		workErr := cleanupAttempt(c.Spool, j)
		if workErr != nil {
			log.Printf("cleanup %s: %v", j.ID, workErr)
		}
		return true, s.finishCleanup(context.Background(), j, workErr)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	j, err = s.claimPublication(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if j.Phase == Superseded {
		return true, nil
	}
	r, workErr := s.Review(ctx, j.ID)
	if workErr == nil {
		workErr = transfer(ctx, c.Storage, c.Spool, j, r, lease)
	}
	if workErr != nil {
		log.Printf("publication %s: %v", j.ID, workErr)
	}
	return true, s.finishPublication(context.Background(), j, workErr)
}

func (s *Store) runPublicationNotices(ctx context.Context, bot *telegram) error {
	for ctx.Err() == nil {
		if err := s.notifyPublication(ctx, bot); err != nil && ctx.Err() == nil {
			if !errors.Is(err, errTelegram) {
				return err
			}
			log.Printf("publication notification: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
	return nil
}

func (s *Store) notifyPublication(ctx context.Context, bot *telegram) error {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT jobs.id FROM jobs JOIN publications ON publications.job_id=jobs.id
 WHERE jobs.phase IN ('published','copy-failed','uncertain','superseded') AND publications.notified_phase!=jobs.phase
 AND publications.notify_after<=? ORDER BY publications.notify_after,jobs.sequence LIMIT 1`, time.Now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	j, err := s.Job(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE publications SET notify_after=? WHERE job_id=?", time.Now().Add(30*time.Second).Unix(), id); err != nil {
		return err
	}
	text := "Job " + id + ": inbox is not ready. Local output retained. Check discipline show for recovery."
	if j.Phase == Superseded {
		text = "Job " + id + ": superseded by a newer approved rip; nothing copied."
	}
	if j.Phase == Published {
		r, err := s.Review(ctx, id)
		if err != nil {
			return err
		}
		var newest int64
		if err := s.db.QueryRowContext(ctx, "SELECT sequence FROM destinations WHERE category=? AND disc_key=?", r.Category, j.DiscKey).Scan(&newest); err != nil {
			return err
		}
		text = "Job " + id + ": historical verified transfer receipt. A newer approved rip owns the destination; this is NOT an import-ready signal."
		if newest == j.Sequence {
			text = "Job " + id + ": transfer complete, ready for manual import of " + r.Category + "/" + j.DiscKey + ". Publication is not library acceptance. Confirm no newer transfer has started. For a category-wide import, all approved transfers must be complete; do not approve or retry more during import."
		}
	}
	if err := bot.call(ctx, "sendMessage", map[string]any{"chat_id": bot.chat, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}, nil); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "UPDATE publications SET notified_phase=? WHERE job_id=?", j.Phase, id)
	return err
}
