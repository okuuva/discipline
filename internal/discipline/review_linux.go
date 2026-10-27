package discipline

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var releaseID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func candidateMetadata(attempt string) (string, string) {
	title := "Unknown metadata"
	statuses, err := filepath.Glob(filepath.Join(attempt, "scratch", "abcde.*", "status"))
	if err != nil || len(statuses) != 1 {
		return title, ""
	}
	data, err := readRegular(statuses[0], 4<<20)
	if err != nil {
		return title, ""
	}
	choice := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cddb-choice=") {
			choice = strings.TrimPrefix(line, "cddb-choice=")
		}
	}
	index, err := strconv.Atoi(choice)
	if err != nil || index < 0 || index > 9999 {
		return title, ""
	}
	root := filepath.Dir(statuses[0])
	if data, err := readRegular(filepath.Join(root, "cddbread."+strconv.Itoa(index)), 128<<10); err == nil {
		var value string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "DTITLE=") {
				value += strings.TrimPrefix(line, "DTITLE=")
			}
		}
		value = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return -1
			}
			return r
		}, value)
		runes := []rune(strings.TrimSpace(value))
		if len(runes) > 200 {
			runes = runes[:200]
		}
		if len(runes) > 0 {
			title = string(runes)
		}
	}
	data, err = readRegular(filepath.Join(root, "mbid."+strconv.Itoa(index)), 128)
	if err != nil {
		return title, ""
	}
	id := strings.TrimSpace(string(data))
	if !releaseID.MatchString(id) {
		return title, ""
	}
	return title, "https://coverartarchive.org/release/" + strings.ToLower(id) + "/front"
}

func (s *Store) prepareArtwork(ctx context.Context, spool string, lease *os.File) (bool, error) {
	if _, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO reviews(job_id) SELECT id FROM jobs WHERE phase=?", AwaitingReview); err != nil {
		return false, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT reviews.job_id FROM reviews JOIN jobs ON jobs.id=reviews.job_id
WHERE jobs.phase=? AND reviews.art_attempted=0 ORDER BY jobs.sequence LIMIT 1`, AwaitingReview).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	r, err := s.Review(ctx, id)
	if err != nil {
		return false, err
	}
	j, err := s.claimCover(ctx, id, r.Revision, updateReceipt{})
	if errors.Is(err, ErrConflict) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	attempt := filepath.Join(spool, id)
	title, source := candidateMetadata(attempt)
	var cover, digest string
	workErr := errors.New("no candidate artwork; supply an image in Telegram")
	if source != "" {
		var data []byte
		data, workErr = downloadCover(ctx, source, lease, net.DefaultResolver.LookupNetIP)
		if workErr == nil {
			cover, digest, workErr = convertCover(ctx, attempt, data, lease)
		}
	}
	message := ""
	if workErr != nil {
		message = workErr.Error()
	}
	return true, s.finishCover(context.Background(), j, updateReceipt{}, cover, digest, source, title, message)
}

func (s *Store) notifyReview(ctx context.Context, bot *telegram, spool string) (bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT reviews.job_id FROM reviews JOIN jobs ON jobs.id=reviews.job_id
WHERE jobs.phase=? AND reviews.art_attempted=1 AND reviews.notified_revision!=reviews.revision AND reviews.notify_after<=?
ORDER BY reviews.notify_after,jobs.sequence LIMIT 1`, AwaitingReview, time.Now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	r, err := s.Review(ctx, id)
	if err != nil {
		return false, err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE reviews SET notify_after=? WHERE job_id=? AND revision=?", time.Now().Add(30*time.Second).Unix(), id, r.Revision); err != nil {
		return false, err
	}
	var image []byte
	if r.Cover != "" {
		j, err := s.claimCover(ctx, id, r.Revision, updateReceipt{})
		if errors.Is(err, ErrConflict) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		image, err = coverBytes(filepath.Join(spool, id), r)
		_, releaseErr := s.Transition(context.Background(), j, AwaitingReview, j.Error)
		if err := errors.Join(err, releaseErr); err != nil {
			return false, err
		}
	}
	message, err := bot.upload(ctx, r, image)
	if err != nil {
		return true, err
	}
	return true, s.rememberPreview(ctx, bot.chat, message, r)
}

