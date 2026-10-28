package discipline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureToken = "12345:private-fixture-token-not-a-real-secret"

func testBot(t *testing.T, handler http.HandlerFunc) *telegram {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(fixtureToken), 0600); err != nil {
		t.Fatal(err)
	}
	bot, err := newTelegram(TelegramConfig{TokenFile: file, ChatID: 42, Users: []int64{7}})
	if err != nil {
		t.Fatal(err)
	}
	bot.base = server.URL
	t.Cleanup(bot.client.CloseIdleConnections)
	return bot
}

func TestTelegramAuthorizationCorrelationAndReplay(t *testing.T) {
	s, _, spool := testStore(t)
	ctx := context.Background()
	first, second := pendingReview(t, s), pendingReview(t, s)
	a, b := fakeCandidate(t, s, first), fakeCandidate(t, s, second)
	for message, r := range map[int64]Review{100: a, 200: b} {
		if err := s.rememberPreview(ctx, 42, message, r); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int64
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"ok":true,"result":true}`) })
	action := func(update, sender, chat, message int64, category string) {
		t.Helper()
		u := tgUpdate{ID: update, Callback: &tgCallback{ID: fmt.Sprintf("callback-%d", update), From: tgUser{ID: sender}, Message: &tgMessage{ID: message, Chat: tgChat{ID: chat}}, Data: category}}
		if err := s.applyTelegram(ctx, bot, u, spool, nil); err != nil {
			t.Fatal(err)
		}
	}
	action(1, 8, 42, 100, "Owned")
	action(2, 7, -42, 100, "Owned")
	action(3, 7, 42, 999, "Owned")
	if calls.Load() != 0 {
		t.Fatal("unauthorized/unknown actions triggered provider calls")
	}
	action(4, 7, 42, 200, "Loaned")
	unchanged, err := s.Review(ctx, first.ID)
	if err != nil || unchanged.Category != "" {
		t.Fatal("action affected the wrong job", unchanged, err)
	}
	approved, err := s.Review(ctx, second.ID)
	if err != nil || approved.Category != "Loaned" {
		t.Fatal("target job not approved", approved, err)
	}
	claim, err := s.claimCover(ctx, first.ID, a.Revision, testUpdate(5))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishCover(ctx, claim, testUpdate(5), a.Cover, strings.Repeat("b", 64), a.Source, "", ""); err != nil {
		t.Fatal(err)
	}
	action(6, 7, 42, 100, "Owned")
	current, err := s.Review(ctx, first.ID)
	if err != nil || current.Category != "" {
		t.Fatal("old preview approved replacement", current, err)
	}
	if err := s.rememberPreview(ctx, 42, 101, current); err != nil {
		t.Fatal(err)
	}
	action(7, 7, 42, 101, "Owned")
	action(7, 7, 42, 101, "Loaned")
	current, err = s.Review(ctx, first.ID)
	if err != nil || current.Category != "Owned" {
		t.Fatal("duplicate update changed approval", current, err)
	}
}

func TestTelegramUploadedReplacementAndFailure(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not available")
	}
	s, _, spool := testStore(t)
	ctx := context.Background()
	j := pendingReview(t, s)
	old := fakeCandidate(t, s, j)
	if err := os.Mkdir(filepath.Join(spool, j.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.rememberPreview(ctx, 42, 100, old); err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewGray(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	var bad atomic.Bool
	var unsafePath atomic.Bool
	var oversized atomic.Bool
	var previewFailure atomic.Bool
	var downloads atomic.Int64
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendPhoto"):
			if previewFailure.Load() {
				http.Error(w, "retry", 503)
				return
			}
			if err := r.ParseMultipartForm(imageLimit); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if !strings.Contains(r.FormValue("caption"), "Last replacement failed") {
				t.Error("replacement rejection was not included in the retried preview")
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":300,"chat":{"id":42}}}`)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			name := "documents/fixture.png"
			if unsafePath.Load() {
				name = "../secret"
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"file_path": name, "file_size": input.Len()}})
		case strings.Contains(r.URL.Path, "/file/bot"):
			downloads.Add(1)
			if oversized.Load() {
				w.Write(make([]byte, imageLimit+1))
			} else if bad.Load() {
				fmt.Fprint(w, "not an image")
			} else {
				w.Write(input.Bytes())
			}
		default:
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":300,"chat":{"id":42}}}`)
		}
	})
	replace := func(update, reply int64) {
		t.Helper()
		u := tgUpdate{ID: update, Message: &tgMessage{ID: 1000 + update, From: tgUser{ID: 7}, Chat: tgChat{ID: 42}, Reply: &tgMessage{ID: reply}, Document: &tgFile{ID: "uploaded-file", Size: int64(input.Len())}}}
		if err := s.applyTelegram(ctx, bot, u, spool, nil); err != nil {
			t.Fatal(err)
		}
	}
	replace(1, 100)
	current, err := s.Review(ctx, j.ID)
	if err != nil || current.Revision != old.Revision+1 || current.Source != "" || current.Cover == old.Cover {
		t.Fatal("uploaded image not staged as a new revision", current, err)
	}
	imageBytes, err := coverBytes(filepath.Join(spool, j.ID), current)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.rememberPreview(ctx, 42, 101, current); err != nil {
		t.Fatal(err)
	}
	bad.Store(true)
	replace(2, 101)
	unchanged, err := s.Review(ctx, j.ID)
	if err != nil || unchanged.Revision != current.Revision || unchanged.Digest != current.Digest || unchanged.NotifiedRevision != -1 || strings.Contains(unchanged.Error, fixtureToken) {
		t.Fatal("invalid replacement changed candidate or exposed token", unchanged, err)
	}
	data, err := coverBytes(filepath.Join(spool, j.ID), unchanged)
	if err != nil || !bytes.Equal(data, imageBytes) {
		t.Fatal("previous candidate bytes changed", err)
	}
	previewFailure.Store(true)
	if _, err := s.notifyReview(ctx, bot, spool); err == nil {
		t.Fatal("expected notification failure")
	}
	previewFailure.Store(false)
	if _, err := s.db.Exec("UPDATE reviews SET notify_after=0 WHERE job_id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.notifyReview(ctx, bot, spool); err != nil {
		t.Fatal("replacement notice was not retried", err)
	}
	oversized.Store(true)
	if _, err := bot.image(ctx, "file"); err == nil {
		t.Fatal("accepted oversized chunked Telegram body")
	}
	unsafePath.Store(true)
	before := downloads.Load()
	if _, err := bot.image(ctx, "file"); err == nil || strings.Contains(err.Error(), fixtureToken) {
		t.Fatal("unsafe file URL accepted or token exposed", err)
	}
	if downloads.Load() != before {
		t.Fatal("downloaded an unsafe Telegram file path")
	}
}

