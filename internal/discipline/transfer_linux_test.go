package discipline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func copyMedia(ctx context.Context, source, destination, lease *os.File, manifest []FileEntry) error {
	if err := verifyManifest(source, manifest); err != nil {
		return err
	}
	return copyVerifiedMedia(ctx, source, destination, lease, manifest)
}

func fixtureMedia(t *testing.T, root, album string) []FileEntry {
	t.Helper()
	dir := filepath.Join(root, album)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"01.mp3": "fixture compressed audio", "cover.jpg": "fixture approved art"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := mediaManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func pinned(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := openDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestMountContract(t *testing.T) {
	c := StorageConfig{Mount: "/mnt/media", Source: "server:/export", ReadyFile: "/run/storage-ready"}
	good := mountRecord{Target: c.Mount, Source: c.Source, Type: "nfs4", Options: "rw,hard,sec=sys,vers=4.1", ID: 42}
	if err := validateMount(c, good, 42); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*mountRecord){
		func(m *mountRecord) { m.Target = "/" }, func(m *mountRecord) { m.Source = "other:/export" },
		func(m *mountRecord) { m.Type = "ext4" }, func(m *mountRecord) { m.ID = 43 },
		func(m *mountRecord) { m.Options = "ro,hard,sec=sys" }, func(m *mountRecord) { m.Options = "rw,soft,sec=sys" },
		func(m *mountRecord) { m.Options = "rw,hard,sec=krb5" }, func(m *mountRecord) { m.Options = "rw,hard,sec=sys,softreval" },
	} {
		bad := good
		change(&bad)
		if err := validateMount(c, bad, 42); err == nil {
			t.Fatal("accepted wrong mount", bad)
		}
	}
	root := t.TempDir()
	c.Mount = root
	c.ReadyFile = filepath.Join(root, "ready")
	if _, err := c.pin(context.Background()); err == nil {
		t.Fatal("missing marker accepted")
	}
	if err := os.WriteFile(c.ReadyFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.pin(context.Background()); err == nil {
		t.Fatal("uncovered local directory accepted as NFS")
	}
}

func TestPinnedCopyMirrorsOneDiscAndVerifies(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Fatal("packaged rsync is required")
	}
	source, destination, other := t.TempDir(), t.TempDir(), t.TempDir()
	manifest := fixtureMedia(t, source, "New album")
	fixtureMedia(t, destination, "Old album")
	fixtureMedia(t, other, "Unrelated disc")
	src, dst := pinned(t, source), pinned(t, destination)
	if err := copyMedia(context.Background(), src, dst, nil, manifest); err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(dst, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "Old album")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale album retained", err)
	}
	if _, err := os.Stat(filepath.Join(other, "Unrelated disc", "01.mp3")); err != nil {
		t.Fatal("another disc changed", err)
	}
	// Same size and timestamp must not trick rsync into retaining the wrong bytes.
	track := filepath.Join(destination, "New album", "01.mp3")
	info, err := os.Stat(filepath.Join(source, "New album", "01.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(track, bytes.Repeat([]byte("x"), int(info.Size())), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(track, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := copyMedia(context.Background(), src, dst, nil, manifest); err != nil {
		t.Fatal("checksum repair", err)
	}
	// Pinning must not follow a replacement at the old destination path.
	moved := destination + "-moved"
	if err := os.Rename(destination, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(moved) })
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyMedia(context.Background(), src, dst, nil, manifest); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(destination); err != nil || len(entries) != 0 {
		t.Fatal("copy followed the replacement directory", entries, err)
	}
}

