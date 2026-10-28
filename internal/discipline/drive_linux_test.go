package discipline

import (
	"context"
	"errors"
	"testing"
	"time"

	udev "github.com/jochenvg/go-udev"
)

func TestDiscIdentity(t *testing.T) {
	disc := Disc{Kind: "audio", Offsets: []int{150, 12345}, Leadout: 23456}
	key, toc, err := disc.Identity()
	if err != nil || len(key) != 64 || toc != "2 150 12345 23456" {
		t.Fatalf("identity: %s %q %v", key, toc, err)
	}
	disc.Leadout++
	other, _, _ := disc.Identity()
	if key == other {
		t.Fatal("lead-out did not affect identity")
	}
	for _, disc := range []Disc{{Kind: "data"}, {Kind: "mixed"}, {Kind: "audio"}, {Kind: "audio", Offsets: []int{150, 140}, Leadout: 200}, {Kind: "audio", Offsets: []int{150}, Leadout: 150}} {
		if _, _, err := disc.Identity(); err == nil {
			t.Fatalf("accepted invalid disc %+v", disc)
		}
	}
}

func TestObservationMustMatchMediaGeneration(t *testing.T) {
	observed := Disc{Kind: "audio", Offsets: []int{150}, Leadout: 450, generation: 12}
	if err := matchingObservation(observed, observed); err != nil {
		t.Fatal(err)
	}
	replacement := observed
	replacement.generation++
	if err := matchingObservation(observed, replacement); !errors.Is(err, errUnobservedMedia) {
		t.Fatalf("pending same-TOC replacement accepted: %v", err)
	}
	replacement = observed
	replacement.Leadout++
	if err := matchingObservation(observed, replacement); !errors.Is(err, errUnobservedMedia) {
		t.Fatalf("different TOC accepted: %v", err)
	}
	observed.generation = 0
	if err := matchingObservation(observed, replacement); !errors.Is(err, errUnobservedMedia) {
		t.Fatalf("unknown observation accepted: %v", err)
	}
}

func TestLibudevLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	startup := false
	err := monitorDevices(ctx, func(d *udev.Device) error {
		if d == nil {
			startup = true
			cancel()
		}
		return nil
	})
	if !startup || !errors.Is(err, context.Canceled) {
		t.Fatalf("monitor startup/shutdown: startup=%v err=%v", startup, err)
	}
}
