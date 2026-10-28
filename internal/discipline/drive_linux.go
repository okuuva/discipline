package discipline

/*
#include <errno.h>
#include <linux/cdrom.h>
#include <linux/fs.h>
#include <limits.h>
#include <sys/ioctl.h>

// Return 0 for no disc, 1 for audio, 2 for data, 3 for mixed mode.
static int read_toc(int fd, int *offsets, int *count, int *leadout, unsigned long long *generation) {
 if (ioctl(fd, BLKGETDISKSEQ, generation) < 0) return -errno;
 int status = ioctl(fd, CDROM_DRIVE_STATUS, CDSL_CURRENT);
 if (status != CDS_DISC_OK) {
  int result = status < 0 ? -errno : 0;
  return result;
 }
 struct cdrom_tochdr header;
 if (ioctl(fd, CDROMREADTOCHDR, &header) < 0) return -errno;
 if (header.cdth_trk0 != 1 || header.cdth_trk1 < 1 || header.cdth_trk1 > 99) return -EINVAL;
 *count = header.cdth_trk1;
 int audio = 0, data = 0;
 for (int i = 0; i <= *count; i++) {
  struct cdrom_tocentry entry = {0};
  entry.cdte_track = i == *count ? CDROM_LEADOUT : i + 1;
  entry.cdte_format = CDROM_LBA;
  if (ioctl(fd, CDROMREADTOCENTRY, &entry) < 0) return -errno;
  if (i == *count) { *leadout = entry.cdte_addr.lba + 150; }
  else {
   offsets[i] = entry.cdte_addr.lba + 150;
   if (entry.cdte_ctrl & CDROM_DATA_TRACK) data++; else audio++;
  }
 }
 unsigned long long after;
 if (ioctl(fd, BLKGETDISKSEQ, &after) < 0) return -errno;
 if (after != *generation) return -EAGAIN;
 return data ? (audio ? 3 : 2) : 1;
}
*/
import "C"

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	udev "github.com/jochenvg/go-udev"
	"golang.org/x/sys/unix"
)

type Disc struct {
	generation uint64
	Kind       string `json:"kind"`
	Offsets    []int  `json:"offsets,omitempty"`
	Leadout    int    `json:"leadout,omitempty"`
}

func (d Disc) Identity() (string, string, error) {
	if d.Kind != "audio" || len(d.Offsets) < 1 || len(d.Offsets) > 99 {
		return "", "", errors.New("not an audio CD")
	}
	parts := []string{strconv.Itoa(len(d.Offsets))}
	previous := 149
	for _, offset := range append(append([]int{}, d.Offsets...), d.Leadout) {
		if offset <= previous || offset > 100*60*75 {
			return "", "", errors.New("invalid audio TOC")
		}
		parts = append(parts, strconv.Itoa(offset))
		previous = offset
	}
	normalized := strings.Join(parts, " ")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalized))), normalized, nil
}

func readDisc(device string) (Disc, error) {
	fd, err := unix.Open(device, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return Disc{}, err
	}
	defer unix.Close(fd)
	return readDiscFD(fd)
}

func readDiscFD(fd int) (Disc, error) {
	var offsets [99]C.int
	var count, leadout C.int
	var generation C.ulonglong
	kind := C.read_toc(C.int(fd), &offsets[0], &count, &leadout, &generation)
	if kind < 0 {
		return Disc{}, fmt.Errorf("read drive: %w", syscall.Errno(-kind))
	}
	disc := Disc{Kind: []string{"absent", "audio", "data", "mixed"}[int(kind)], generation: uint64(generation)}
	if kind == 1 {
		disc.Leadout = int(leadout)
		for _, offset := range offsets[:int(count)] {
			disc.Offsets = append(disc.Offsets, int(offset))
		}
		if _, _, err := disc.Identity(); err != nil {
			return Disc{}, err
		}
	}
	return disc, nil
}