func TestCopyRejectsCorruptSourceAndUnsafeDestinationBeforeDeletion(t *testing.T) {
	for _, damage := range []string{"missing", "empty", "corrupt", "source-link", "destination-link", "directory-link", "hardlink", "fifo"} {
		t.Run(damage, func(t *testing.T) {
			source, destination, outside := t.TempDir(), t.TempDir(), t.TempDir()
			manifest := fixtureMedia(t, source, "Album")
			sentinel := filepath.Join(destination, "retain.txt")
			os.WriteFile(sentinel, []byte("retain"), 0600)
			track := filepath.Join(source, "Album", "01.mp3")
			switch damage {
			case "missing":
				os.Remove(track)
			case "empty":
				os.Truncate(track, 0)
			case "corrupt":
				os.WriteFile(track, []byte("bad"), 0600)
			case "source-link":
				os.Remove(track)
				os.Symlink(filepath.Join(outside, "private"), track)
			case "destination-link":
				os.Symlink(filepath.Join(outside, "private"), filepath.Join(destination, "escape.mp3"))
			case "directory-link":
				os.Symlink(outside, filepath.Join(destination, "Album"))
			case "hardlink":
				private := filepath.Join(outside, "private")
				if err := os.WriteFile(private, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(private, filepath.Join(destination, "shared")); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				cmd := exec.Command("mkfifo", filepath.Join(destination, "pipe"))
				if err := cmd.Run(); err != nil {
					t.Fatal(err)
				}
			}
			if err := copyMedia(context.Background(), pinned(t, source), pinned(t, destination), nil, manifest); err == nil {
				t.Fatal("unsafe copy accepted")
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "retain" {
				t.Fatal("preflight changed destination", err)
			}
			if entries, err := os.ReadDir(outside); err != nil || (damage != "hardlink" && len(entries) != 0) {
				t.Fatal("escaped destination", entries, err)
			}
		})
	}
}

func TestSuccessfulExitCannotBypassDestinationVerification(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	manifest := fixtureMedia(t, source, "Album")
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "rsync"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := copyMedia(context.Background(), pinned(t, source), pinned(t, destination), nil, manifest); err == nil {
		t.Fatal("zero exit established success without bytes")
	}
	if _, err := os.Stat(filepath.Join(source, "Album", "01.mp3")); err != nil {
		t.Fatal("failed copy removed source", err)
	}
}

func TestCleanupOnlyRegisteredAttempt(t *testing.T) {
	s, _, spool := testStore(t)
	j, other := job(t, s), job(t, s)
	fixtureMedia(t, filepath.Join(spool, j.ID), "Album")
	fixtureMedia(t, filepath.Join(spool, other.ID), "Other")
	j = step(t, s, step(t, s, j, Abandoned), Cleaning)
	if err := cleanupAttempt(spool, j); err != nil {
		t.Fatal(err)
	}
	if err := cleanupAttempt(spool, j); err != nil {
		t.Fatal("interrupted cleanup retry", err)
	}
	if _, err := os.Stat(filepath.Join(spool, other.ID, "Other", "01.mp3")); err != nil {
		t.Fatal("another attempt was deleted", err)
	}
	if err := s.finishCleanup(context.Background(), j, nil); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Job(context.Background(), j.ID)
	if err != nil || !receipt.Cleaned || receipt.Outcome != Abandoned || receipt.Phase != Abandoned {
		t.Fatal("cleanup lost receipt", receipt, err)
	}
	if err := cleanupAttempt(spool, receipt); err == nil {
		t.Fatal("cleanup ran without ownership")
	}
}

func TestCleanupRejectsSymlinkWithoutFollowingIt(t *testing.T) {
	s, _, spool := testStore(t)
	j := job(t, s)
	outside := t.TempDir()
	fixtureMedia(t, outside, "Private")
	os.Mkdir(filepath.Join(spool, j.ID), 0700)
	os.Symlink(outside, filepath.Join(spool, j.ID, "escape"))
	j = step(t, s, step(t, s, j, Abandoned), Cleaning)
	if err := cleanupAttempt(spool, j); err == nil {
		t.Fatal("unsafe cleanup accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "Private", "01.mp3")); err != nil {
		t.Fatal("cleanup escaped", err)
	}
}
