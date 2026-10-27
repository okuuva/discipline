package discipline

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCoverTargets(t *testing.T) {
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	_, pin, err := coverTarget(context.Background(), "https://example.com/cover", lookup)
	if err != nil || pin != "example.com:443:1.1.1.1" {
		t.Fatalf("unpinned request: %q %v", pin, err)
	}
	for _, source := range []string{"http://example.com/a", "https://user:secret@example.com/a", "https://127.0.0.1/a", "https://[::1]/a", "https://[::ffff:127.0.0.1]/a", "https://169.254.169.254/a", "https://10.1.2.3/a", "https://100.64.0.1/a", "https://[64:ff9b::7f00:1]/a", "https://[2002:7f00:1::]/a", "https://2130706433/a", "https://0177.0.0.1/a", "https://example.com:0/a", "https://[2606:4700:4700::1111%25lo]/a", "https://api.telegram.org/file/bot-secret/a"} {
		if _, _, err := coverTarget(context.Background(), source, lookup); err == nil {
			t.Errorf("accepted unsafe URL %q", source)
		}
	}
	private := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("192.168.1.1")}, nil
	}
	if _, _, err := coverTarget(context.Background(), "https://example.com/a", private); err == nil {
		t.Fatal("accepted mixed public/private DNS answer")
	}
}

func TestBoundedBufferCannotBypassLimit(t *testing.T) {
	buffer := &boundedBuffer{limit: 4}
	if _, err := io.Copy(buffer, io.LimitReader(strings.NewReader("12345"), 5)); err == nil || len(buffer.Bytes()) > 4 {
		t.Fatal("copy bypassed output limit")
	}
}

func TestCoverHTTPBoundaries(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not available")
	}
	for _, http2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("HTTP2_available_%t", http2), func(t *testing.T) {
			var calls atomic.Int64
			var protocol atomic.Int64
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				protocol.Store(int64(r.ProtoMajor))
				switch r.URL.Path {
				case "/chunked":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					w.Write([]byte("image bytes"))
				case "/early":
					w.WriteHeader(http.StatusEarlyHints)
					w.Write([]byte("image bytes"))
				case "/private":
					w.Header().Set("Location", "https://private.example/a")
					w.WriteHeader(302)
				case "/downgrade":
					w.Header().Set("Location", "http://example.com/a")
					w.WriteHeader(302)
				case "/loop":
					w.Header().Set("Location", "/loop")
					w.WriteHeader(302)
				case "/large":
					w.Header().Set("Content-Length", fmt.Sprint(imageLimit+1))
					w.WriteHeader(200)
				default:
					w.Write([]byte("image bytes"))
				}
			}))
			server.EnableHTTP2 = http2
			server.StartTLS()
			defer server.Close()
			root := t.TempDir()
			cert := filepath.Join(root, "certificate.pem")
			if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(root, "arguments")
			port := strings.TrimPrefix(server.URL, "https://127.0.0.1:")
			// Route this fixture to the local TLS server while retaining the production
			// --resolve arguments for inspection; no test connects to a public address.
			shim := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\nexec %q --disable --cacert %q --connect-to ::127.0.0.1:%s \"$@\"\n", log, curl, cert, port)
			if err := os.WriteFile(filepath.Join(root, "curl"), []byte(shim), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
			lookups := 0
			rebind := true
			lookup := func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
				lookups++
				ip := "1.1.1.1"
				if host == "private.example" || rebind && lookups > 1 {
					ip = "127.0.0.1"
				}
				return []netip.Addr{netip.MustParseAddr(ip)}, nil
			}
			data, err := downloadCover(context.Background(), "https://example.com/ok", nil, lookup)
			if err != nil || string(data) != "image bytes" || lookups != 1 {
				t.Fatalf("rebound or failed request over HTTP/%d: %q %d %v", protocol.Load(), data, lookups, err)
			}
			args, err := os.ReadFile(log)
			if err != nil || !strings.Contains(string(args), "example.com:443:1.1.1.1") || strings.Contains(string(args), "--location") {
				t.Fatalf("unsafe curl arguments: %s %v", args, err)
			}
			rebind = false
			for _, path := range []string{"chunked", "early"} {
				data, err := downloadCover(context.Background(), "https://example.com/"+path, nil, lookup)
				if err != nil || string(data) != "image bytes" {
					t.Errorf("%s response failed: %q %v", path, data, err)
				}
			}
			for _, path := range []string{"private", "downgrade", "loop", "large"} {
				before := calls.Load()
				if _, err := downloadCover(context.Background(), "https://example.com/"+path, nil, lookup); err == nil {
					t.Fatalf("accepted %s response", path)
				}
				want := 1
				if path == "loop" {
					want = 4
				}
				if calls.Load()-before != int64(want) {
					t.Fatalf("%s made %d HTTP calls, wanted %d", path, calls.Load()-before, want)
				}
			}
		})
	}
}

