package discipline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func coverBytes(attempt string, r Review) ([]byte, error) {
	name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(r.Cover), "cover-"), ".jpg")
	if !uuidPattern.MatchString(name) || r.Cover != filepath.Join("review", "cover-"+name+".jpg") {
		return nil, errors.New("invalid stored cover path")
	}
	for _, dir := range []string{attempt, filepath.Join(attempt, "review")} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return nil, errors.New("cover directory missing or unsafe")
		}
	}
	data, err := readRegular(filepath.Join(attempt, r.Cover), imageLimit)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != r.Digest {
		return nil, errors.New("stored cover digest mismatch")
	}
	return data, nil
}

func audioHash(ctx context.Context, file string, lease *os.File) (string, error) {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", errors.New("audio file is missing or unsafe")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", file, "-map", "0:a:0", "-c", "copy", "-f", "streamhash", "-hash", "sha256", "-")
	output := &boundedBuffer{limit: 1024}
	cmd.Stdout = output
	if err := runMediaProcess(ctx, cmd, lease); err != nil {
		return "", errors.New("cannot hash compressed MP3 packets")
	}
	value := strings.TrimSpace(output.String())
	if len(value) != len("0,a,SHA256=")+64 || !strings.HasPrefix(value, "0,a,SHA256=") {
		return "", errors.New("invalid compressed audio hash")
	}
	return value, nil
}

func mediaInventory(root string) ([]FileEntry, error) {
	var entries []FileEntry
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 || (filepath.Ext(file) != ".mp3" && filepath.Base(file) != "cover.jpg") {
			return fmt.Errorf("unexpected media file: %s", file)
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		entries = append(entries, FileEntry{Path: rel, Size: info.Size()})
		return nil
	})
	if err == nil && len(entries) == 0 {
		err = errors.New("empty media manifest")
	}
	return entries, err
}

func mediaManifest(root string) ([]FileEntry, error) {
	entries, err := mediaInventory(root)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		f, err := os.Open(filepath.Join(root, entry.Path))
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, f)
		if err := errors.Join(readErr, f.Close()); err != nil {
			return nil, err
		}
		if n != entry.Size {
			return nil, errors.New("media file changed during hashing")
		}
		entries[i].SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	}
	return entries, nil
}

func finalize(ctx context.Context, attempt string, j Job, r Review, lease *os.File) ([]FileEntry, error) {
	if j.Phase != Finalizing || (r.Category != "Owned" && r.Category != "Loaned") {
		return nil, errors.New("job has no frozen approval")
	}
	cover, err := coverBytes(attempt, r)
	if err != nil {
		return nil, err
	}
	disc, err := storedDisc(j)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(attempt, "media")
	files, err := mediaInventory(root)
	if err != nil {
		return nil, err
	}
	tracks := 0
	albums := map[string]bool{}
	for _, file := range files {
		if filepath.Ext(file.Path) == ".mp3" {
			tracks++
			albums[filepath.Dir(file.Path)] = true
		}
	}
	if tracks != len(disc.Offsets) {
		return nil, errors.New("finalized track count differs from recorded TOC")
	}
	for _, file := range files {
		if !albums[filepath.Dir(file.Path)] {
			return nil, errors.New("sidecar has no album tracks")
		}
	}
	for _, file := range files {
		if filepath.Ext(file.Path) == ".mp3" {
			if err := tagTrack(ctx, attempt, filepath.Join(root, file.Path), r.Cover, cover, lease); err != nil {
				return nil, err
			}
		}
	}
	for album := range albums {
		sidecar := filepath.Join(root, album, "cover.jpg")
		temp, err := os.CreateTemp(attempt, "cover-sidecar-*")
		if err != nil {
			return nil, err
		}
		_, writeErr := temp.Write(cover)
		err = errors.Join(writeErr, temp.Sync(), temp.Close())
		if err == nil {
			err = os.Rename(temp.Name(), sidecar)
		}
		os.Remove(temp.Name())
		if err != nil {
			return nil, err
		}
		data, err := readRegular(sidecar, imageLimit)
		if err != nil || !bytes.Equal(data, cover) {
			return nil, errors.Join(errors.New("sidecar verification failed"), err)
		}
	}
	if err := flushTree(root); err != nil {
		return nil, err
	}
	return mediaManifest(root)
}

// Never let an interrupted or faulty tag write become a retry's audio baseline.
func tagTrack(ctx context.Context, attempt, track, coverPath string, cover []byte, lease *os.File) error {
	before, err := audioHash(ctx, track, lease)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(attempt, "tagged-*.mp3")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	source, err := os.Open(track)
	if err != nil {
		temp.Close()
		return err
	}
	_, copyErr := io.Copy(temp, source)
	if err := errors.Join(copyErr, source.Close(), temp.Close()); err != nil {
		return err
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	cmd := exec.CommandContext(cmdCtx, "eyeD3", "--no-config", "--remove-all-images", "--add-image", coverPath+":FRONT_COVER", temp.Name())
	cmd.Dir = attempt
	err = runMediaProcess(cmdCtx, cmd, lease)
	cancel()
	if err != nil {
		return errors.New("front-cover tagging failed")
	}
	after, err := audioHash(ctx, temp.Name(), lease)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("cover tagging changed compressed audio packets")
	}
	if err := verifyEmbeddedCover(ctx, attempt, temp.Name(), cover, lease); err != nil {
		return err
	}
	if err := flushTree(temp.Name()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), track)
}

func verifyEmbeddedCover(ctx context.Context, attempt, track string, cover []byte, lease *os.File) error {
	dir, err := os.MkdirTemp(attempt, "verify-cover-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "eyeD3", "--no-config", "--write-images", dir, track)
	if err := runMediaProcess(ctx, cmd, lease); err != nil {
		return errors.New("embedded cover extraction failed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != "FRONT_COVER.jpg" {
		return errors.New("expected one embedded front cover")
	}
	data, err := readRegular(filepath.Join(dir, entries[0].Name()), imageLimit)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, cover) {
		return errors.New("embedded image differs from the approved cover")
	}
	return nil
}