// monitorDrive subscribes before sampling the loaded drive. Blocking extraction
// runs elsewhere; errors stop detection instead of guessing a new insertion.
func monitorDrive(ctx context.Context, device string, observe func(Disc) error) error {
	device, err := filepath.EvalSymlinks(device)
	if err != nil {
		return err
	}
	var startupSequence uint64
	return monitorDevices(ctx, func(d *udev.Device) error {
		if d == nil {
			// Ignore queued events already represented by a stable startup sample.
			for range 3 {
				before, err := kernelSequence()
				if err != nil {
					return err
				}
				disc, err := readDisc(device)
				if err != nil {
					return err
				}
				after, err := kernelSequence()
				if err != nil {
					return err
				}
				if before == after {
					startupSequence = after
					return observe(disc)
				}
			}
			return errors.New("drive changed during startup; reconcile before restarting")
		}
		if d.Seqnum() <= startupSequence {
			return nil
		}
		if d.Devnode() != device {
			return nil
		}
		if d.Action() == "remove" || d.PropertyValue("ID_CDROM_MEDIA") != "1" {
			return observe(Disc{Kind: "absent"})
		}
		disc, err := readDisc(device)
		if err != nil {
			return err
		}
		return observe(disc)
	})
}

var errUnobservedMedia = errors.New("media changed before its udev observation was reconciled")

type driveLease struct {
	fd     int
	disc   Disc
	locked bool
}

// Lock the tray and sample through the pinned descriptor before claiming a job.
// BLKGETDISKSEQ changes on media change even when udev delivery is still pending.
func acquireDrive(device string, observed Disc) (*driveLease, error) {
	fd, err := unix.Open(device, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	lease := &driveLease{fd: fd}
	if err := unix.IoctlSetInt(fd, C.CDROM_LOCKDOOR, 1); err != nil {
		unix.Close(fd)
		return nil, err
	}
	lease.locked = true
	lease.disc, err = readDiscFD(fd)
	if err == nil {
		err = matchingObservation(observed, lease.disc)
	}
	if err != nil {
		if closeErr := lease.Close(); closeErr != nil {
			return nil, fmt.Errorf("release changed drive: %w", closeErr)
		}
		return nil, err
	}
	return lease, nil
}

func matchingObservation(observed, loaded Disc) error {
	if observed.generation == 0 || loaded.generation != observed.generation {
		return errUnobservedMedia
	}
	oldKey, _, oldErr := observed.Identity()
	key, _, err := loaded.Identity()
	if oldErr != nil || err != nil || oldKey != key {
		return errUnobservedMedia
	}
	return nil
}

func (d *driveLease) Close() error {
	var err error
	if d.locked {
		err = unix.IoctlSetInt(d.fd, C.CDROM_LOCKDOOR, 0)
	}
	d.locked = false
	return errors.Join(err, unix.Close(d.fd))
}

func (d *driveLease) Eject(jobID, insertionID string) error {
	if jobID != insertionID {
		return errors.New("another insertion now owns the drive")
	}
	loaded, err := readDiscFD(d.fd)
	if err != nil {
		return err
	}
	if err := matchingObservation(d.disc, loaded); err != nil {
		return err
	}
	if err := unix.IoctlSetInt(d.fd, C.CDROM_LOCKDOOR, 0); err != nil {
		return err
	}
	d.locked = false
	return unix.IoctlSetInt(d.fd, C.CDROMEJECT, 0)
}

func kernelSequence() (uint64, error) {
	data, err := os.ReadFile("/sys/kernel/uevent_seqnum")
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func monitorDevices(ctx context.Context, observe func(*udev.Device) error) error {
	u := &udev.Udev{}
	monitor := u.NewMonitorFromNetlink("udev")
	if monitor == nil {
		return errors.New("create libudev monitor")
	}
	if err := monitor.FilterAddMatchSubsystemDevtype("block", "disk"); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	events, failures, err := monitor.DeviceChan(ctx)
	if err != nil {
		cancel()
		return err
	}
	defer func() {
		cancel()
		// The binding sends on unbuffered channels without a cancellation select.
		for events != nil || failures != nil {
			select {
			case _, ok := <-events:
				if !ok {
					events = nil
				}
			case _, ok := <-failures:
				if !ok {
					failures = nil
				}
			}
		}
	}()
	if err := observe(nil); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case failure, ok := <-failures:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !ok || failure == nil {
				return errors.New("udev error channel closed unexpectedly; reconcile drive")
			}
			return fmt.Errorf("udev: %w", failure)
		case device, ok := <-events:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !ok || device == nil {
				return errors.New("udev event channel closed unexpectedly; reconcile drive")
			}
			if err := observe(device); err != nil {
				return err
			}
		}
	}
}
