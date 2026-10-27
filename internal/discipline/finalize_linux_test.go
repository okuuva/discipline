package discipline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureCover(t *testing.T, attempt string, value byte) (string, string, []byte) {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 20, 10))
	for i := range img.Pix {
		img.Pix[i] = value
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatal(err)
	}
	cover, digest, err := convertCover(context.Background(), attempt, input.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(attempt, cover))
	if err != nil {
		t.Fatal(err)
	}
	return cover, digest, data
}

func TestFinalizationReplacesWrongArtAndPreservesAudio(t *testing.T) {
	for _, tool := range []string{"magick", "lame", "rsgain", "eyeD3", "ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	s, _, spool := testStore(t)
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
	j = step(t, s, step(t, s, j, Ripping), AwaitingReview)
	if _, err := s.db.Exec("INSERT INTO reviews(job_id) VALUES(?)", id); err != nil {
		t.Fatal(err)
	}
	attempt := filepath.Join(spool, id)
	album := filepath.Join(attempt, "media", "Artist", "2026_-_Album")
	if err := os.MkdirAll(album, 0700); err != nil {
		t.Fatal(err)
	}
	wave := filepath.Join(attempt, "input.wav")
	writeWave(t, wave)
	track := filepath.Join(album, "01_-_Track.mp3")
	run := func(name string, args ...string) {
		t.Helper()
		if output, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %s %v", name, output, err)
		}
	}
	run("lame", "--silent", "--preset", "insane", wave, track)
	original, err := audioHash(ctx, track, nil)
	if err != nil {
		t.Fatal(err)
	}
	run("rsgain", "custom", "--album", "--tagmode=i", track)
	gained, err := audioHash(ctx, track, nil)
	if err != nil || gained != original {
		t.Fatal("gain tagging changed audio frames", err)
	}
	wrong, _, wrongBytes := fixtureCover(t, attempt, 0)
	run("eyeD3", "--no-config", "--add-image", filepath.Join(attempt, wrong)+":FRONT_COVER", track)
	if err := os.WriteFile(filepath.Join(album, "cover.jpg"), wrongBytes, 0600); err != nil {
		t.Fatal(err)
	}
	chosen, digest, chosenBytes := fixtureCover(t, attempt, 255)
	if bytes.Equal(chosenBytes, wrongBytes) {
		t.Fatal("fixture images must differ")
	}
	claim, err := s.claimCover(ctx, id, 0, testUpdate(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.finishCover(ctx, claim, testUpdate(1), chosen, digest, "https://example.com/chosen", "Fixture album", ""); err != nil {
		t.Fatal(err)
	}
	r, err := s.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if yes, err := s.approveCover(ctx, id, r.Revision, testUpdate(2), "Owned"); err != nil || !yes {
		t.Fatal("approval failed", err)
	}
	j, err = s.claimFinalization(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := os.ReadFile(track)
	if err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	damaged := filepath.Join(tools, "damaged.mp3")
	run("lame", "--silent", "-b", "64", wave, damaged)
	if err := os.WriteFile(filepath.Join(tools, "eyeD3"), []byte(fmt.Sprintf("#!/bin/sh\nfor last; do :; done\ncp %q \"$last\"\n", damaged)), 0700); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", tools+string(os.PathListSeparator)+originalPath)
	_, workErr := finalize(ctx, attempt, j, r, nil)
	if workErr == nil || !strings.Contains(workErr.Error(), "changed compressed audio") {
		t.Fatal("readable tagger corruption not detected", workErr)
	}
	unchanged, err := os.ReadFile(track)
	if err != nil || !bytes.Equal(unchanged, originalBytes) {
		t.Fatal("failed tag write damaged the retry baseline", err)
	}
	if err := s.finishFinalization(ctx, j, nil, workErr); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: id, Action: "retry"}); err != nil {
		t.Fatal(err)
	}
	j, err = s.claimFinalization(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", originalPath)
	manifest, err := finalize(ctx, attempt, j, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 2 {
		t.Fatalf("unexpected frozen media: %+v", manifest)
	}
	if err := verifyEmbeddedCover(ctx, attempt, track, chosenBytes, nil); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(album, "cover.jpg"))
	if err != nil || !bytes.Equal(actual, chosenBytes) {
		t.Fatal("sidecar was not replaced", err)
	}
	encoded, err := os.ReadFile(track)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK", "REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK"} {
		if !bytes.Contains(encoded, []byte(tag)) {
			t.Fatal("cover tagging lost gain metadata", tag)
		}
	}
	finalHash, err := audioHash(ctx, track, nil)
	if err != nil || finalHash != original {
		t.Fatal("artwork changed audio frames", err)
	}
	// Model a crash after the files changed but before the ready receipt committed.
	if err := s.finishFinalization(ctx, j, nil, errors.New("interrupted before receipt")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, Request{JobID: id, Action: "retry"}); err != nil {
		t.Fatal(err)
	}
	j, err = s.claimFinalization(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = finalize(ctx, attempt, j, r, nil)
	if err != nil {
		t.Fatal("idempotent finalization retry", err)
	}
	if err := s.finishFinalization(ctx, j, manifest, nil); err != nil {
		t.Fatal(err)
	}
	ready, err := s.Job(ctx, id)
	if err != nil || ready.Phase != Ready {
		t.Fatal("finalization not committed", ready, err)
	}
	r, err = s.Review(ctx, id)
	if err != nil || len(r.Manifest) != 2 || r.Category != "Owned" || r.Digest != digest {
		t.Fatal("approval or manifest changed", r, err)
	}
}

func TestManifestAndCoverRejectSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "track.mp3")); err != nil {
		t.Fatal(err)
	}
	if _, err := mediaManifest(root); err == nil {
		t.Fatal("manifest followed a symlink")
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "review")); err != nil {
		t.Fatal(err)
	}
	id, _ := newJobID()
	if _, err := coverBytes(root, Review{Cover: "review/cover-" + id + ".jpg"}); err == nil {
		t.Fatal("cover followed a symlink directory")
	}
	if _, err := coverBytes(root, Review{Cover: outside}); err == nil {
		t.Fatal("accepted an absolute cover path")
	}
}
