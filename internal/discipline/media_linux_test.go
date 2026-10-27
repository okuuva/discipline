package discipline

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireBackend(t *testing.T, name string) string {
	t.Helper()
	path := os.Getenv(name)
	if path == "" {
		t.Skip("pinned media backend is supplied by the Nix package check")
	}
	return path
}

func TestPinnedMetadataQuoting(t *testing.T) {
	backend := requireBackend(t, "DISCIPLINE_TEST_CDDB")
	fixture := filepath.Join(t.TempDir(), "metadata")
	payload := "$(printf UNEXPECTED-SHELL-EVALUATION >&2)"
	data := "DISCID=" + payload + "\nDTITLE=Artist / Album\n#CATEGORY=" + payload + "\nDYEAR=" + payload + "\nDGENRE=" + payload + "\nTTITLE0=Track\n"
	if err := os.WriteFile(fixture, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `eval "$("$1" parse "$2")"; printf '%s\n' "$DISCID" "$CDDBGENRE" "$CDYEAR" "$CDGENRE"`, "--", backend, fixture)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || stderr.Len() != 0 || string(out) != strings.Repeat(payload+"\n", 4) {
		t.Fatalf("metadata was evaluated or changed: %q stderr=%q err=%v", out, &stderr, err)
	}
}

func TestPinnedSingleTrackID(t *testing.T) {
	backend := requireBackend(t, "DISCIPLINE_TEST_MUSICBRAINZ")
	out, err := exec.Command(backend, "--command", "calcid", "--discinfo", "1", "1", "150", "300", "0").CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(out))) != 28 {
		t.Fatalf("single-track ID: %q %v", out, err)
	}
	if err := exec.Command(backend, "--command", "calcid", "--discinfo", "1", "1", "150", "300").Run(); err == nil {
		t.Fatal("accepted incomplete TOC")
	}
}

func TestPinnedUnknownDiscExtraction(t *testing.T) {
	requireBackend(t, "DISCIPLINE_TEST_ABCDE")
	for _, failGain := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "gain-failure"}[failGain], func(t *testing.T) {
			s, state, spool := testStore(t)
			disc := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450}
			id, err := s.ObserveDisc(context.Background(), disc)
			if err != nil {
				t.Fatal(err)
			}
			j, err := s.Job(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			fixtures := t.TempDir()
			wave := filepath.Join(fixtures, "fixture.wav")
			writeWave(t, wave)
			bash, err := exec.LookPath("bash")
			if err != nil {
				t.Fatal(err)
			}
			stub := func(name, content string) string {
				path := filepath.Join(fixtures, name)
				if err := os.WriteFile(path, []byte("#!"+bash+"\n"+content+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				return path
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			discid := stub("disc-id", "printf '%s\\n' '02000401 1 150 6'")
			reader := stub("reader", "case \" $* \" in *' -Q '*) printf '%s\\n' ' 1. 300 [00:04.00] 0 [00:00.00]' 'TOTAL 300 [00:04.00]'; exit 0;; esac\nfor last; do :; done\ncp "+quote(wave)+" \"$last\"")
			config, err := os.ReadFile("../../data/abcde.conf")
			if err != nil {
				t.Fatal(err)
			}
			config = append(config, []byte("\nCDDBMETHOD=cddb\nNOCDDBQUERY=y\nCDDISCID="+quote(discid)+"\nCDPARANOIA="+quote(reader)+"\n")...)
			if failGain {
				config = append(config, []byte("MP3GAIN="+quote(stub("fail-gain", "exit 17"))+"\n")...)
			}
			cfgPath := filepath.Join(fixtures, "abcde.conf")
			if err := os.WriteFile(cfgPath, config, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := ServiceConfig{State: state, Spool: spool, Drive: "/dev/null", ABCDEConfig: cfgPath}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			err = extract(ctx, cfg, j, nil)
			attempt := filepath.Join(spool, id)
			if failGain {
				if err == nil || !strings.Contains(err.Error(), "missing successful album gain evidence") {
					output, _ := os.ReadFile(filepath.Join(attempt, "abcde.log"))
					t.Fatalf("expected zero-exit abcde with failed gain, got %v\n%s", err, output)
				}
				if _, statErr := os.Stat(filepath.Join(attempt, "media")); statErr != nil {
					t.Fatal("lost failed output", statErr)
				}
				return
			}
			if err != nil {
				output, _ := os.ReadFile(filepath.Join(attempt, "abcde.log"))
				t.Fatalf("unknown-disc extraction: %v\n%s", err, output)
			}
			files, err := filepath.Glob(filepath.Join(attempt, "media", "Unknown_Artist", "_-_Unknown_Album", "*.mp3"))
			if err != nil || len(files) != 1 {
				t.Fatalf("preserved naming: %v %v", files, err)
			}
			encoded, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, tag := range []string{"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK", "REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK"} {
				if !bytes.Contains(encoded, []byte(tag)) {
					t.Fatalf("missing rsgain tag %s", tag)
				}
			}
			if err := validateAudio(ctx, attempt, 2, nil); err == nil {
				t.Fatal("accepted missing track")
			}
			statuses, _ := filepath.Glob(filepath.Join(attempt, "scratch", "abcde.*", "status"))
			status, err := os.ReadFile(statuses[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statuses[0], []byte(strings.ReplaceAll(string(status), "readtrack-01\n", "")), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateAudio(ctx, attempt, 1, nil); err == nil {
				t.Fatal("accepted missing processing evidence")
			}
			if err := os.WriteFile(statuses[0], status, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(files[0]); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(wave, files[0]); err != nil {
				t.Fatal(err)
			}
			if err := validateAudio(ctx, attempt, 1, nil); err == nil {
				t.Fatal("accepted symlink output")
			}
		})
	}
}

func writeWave(t *testing.T, path string) {
	t.Helper()
	var b bytes.Buffer
	frames := 44100 * 4
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+frames*4))
	b.WriteString("WAVEfmt ")
	for _, value := range []any{uint32(16), uint16(1), uint16(2), uint32(44100), uint32(44100 * 4), uint16(4), uint16(16)} {
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(frames*4))
	for i := 0; i < frames; i++ {
		sample := int16(12000 * math.Sin(2*math.Pi*440*float64(i)/44100))
		binary.Write(&b, binary.LittleEndian, sample)
		binary.Write(&b, binary.LittleEndian, sample)
	}
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