func TestPhotoPreviewContainsExactImageAndPublicSource(t *testing.T) {
	for field, width := range map[string]int{"photo": 20, "document": 1000} {
		t.Run(field, func(t *testing.T) {
			var imageData bytes.Buffer
			if err := jpeg.Encode(&imageData, image.NewGray(image.Rect(0, 0, width, 1)), nil); err != nil {
				t.Fatal(err)
			}
			imageBytes := imageData.Bytes()
			r := Review{JobID: "fixture-job", Title: "Fixture album", Source: "https://example.com/cover", Error: "private failure details"}
			bot := testBot(t, func(w http.ResponseWriter, request *http.Request) {
				method := map[string]string{"photo": "sendPhoto", "document": "sendDocument"}[field]
				if !strings.HasSuffix(request.URL.Path, "/"+method) {
					t.Error("wrong Telegram attachment method", request.URL.Path)
				}
				if err := request.ParseMultipartForm(imageLimit); err != nil {
					t.Error(err)
					return
				}
				defer request.MultipartForm.RemoveAll()
				caption := request.FormValue("caption")
				if !strings.HasPrefix(caption, "Source\n") || !strings.Contains(caption, r.JobID) || !strings.Contains(caption, "Ripped, awaiting review") || !strings.Contains(caption, "Last replacement failed") || strings.Contains(caption, r.Error) {
					t.Errorf("unsafe or incomplete caption: %s", caption)
				}
				var entities []struct {
					Type   string `json:"type"`
					Offset int    `json:"offset"`
					Length int    `json:"length"`
					URL    string `json:"url"`
				}
				if err := json.Unmarshal([]byte(request.FormValue("caption_entities")), &entities); err != nil || len(entities) != 1 || entities[0].URL != r.Source || entities[0].Offset != 0 || entities[0].Length != 6 {
					t.Errorf("wrong source link: %+v %v", entities, err)
				}
				file, header, err := request.FormFile(field)
				if err != nil {
					t.Error(err)
					return
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil || header.Filename != "cover.jpg" || !bytes.Equal(data, imageBytes) {
					t.Error("preview did not contain exact candidate", err)
				}
				if len(request.MultipartForm.File) != 1 || strings.Contains(caption, fixtureToken) {
					t.Error("unexpected attachment or credential disclosure")
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":99,"chat":{"id":42}}}`)
			})
			if id, err := bot.upload(context.Background(), r, imageBytes); err != nil || id != 99 {
				t.Fatal("preview failed", id, err)
			}
		})
	}
}

func TestPreviewRetryAndTokenRedaction(t *testing.T) {
	s, _, spool := testStore(t)
	ctx := context.Background()
	j := pendingReview(t, s)
	if _, err := s.db.Exec("UPDATE reviews SET art_attempted=1 WHERE job_id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	var failing atomic.Bool
	failing.Store(true)
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, fixtureToken, http.StatusBadGateway)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		text, _ := payload["text"].(string)
		if !strings.Contains(text, j.ID) || !strings.Contains(text, "Ripped, awaiting review") || strings.Contains(text, spool) || strings.Contains(text, fixtureToken) {
			t.Errorf("unsafe or incomplete notification: %s", text)
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":50,"chat":{"id":42}}}`)
	})
	if _, err := s.notifyReview(ctx, bot, spool); err == nil || strings.Contains(err.Error(), fixtureToken) {
		t.Fatal("failed notification accepted or leaked token", err)
	}
	r, err := s.Review(ctx, j.ID)
	if err != nil || r.NotifiedRevision != -1 {
		t.Fatal("failed notification lost pending review", r, err)
	}
	failing.Store(false)
	if _, err := s.db.Exec("UPDATE reviews SET notify_after=0 WHERE job_id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.notifyReview(ctx, bot, spool); err != nil {
		t.Fatal(err)
	}
	var associated string
	if err := s.db.QueryRow("SELECT job_id FROM review_messages WHERE chat_id=42 AND message_id=50").Scan(&associated); err != nil || associated != j.ID {
		t.Fatal("preview association missing", associated, err)
	}
	bot.base = "https://127.0.0.1:1"
	if err := bot.call(ctx, "getMe", map[string]any{}, nil); err == nil || strings.Contains(err.Error(), fixtureToken) {
		t.Fatal("transport error exposed credentials", err)
	}
}

func TestFailedPreviewDoesNotBlockUpdatesOrOtherJobs(t *testing.T) {
	s, _, spool := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shown := pendingReview(t, s)
	r := fakeCandidate(t, s, shown)
	if err := s.rememberPreview(ctx, 42, 100, r); err != nil {
		t.Fatal(err)
	}
	failing, later := pendingReview(t, s), pendingReview(t, s)
	if _, err := s.db.Exec("UPDATE reviews SET art_attempted=1 WHERE job_id IN (?,?)", failing.ID, later.ID); err != nil {
		t.Fatal(err)
	}
	var polls atomic.Int64
	progress := make(chan struct{}, 1)
	bot := testBot(t, func(w http.ResponseWriter, request *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/sendMessage"):
			var text string
			json.Unmarshal(payload["text"], &text)
			if strings.Contains(text, failing.ID) {
				http.Error(w, "rejected preview", 400)
				return
			}
			if !strings.Contains(text, later.ID) {
				t.Error("wrong pending preview", text)
			}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":200,"chat":{"id":42}}}`)
		case strings.HasSuffix(request.URL.Path, "/getUpdates"):
			if polls.Add(1) == 1 {
				update := tgUpdate{ID: 1, Callback: &tgCallback{ID: "approve", From: tgUser{ID: 7}, Message: &tgMessage{ID: 100, Chat: tgChat{ID: 42}}, Data: "Owned"}}
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []tgUpdate{update}})
			} else {
				fmt.Fprint(w, `{"ok":true,"result":[]}`)
				select {
				case progress <- struct{}{}:
				default:
				}
			}
		default:
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		}
	})
	done := make(chan error, 1)
	go func() { done <- s.runReview(ctx, bot, spool, nil) }()
	select {
	case <-progress:
	case <-time.After(10 * time.Second):
		t.Fatal("failed preview blocked polling or the next notification")
	}
	approved, err := s.Review(ctx, shown.ID)
	if err != nil || approved.Category != "Owned" {
		t.Fatal("pending approval was not consumed", approved, err)
	}
	next, err := s.Review(ctx, later.ID)
	if err != nil || next.NotifiedRevision != next.Revision {
		t.Fatal("later preview was starved", next, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review did not stop")
	}
}