func TestCoverConversionPolicy(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not available")
	}
	if os.Getenv("MAGICK_CONFIGURE_PATH") == "" {
		t.Fatal("run with the packaged image policy")
	}
	attempt := t.TempDir()
	t.Setenv("MAGICK_CODER_MODULE_PATH", "/not/a/valid/module/path")
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewGray(image.Rect(0, 0, 1200, 600))); err != nil {
		t.Fatal(err)
	}
	cover, digest, err := convertCover(context.Background(), attempt, input.Bytes(), nil)
	if err != nil || len(digest) != 64 {
		t.Fatalf("conversion failed: %q %v", digest, err)
	}
	converted, err := readRegular(filepath.Join(attempt, cover), imageLimit)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(converted))
	if err != nil || format != "jpeg" || config.Width != 1000 || config.Height != 500 {
		t.Fatalf("wrong output: %+v %s %v", config, format, err)
	}
	if _, _, err := convertCover(context.Background(), attempt, converted, nil); err != nil {
		t.Fatal("JPEG input", err)
	}
	webp := filepath.Join(attempt, "fixture.webp")
	if output, err := imageCommand(context.Background(), attempt, "JPEG:"+filepath.Join(attempt, cover), "WEBP:"+webp).CombinedOutput(); err != nil {
		t.Fatal("WebP fixture", string(output), err)
	}
	webpBytes, err := os.ReadFile(webp)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := convertCover(context.Background(), attempt, webpBytes, nil); err != nil {
		t.Fatal("WebP input", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := convertCover(cancelled, attempt, input.Bytes(), nil); err == nil {
		t.Fatal("ignored image cancellation")
	}
	for _, data := range [][]byte{[]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"), []byte("%PDF-1.0"), input.Bytes()[:20], make([]byte, imageLimit+1)} {
		if _, _, err := convertCover(context.Background(), attempt, data, nil); err == nil {
			t.Fatal("accepted unsafe image")
		}
	}
	input.Reset()
	if err := png.Encode(&input, image.NewGray(image.Rect(0, 0, 6000, 5000))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := convertCover(context.Background(), attempt, input.Bytes(), nil); err == nil {
		t.Fatal("accepted image over 25 megapixels")
	}
	if err := imageCommand(context.Background(), attempt, "xc:red", filepath.Join(attempt, "forbidden.jpg")).Run(); err == nil {
		t.Fatal("coder policy did not block XC")
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write(converted) }))
	defer server.Close()
	if err := imageCommand(context.Background(), attempt, server.URL+"/cover", filepath.Join(attempt, "forbidden.jpg")).Run(); err == nil || requests.Load() != 0 {
		t.Fatal("policy allowed a network delegate", err)
	}
	if after, err := os.ReadFile(filepath.Join(attempt, cover)); err != nil || !bytes.Equal(after, converted) {
		t.Fatal("failed replacement changed previous candidate")
	}
}
