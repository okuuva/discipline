package discipline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMediaCancellationReapsChildren(t *testing.T) {
	for _, script := range []string{"sleep 60 & wait", "sleep 60 >/dev/null 2>&1 &"} {
		t.Run(script, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", script)
			output, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			cmd.Stdout, cmd.Stderr = output, output
			done := make(chan error, 1)
			go func() { done <- runMediaProcess(ctx, cmd, nil) }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled command reported success")
				}
				if err := unix.Kill(-cmd.Process.Pid, 0); !errors.Is(err, unix.ESRCH) {
					t.Fatalf("process group survived: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled process group did not quiesce")
			}
		})
	}
}

func TestOldRipCannotEjectNewInsertion(t *testing.T) {
	err := (&driveLease{fd: -1}).Eject("old-insertion", "new-insertion")
	if err == nil || err.Error() != "another insertion now owns the drive" {
		t.Fatalf("old job reached the device: %v", err)
	}
}

func TestMediaInheritsLifetimeLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	lease, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), "bash", "-c", "test /proc/self/fd/3 -ef \"$1\" && flock -n 3", "--", path)
	if err := runMediaProcess(context.Background(), cmd, lease); err != nil {
		t.Fatalf("child did not inherit the same lock: %v", err)
	}
	other, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		t.Fatal("another process acquired the live lease")
	}
	lease.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("lock held after quiescence: %v", err)
	}
}