func TestPublicationNoticeMustSucceedBeforeAutomaticCleanup(t *testing.T) {
	s, _, spool := testStore(t)
	j := readyPublication(t, s, spool, Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450})
	j, err := s.claimPublication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishPublication(context.Background(), j, nil); err != nil {
		t.Fatal(err)
	}
	var failing atomic.Bool
	failing.Store(true)
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		text, _ := payload["text"].(string)
		if !strings.Contains(text, j.ID) || !strings.Contains(text, "transfer complete") || strings.Contains(text, spool) {
			t.Error("wrong publication notice", text)
		}
		if failing.Load() {
			http.Error(w, "temporary failure", 503)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	})
	if err := s.notifyPublication(context.Background(), bot); err == nil {
		t.Fatal("notification failure lost")
	}
	if _, err := s.claimCleanup(context.Background(), false); err == nil {
		t.Fatal("deleted before notifying")
	}
	failing.Store(false)
	if _, err := s.db.Exec("UPDATE publications SET notify_after=0"); err != nil {
		t.Fatal(err)
	}
	if err := s.notifyPublication(context.Background(), bot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimCleanup(context.Background(), false); err != nil {
		t.Fatal("successful notice did not release cleanup", err)
	}
}

func TestDelayedOldReceiptCannotAdvertiseNewerPartialInbox(t *testing.T) {
	s, _, spool := testStore(t)
	disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
	old := readyPublication(t, s, spool, disc)
	claim, err := s.claimPublication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishPublication(context.Background(), claim, nil); err != nil {
		t.Fatal(err)
	}
	readyPublication(t, s, spool, disc)
	claim, err = s.claimPublication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishPublication(context.Background(), claim, fmt.Errorf("partial newer copy")); err != nil {
		t.Fatal(err)
	}
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		text, _ := payload["text"].(string)
		if !strings.Contains(text, old.ID) || !strings.Contains(text, "historical") || strings.Contains(text, "ready for manual import") {
			t.Error("old receipt advertised a partial current destination", text)
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	if err := s.notifyPublication(context.Background(), bot); err != nil {
		t.Fatal(err)
	}
}

func TestStalledPublicationNoticeDoesNotDelayReview(t *testing.T) {
	s, _, spool := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readyPublication(t, s, spool, Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450})
	claim, err := s.claimPublication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishPublication(ctx, claim, nil); err != nil {
		t.Fatal(err)
	}
	pending := pendingReview(t, s)
	review := fakeCandidate(t, s, pending)
	if err := s.rememberPreview(ctx, 42, 100, review); err != nil {
		t.Fatal(err)
	}
	stalled := make(chan struct{})
	approved := make(chan struct{}, 1)
	var sent atomic.Bool
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			close(stalled)
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			select {
			case <-stalled:
			case <-r.Context().Done():
				return
			}
			if !sent.Swap(true) {
				update := tgUpdate{ID: 1, Callback: &tgCallback{ID: "approve-during-notice", From: tgUser{ID: 7}, Message: &tgMessage{ID: 100, Chat: tgChat{ID: 42}}, Data: "Owned"}}
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []tgUpdate{update}})
			} else {
				fmt.Fprint(w, `{"ok":true,"result":[]}`)
			}
		default:
			fmt.Fprint(w, `{"ok":true,"result":true}`)
			select {
			case approved <- struct{}{}:
			default:
			}
		}
	})
	done := make(chan error, 1)
	go func() { done <- s.runReview(ctx, bot, spool, nil) }()
	select {
	case <-approved:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled publication notice blocked approval polling")
	}
	got, err := s.Review(ctx, pending.ID)
	if err != nil || got.Category != "Owned" {
		t.Fatal(got, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notice worker did not stop")
	}
}

