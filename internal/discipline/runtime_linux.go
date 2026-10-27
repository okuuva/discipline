package discipline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type ServiceConfig struct {
	State, Spool, Drive, ABCDEConfig string
	Telegram                         TelegramConfig
}

func Serve(ctx context.Context, cfg ServiceConfig) (result error) {
	if os.Geteuid() == 0 {
		return errors.New("run the service as a non-root user with optical device access")
	}
	if !filepath.IsAbs(cfg.Drive) || cfg.ABCDEConfig == "" {
		return errors.New("serve requires an absolute drive path and the packaged abcde configuration")
	}
	bot, err := newTelegram(cfg.Telegram)
	if err != nil {
		return err
	}
	defer bot.client.CloseIdleConnections()
	s, err := OpenStore(cfg.State, cfg.Spool, true)
	if err != nil {
		return err
	}
	defer s.Close()
	fd, err := unix.Open(filepath.Join(cfg.State, "main.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	lease := os.NewFile(uintptr(fd), "main.lock")
	defer lease.Close()
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("another main service or its child still owns the drive: %w", err)
	}
	if err := s.interruptedRips(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reviewDone := make(chan error, 1)
	go func() { reviewDone <- s.runReview(ctx, bot, cfg.Spool, lease) }()
	defer func() { cancel(); result = errors.Join(result, <-reviewDone) }()
	var mu sync.Mutex
	var current Disc
	wake := make(chan struct{}, 1)
	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- monitorDrive(ctx, cfg.Drive, func(disc Disc) error {
			mu.Lock()
			defer mu.Unlock()
			changed := current.generation != 0 && disc.generation != 0 && current.generation != disc.generation
			if _, err := s.observeInsertion(ctx, disc, changed); err != nil {
				return err
			}
			current = disc
			if disc.Kind == "mixed" {
				log.Print("mixed-mode CD held: unsupported media")
			}
			select {
			case wake <- struct{}{}:
			default:
			}
			return nil
		})
	}()
	defer func() {
		cancel()
		if err := <-monitorDone; err != ctx.Err() {
			result = errors.Join(result, err)
		}
	}()
	// The ticker services local operator requests, not failed extraction retries.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	workerDone := make(chan error, 1)
	busy := false
	defer func() {
		cancel()
		if busy {
			result = errors.Join(result, <-workerDone)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-reviewDone:
			reviewDone <- nil
			return err
		case err := <-monitorDone:
			monitorDone <- nil
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil
			}
			return err
		case err := <-workerDone:
			busy = false
			if err != nil {
				return err
			}
		case <-wake:
		case <-ticker.C:
		}
		if busy {
			continue
		}
		mu.Lock()
		disc := current
		mu.Unlock()
		if disc.Kind == "" {
			continue
		}
		var work bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE phase='queued') OR EXISTS(
SELECT 1 FROM requests WHERE `+driveRequestFilter+`)`).Scan(&work); err != nil {
			return err
		}
		if !work {
			continue
		}
		mu.Lock()
		j, discLease, err := s.claimRip(ctx, cfg.Drive, current)
		mu.Unlock()
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errUnobservedMedia) {
			continue
		}
		if errors.Is(err, errDiscChanged) {
			log.Print(err)
			continue
		}
		if err != nil {
			return err
		}
		busy = true
		go func() {
			ripErr := extract(ctx, cfg, j, lease)
			phase, message := AwaitingReview, ""
			if ripErr != nil {
				phase, message = RipFailed, ripErr.Error()
			}
			// A cancelled request must still retain a durable interrupted result.
			_, stateErr := s.Transition(context.Background(), j, phase, message)
			if stateErr == nil && ripErr == nil {
				mu.Lock()
				var currentJob string
				err := s.db.QueryRowContext(ctx, "SELECT COALESCE(job_id, '') FROM drive WHERE id=1").Scan(&currentJob)
				if err == nil {
					err = discLease.Eject(j.ID, currentJob)
				}
				if err != nil {
					log.Printf("rip %s complete, leaving disc loaded: %v", j.ID, err)
				}
				mu.Unlock()
			}
			if ripErr != nil {
				log.Printf("rip %s failed: %v", j.ID, ripErr)
			}
			workerDone <- errors.Join(stateErr, discLease.Close())
		}()
	}
}

// The caller serializes this acquisition with libudev observations. No database
// transaction spans device access; the returned tray lock belongs to the worker.
func (s *Store) claimRip(ctx context.Context, device string, observed Disc) (j Job, lease *driveLease, err error) {
	loaded := observed
	if observed.Kind == "audio" {
		lease, err = acquireDrive(device, observed)
		if err != nil {
			return Job{}, nil, err
		}
		loaded = lease.disc
	}
	defer func() {
		if err != nil && lease != nil {
			if closeErr := lease.Close(); closeErr != nil {
				err = fmt.Errorf("release drive after claim failure: %w", closeErr)
			}
			lease = nil
		}
	}()
	var queuedOwner bool
	if err = s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM jobs WHERE phase='queued' AND id=(SELECT job_id FROM drive WHERE id=1))").Scan(&queuedOwner); err != nil {
		return Job{}, lease, err
	}
	if !queuedOwner {
		if err = s.applyDriveRequests(ctx, loaded); err != nil {
			return Job{}, lease, err
		}
	}
	j, err = s.nextRip(ctx, loaded)
	return j, lease, err
}

func extract(ctx context.Context, cfg ServiceConfig, j Job, lease *os.File) error {
	disc, err := storedDisc(j)
	if err != nil {
		return err
	}
	var space unix.Statfs_t
	if err := unix.Statfs(cfg.Spool, &space); err != nil {
		return err
	}
	if space.Bavail*uint64(space.Bsize) < uint64(disc.Leadout)*2352*2+(64<<20) {
		return errors.New("insufficient local space for extraction; retain pending attempts and free space manually")
	}
	attempt := filepath.Join(cfg.Spool, j.ID)
	if !uuidPattern.MatchString(j.ID) {
		return errors.New("invalid attempt UUID")
	}
	if err := os.MkdirAll(attempt, 0700); err != nil {
		return err
	}
	if err := flushTree(attempt); err != nil {
		return err
	}
	if err := syncPath(cfg.Spool); err != nil {
		return err
	}
	for _, dir := range []string{"home", "scratch", "media", "tmp"} {
		if err := os.MkdirAll(filepath.Join(attempt, dir), 0700); err != nil {
			return err
		}
	}
	config, err := readRegular(cfg.ABCDEConfig, 1<<20)
	if err != nil {
		return err
	}
	configPath := filepath.Join(attempt, "abcde.conf")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		return err
	}
	output, err := os.OpenFile(filepath.Join(attempt, "abcde.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	cmd := exec.CommandContext(ctx, "abcde", "-N", "-c", configPath, "-d", cfg.Drive)
	cmd.Dir = attempt
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(attempt, "home"), "TMPDIR=" + filepath.Join(attempt, "tmp"), "LANG=C.UTF-8", "DISCIPLINE_ATTEMPT=" + attempt, "DISCIPLINE_DRIVE=" + cfg.Drive}
	cmd.Stdout, cmd.Stderr = output, output
	if err := runMediaProcess(ctx, cmd, lease); err != nil {
		return fmt.Errorf("abcde failed; see retained abcde.log: %w", err)
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return validateAudio(ctx, attempt, len(disc.Offsets), lease)
}

// Children inherit the service lock. Cancellation kills the whole process group,
// and we retain ownership until it is gone rather than racing a surviving reader.
func runMediaProcess(ctx context.Context, cmd *exec.Cmd, lease *os.File) error {
	// Reap orphaned grandchildren after group cancellation, including in containers.
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if lease != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, lease)
	}
	cmd.Cancel = func() error {
		err := unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		if errors.Is(err, unix.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if cmd.Stdout == nil {
		cmd.Stdout = io.Discard
	}
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	err := cmd.Run()
	if cmd.Process != nil {
		for {
			if ctx.Err() != nil {
				_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
			}
			for {
				pid, reapErr := unix.Wait4(-cmd.Process.Pid, nil, unix.WNOHANG, nil)
				if pid <= 0 || reapErr != nil {
					break
				}
			}
			probeErr := unix.Kill(-cmd.Process.Pid, 0)
			if errors.Is(probeErr, unix.ESRCH) {
				break
			}
			if probeErr != nil {
				return errors.Join(err, fmt.Errorf("cannot establish worker quiescence: %w", probeErr))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return errors.Join(err, ctx.Err())
}
