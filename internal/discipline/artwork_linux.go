package discipline

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const imageLimit = 10 << 20

var publicName = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z][a-z0-9-]+$`)

func publicAddress(ip netip.Addr) bool {
	if ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return ip.Is4() || netip.MustParsePrefix("2000::/3").Contains(ip)
}

func coverTarget(ctx context.Context, raw string, lookup func(context.Context, string, string) ([]netip.Addr, error)) (*url.URL, string, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Fragment != "" {
		return nil, "", errors.New("cover URL must be HTTPS without credentials or a fragment")
	}
	host := strings.ToLower(u.Hostname())
	if strings.EqualFold(host, "api.telegram.org") {
		return nil, "", errors.New("send Telegram images as attachments, not credential-bearing file URLs")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, "", errors.New("invalid cover URL port")
	}
	port = strconv.Itoa(n)
	u.Host = net.JoinHostPort(host, port)
	var addresses []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{ip}
	} else {
		if !publicName.MatchString(host) {
			return nil, "", errors.New("invalid public cover hostname")
		}
		addresses, err = lookup(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, "", errors.New("cover hostname lookup failed")
		}
	}
	for _, ip := range addresses {
		if !publicAddress(ip) {
			return nil, "", errors.New("cover destination is not a public address")
		}
	}
	selected := addresses[0].Unmap()
	for _, ip := range addresses {
		if ip.Unmap().Is4() {
			selected = ip.Unmap()
			break
		}
	}
	address := selected.String()
	if selected.Is6() {
		address = "[" + address + "]"
	}
	return u, net.JoinHostPort(host, port) + ":" + address, nil
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedBuffer) String() string { return b.buffer.String() }
func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("response exceeds byte limit")
	}
	return b.buffer.Write(p)
}

func coverCurl(ctx context.Context, source, target string) *exec.Cmd {
	// shortcut: use HTTP/1.1 wire output for Go's parser; replace this transport if a required cover server is HTTP/2-only.
	return exec.CommandContext(ctx, "curl", "--disable", "--silent", "--show-error", "--include", "--http1.1", "--raw", "--header", "Accept-Encoding: identity",
		"--proto", "=https", "--max-time", "30", "--connect-timeout", "10",
		"--max-filesize", strconv.Itoa(imageLimit), "--noproxy", "*", "--proxy", "",
		"--resolve", target, "--url", source)
}

func downloadCover(ctx context.Context, source string, lease *os.File, lookup func(context.Context, string, string) ([]netip.Addr, error)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for redirects := 0; redirects <= 3; redirects++ {
		u, target, err := coverTarget(ctx, source, lookup)
		if err != nil {
			return nil, err
		}
		cmd := coverCurl(ctx, u.String(), target)
		output := &boundedBuffer{limit: imageLimit + (64 << 10)}
		cmd.Stdout = output
		if err := runMediaProcess(ctx, cmd, lease); err != nil {
			return nil, errors.New("cover download failed or exceeded its limits")
		}
		reader := bufio.NewReader(bytes.NewReader(output.Bytes()))
		var response *http.Response
		for {
			response, err = http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
			if err != nil {
				return nil, errors.New("invalid cover HTTP response")
			}
			if response.StatusCode/100 != 1 || response.StatusCode == http.StatusSwitchingProtocols {
				break
			}
			response.Body.Close()
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, imageLimit+1))
		response.Body.Close()
		if readErr != nil || len(data) > imageLimit {
			return nil, errors.New("cover response exceeds byte limit")
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			next, err := u.Parse(response.Header.Get("Location"))
			if err != nil || response.Header.Get("Location") == "" {
				return nil, errors.New("invalid cover redirect")
			}
			source = next.String()
			continue
		}
		if response.StatusCode != http.StatusOK {
			return nil, errors.New("cover server did not return an image")
		}
		return data, nil
	}
	return nil, errors.New("cover redirect limit exceeded")
}

func imageCommand(ctx context.Context, attempt string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "magick", args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "HOME=" + filepath.Join(attempt, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(attempt, "home", ".config"),
		"MAGICK_CONFIGURE_PATH=" + os.Getenv("MAGICK_CONFIGURE_PATH"),
		"MAGICK_TEMPORARY_PATH=" + filepath.Join(attempt, "tmp")}
	return cmd
}

// Convert into a new file, leaving the current candidate untouched until a
// database commit selects this filename and digest as the next review revision.
func convertCover(ctx context.Context, attempt string, data []byte, lease *os.File) (string, string, error) {
	policy := os.Getenv("MAGICK_CONFIGURE_PATH")
	if !filepath.IsAbs(policy) {
		return "", "", errors.New("packaged ImageMagick policy is required")
	}
	if _, err := readRegular(filepath.Join(policy, "policy.xml"), 64<<10); err != nil {
		return "", "", err
	}
	if len(data) == 0 || len(data) > imageLimit {
		return "", "", errors.New("image must be between 1 byte and 10 MiB")
	}
	format := map[string]string{"image/jpeg": "JPEG", "image/png": "PNG", "image/webp": "WEBP"}[http.DetectContentType(data)]
	if format == "" {
		return "", "", errors.New("only JPEG, PNG and WebP images are accepted")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := os.Lstat(attempt)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("attempt is not a regular directory")
	}
	root := filepath.Join(attempt, "review")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", "", err
	}
	info, err = os.Lstat(root)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("review path is not a regular directory")
	}
	input, err := os.CreateTemp(root, "input-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(input.Name())
	_, writeErr := input.Write(data)
	if err := errors.Join(writeErr, input.Close()); err != nil {
		return "", "", err
	}
	var dimensions boundedBuffer
	dimensions.limit = 1024
	identify := imageCommand(ctx, attempt, "identify", "-ping", "-format", "%m %w %h %n", format+":"+input.Name())
	identify.Stdout = &dimensions
	if err := runMediaProcess(ctx, identify, lease); err != nil {
		return "", "", errors.New("image identification failed")
	}
	fields := strings.Fields(dimensions.String())
	if len(fields) != 4 || fields[0] != format || fields[3] != "1" {
		return "", "", errors.New("expected one supported image")
	}
	width, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return "", "", err
	}
	height, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil || width == 0 || height == 0 || width > 25000000/height {
		return "", "", errors.New("image exceeds 25 megapixels")
	}
	id, err := newJobID()
	if err != nil {
		return "", "", err
	}
	rel := filepath.Join("review", "cover-"+id+".jpg")
	output := filepath.Join(attempt, rel)
	cmd := imageCommand(ctx, attempt, format+":"+input.Name(), "-strip", "-sampling-factor", "4:2:0", "-interlace", "Plane", "-resize", "1000x1000>", "JPEG:"+output)
	if err := runMediaProcess(ctx, cmd, lease); err != nil {
		os.Remove(output)
		return "", "", errors.New("image conversion failed")
	}
	converted, err := readRegular(output, imageLimit)
	if err != nil {
		return "", "", err
	}
	if !bytes.HasPrefix(converted, []byte{0xff, 0xd8, 0xff}) {
		return "", "", errors.New("conversion did not produce JPEG")
	}
	for _, path := range []string{output, root, attempt} {
		if err := syncPath(path); err != nil {
			return "", "", err
		}
	}
	return rel, fmt.Sprintf("%x", sha256.Sum256(converted)), nil
}