func (s *Store) applyTelegram(ctx context.Context, bot *telegram, update tgUpdate, spool string, lease *os.File) error {
	delivery := update.receipt()
	if !delivery.valid() {
		return errors.New("invalid Telegram update ID")
	}
	var sender, chat, message int64
	if callback := update.Callback; callback != nil && callback.Message != nil && callback.ID != "" {
		sender, chat, message = callback.From.ID, callback.Message.Chat.ID, callback.Message.ID
	} else if msg := update.Message; msg != nil && msg.Reply != nil && msg.ID > 0 {
		sender, chat, message = msg.From.ID, msg.Chat.ID, msg.Reply.ID
	}
	if chat != bot.chat || !bot.users[sender] {
		return s.ignoreUpdate(ctx, delivery, "unauthorized or uncorrelated")
	}
	var id string
	var revision int64
	err := s.db.QueryRowContext(ctx, "SELECT job_id,revision FROM review_messages WHERE chat_id=? AND message_id=?", chat, message).Scan(&id, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return s.ignoreUpdate(ctx, delivery, "unknown preview")
	}
	if err != nil {
		return err
	}
	if callback := update.Callback; callback != nil {
		if callback.Data != "Owned" && callback.Data != "Loaned" {
			return s.ignoreUpdate(ctx, delivery, "invalid category")
		}
		approved, err := s.approveCover(ctx, id, revision, delivery, callback.Data)
		if err != nil {
			return err
		}
		answer := "This preview is stale or the job is no longer editable."
		if approved {
			answer = "Approved " + callback.Data + "; finalization queued."
		}
		bot.answer(ctx, callback.ID, answer)
		return nil
	}
	j, err := s.claimCover(ctx, id, revision, delivery)
	if errors.Is(err, errUpdateApplied) {
		return nil
	}
	if errors.Is(err, ErrConflict) {
		return s.ignoreUpdate(ctx, delivery, "stale replacement")
	}
	if err != nil {
		return err
	}
	msg := update.Message
	var data []byte
	var source string
	if msg.Document != nil {
		if msg.Document.Size > imageLimit {
			err = errors.New("image exceeds 10 MiB")
		} else {
			data, err = bot.image(ctx, msg.Document.ID)
		}
	} else if len(msg.Photos) > 0 {
		var selected tgFile
		for _, photo := range msg.Photos {
			if photo.Width > 0 && photo.Height > 0 && photo.Height <= 25000000 && photo.Width <= 25000000/photo.Height && photo.Size <= imageLimit && int64(photo.Width)*int64(photo.Height) > int64(selected.Width)*int64(selected.Height) {
				selected = photo
			}
		}
		if selected.ID == "" {
			err = errors.New("no photo fits the image limits")
		} else {
			data, err = bot.image(ctx, selected.ID)
		}
	} else {
		source = strings.TrimSpace(msg.Text)
		data, err = downloadCover(ctx, source, lease, net.DefaultResolver.LookupNetIP)
	}
	var cover, digest string
	if err == nil {
		cover, digest, err = convertCover(ctx, filepath.Join(spool, id), data, lease)
	}
	messageText := ""
	if err != nil {
		messageText = err.Error()
	}
	if err := s.finishCover(context.Background(), j, delivery, cover, digest, source, "", messageText); err != nil {
		return err
	}
	return nil
}

func (s *Store) claimFinalization(ctx context.Context) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE phase=? ORDER BY sequence LIMIT 1", Approved))
	if errors.Is(err, sql.ErrNoRows) {
		var requestID, revision int64
		var id string
		err = tx.QueryRowContext(ctx, "SELECT id,job_id,revision FROM requests WHERE status='queued' AND action='retry' AND phase=? ORDER BY id LIMIT 1", FinalizeFailed).Scan(&requestID, &id, &revision)
		if err != nil {
			return Job{}, err
		}
		j, err = scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=?", id))
		if err != nil {
			return Job{}, err
		}
		status, result := "completed", "finalization claimed"
		if j.Phase != FinalizeFailed || j.Revision != revision {
			status, result = "rejected", "job changed or is busy"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE requests SET status=?,result=? WHERE id=?", status, result, requestID); err != nil {
			return Job{}, err
		}
		if status == "rejected" {
			if err := tx.Commit(); err != nil {
				return Job{}, err
			}
			return Job{}, sql.ErrNoRows
		}
	}
	if err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error='' WHERE id=?", Finalizing, j.ID); err != nil {
		return Job{}, err
	}
	j.Phase, j.Revision, j.Error = Finalizing, j.Revision+1, ""
	return j, tx.Commit()
}

func (s *Store) runFinalizer(ctx context.Context, spool string, lease *os.File) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		j, err := s.claimFinalization(ctx)
		if ctx.Err() != nil && err == ctx.Err() {
			return nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		r, workErr := s.Review(ctx, j.ID)
		var manifest []FileEntry
		if workErr == nil {
			manifest, workErr = finalize(ctx, filepath.Join(spool, j.ID), j, r, lease)
		}
		if workErr != nil {
			log.Printf("finalization %s failed: %v", j.ID, workErr)
		}
		if err := s.finishFinalization(context.Background(), j, manifest, workErr); err != nil {
			return err
		}
	}
}

func (s *Store) runReview(ctx context.Context, bot *telegram, spool string, lease *os.File) (result error) {
	if _, err := s.db.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error='cover work interrupted; previous candidate retained' WHERE phase=?", AwaitingReview, EditingCover); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE jobs SET phase=?,revision=revision+1,error='finalization interrupted; explicit retry required' WHERE phase=?", FinalizeFailed, Finalizing); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finalDone := make(chan error, 1)
	go func() {
		err := s.runFinalizer(ctx, spool, lease)
		finalDone <- err
		if err != nil {
			cancel()
		}
	}()
	defer func() {
		cancel()
		if result == ctx.Err() {
			result = nil
		}
		result = errors.Join(result, <-finalDone)
	}()
	for ctx.Err() == nil {
		worked, err := s.prepareArtwork(ctx, spool, lease)
		if err != nil {
			return err
		}
		notified, err := s.notifyReview(ctx, bot, spool)
		if err != nil && !errors.Is(err, errTelegram) {
			return err
		}
		if err != nil {
			log.Printf("review notification: %v", err)
		}
		offset, err := s.pollOffset(ctx)
		if err != nil {
			return err
		}
		wait := 20
		if worked || notified {
			wait = 0
		}
		updates, err := bot.updates(ctx, offset, wait)
		if err == nil {
			// shortcut: image work is ordered; add a durable input queue if 30s download + 30s conversion delays are too slow for this private bot.
			for _, update := range updates {
				if err := s.applyTelegram(ctx, bot, update, spool, lease); err != nil {
					return err
				}
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("review polling: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
	return nil
}