func TestCandidateMetadataUsesChosenRelease(t *testing.T) {
	attempt := t.TempDir()
	root := filepath.Join(attempt, "scratch", "abcde.fixture")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	id := "12345678-1234-1234-1234-123456789abc"
	for name, data := range map[string]string{"status": "cddb-choice=1\n", "cddbread.1": "DTITLE=Artist\u202e / Album\n", "mbid.1": id, "mbid.2": "ffffffff-ffff-ffff-ffff-ffffffffffff"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	title, source := candidateMetadata(attempt)
	if title != "Artist / Album" || source != "https://coverartarchive.org/release/"+id+"/front" {
		t.Fatal("wrong candidate provenance", title, source)
	}
	if err := os.WriteFile(filepath.Join(root, "status"), []byte("cddb-choice=../../private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, source := candidateMetadata(attempt); source != "" {
		t.Fatal("accepted unsafe metadata selection")
	}
}

func TestReviewRecoveryAndLongPollDoesNotOwnDatabase(t *testing.T) {
	s, _, spool := testStore(t)
	j := pendingReview(t, s)
	old := fakeCandidate(t, s, j)
	if _, err := s.claimCover(context.Background(), j.ID, old.Revision, testUpdate(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE reviews SET notified_revision=revision WHERE job_id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	polling := make(chan struct{}, 1)
	bot := testBot(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		select {
		case polling <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.runReview(ctx, bot, spool, nil) }()
	select {
	case <-polling:
	case <-time.After(5 * time.Second):
		t.Fatal("poller did not start")
	}
	checkCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	id, err := s.ObserveDisc(checkCtx, Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450})
	if err != nil || id == "" {
		t.Fatal("stalled Telegram blocked a new insertion", id, err)
	}
	got, err := s.Job(checkCtx, j.ID)
	if err != nil || got.Phase != AwaitingReview {
		t.Fatal("interrupted cover work not recovered", got, err)
	}
	r, err := s.Review(checkCtx, j.ID)
	if err != nil || r.Digest != old.Digest || r.Revision != old.Revision {
		t.Fatal("recovery changed candidate", r, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review did not stop")
	}
}
