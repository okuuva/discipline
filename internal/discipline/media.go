//go:build linux

package discipline

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func validateAudio(ctx context.Context, attempt string, tracks int, lease *os.File) error {
	if err := flushTree(attempt); err != nil {
		return err
	}
	statuses, err := filepath.Glob(filepath.Join(attempt, "scratch", "abcde.*", "status"))
	if err != nil || len(statuses) != 1 {
		return errors.New("expected exactly one retained abcde status file")
	}
	statusPath := statuses[0]
	data, err := readRegular(statusPath, 4<<20)
	if err != nil {
		return err
	}
	evidence := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		evidence[scanner.Text()] = true
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !evidence["replaygain"] || !evidence["replaygain-mp3"] {
		return errors.New("missing successful album gain evidence")
	}
	for n := 1; n <= tracks; n++ {
		for _, phase := range []string{"readtrack", "encodetrack", "encodetrack-mp3", "tagtrack", "tagtrack-mp3", "movetrack"} {
			if !evidence[fmt.Sprintf("%s-%02d", phase, n)] {
				return fmt.Errorf("missing successful %s evidence for track %d", phase, n)
			}
		}
	}
	failures, err := readRegular(filepath.Join(filepath.Dir(statusPath), "errors"), 4<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(failures) != 0 {
		return errors.New("abcde recorded processing errors; inspect the retained attempt log")
	}
	count := 0
	err = filepath.WalkDir(filepath.Join(attempt, "media"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 || filepath.Ext(path) != ".mp3" {
			return fmt.Errorf("unexpected media output %q", path)
		}
		cmd := exec.CommandContext(ctx, "lame", "--silent", "--decode", path, os.DevNull)
		if err := runMediaProcess(ctx, cmd, lease); err != nil {
			return fmt.Errorf("MP3 decode failed: %w", err)
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if tracks < 1 || count != tracks {
		return fmt.Errorf("expected %d MP3 tracks, found %d", tracks, count)
	}
	return nil
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("unexpected file type or size: %s", path)
	}
	return os.ReadFile(path)
}

func flushTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected attempt entry: %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		return errors.Join(f.Sync(), f.Close())
	})
}

func storedDisc(j Job) (Disc, error) {
	parts := strings.Fields(j.TOC)
	if len(parts) < 3 {
		return Disc{}, errors.New("job has no recorded TOC")
	}
	tracks, err := strconv.Atoi(parts[0])
	if err != nil || tracks < 1 || tracks > 99 || len(parts) != tracks+2 {
		return Disc{}, errors.New("invalid stored TOC")
	}
	disc := Disc{Kind: "audio"}
	for _, part := range parts[1 : len(parts)-1] {
		offset, err := strconv.Atoi(part)
		if err != nil {
			return Disc{}, err
		}
		disc.Offsets = append(disc.Offsets, offset)
	}
	disc.Leadout, err = strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return Disc{}, err
	}
	key, _, err := disc.Identity()
	if err != nil {
		return Disc{}, err
	}
	if key != j.DiscKey {
		return Disc{}, errors.New("stored TOC does not match disc fingerprint")
	}
	return disc, nil
}
