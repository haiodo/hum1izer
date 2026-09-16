package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/haiodo/hum1izer/releases/latest"

const upgradeUsage = `hum1izer upgrade - upgrade to the latest GitHub release.

  --check  only say whether a new version is available
  --force  install even if the version matches
`

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpgrade(args []string) int {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, upgradeUsage) }
	check := fs.Bool("check", false, "only check whether a new version is available")
	force := fs.Bool("force", false, "install even if the version matches")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rel, err := latestRelease()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to query GitHub:", err)
		return 2
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	cur := strings.TrimPrefix(Version, "v")

	switch {
	case *force:
	case cur == "dev":
		fmt.Printf("built from source, latest release %s\n", latest)
	case !newer(latest, cur):
		fmt.Printf("hum1izer %s, nothing newer on GitHub\n", cur)
		return 0
	default:
		fmt.Printf("%s is available, current %s\n", latest, cur)
	}
	if *check {
		return 0
	}

	bin, err := downloadBinary(rel, latest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	exe, err := replaceSelf(bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("installed: %s %s\n", exe, latest)
	return 0
}

// newer сравнивает версии вида 1.3.0 почислово. Нечисловой хвост (rc, beta)
// отбрасывается: для решения "качать или нет" его точности хватает.
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		if num(pa, i) != num(pb, i) {
			return num(pa, i) > num(pb, i)
		}
	}
	return false
}

func num(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	digits := parts[i]
	if cut := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
		digits = digits[:cut]
	}
	n, _ := strconv.Atoi(digits)
	return n
}

func latestRelease() (*release, error) {
	body, err := fetch(releasesAPI)
	if err != nil {
		return nil, err
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, fmt.Errorf("response has no tag_name")
	}
	return &rel, nil
}

// downloadBinary качает архив под текущую платформу и сверяет его с SHA256SUMS
// из того же релиза. Без сверки мы бы подменяли исполняемый файл чем попало.
func downloadBinary(rel *release, version string) ([]byte, error) {
	name := fmt.Sprintf("hum1izer_%s_%s_%s", version, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".zip"
	} else {
		name += ".tar.gz"
	}

	var assetURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case name:
			assetURL = a.URL
		case "SHA256SUMS":
			sumsURL = a.URL
		}
	}
	if assetURL == "" {
		return nil, fmt.Errorf("release %s has no file %s - download it manually from https://github.com/haiodo/hum1izer/releases", rel.Tag, name)
	}
	if sumsURL == "" {
		return nil, fmt.Errorf("release %s has no SHA256SUMS, nothing to verify against", rel.Tag)
	}

	sums, err := fetch(sumsURL)
	if err != nil {
		return nil, fmt.Errorf("SHA256SUMS: %w", err)
	}
	want := sumFor(sums, name)
	if want == "" {
		return nil, fmt.Errorf("SHA256SUMS has no line for %s", name)
	}

	blob, err := fetch(assetURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	got := sha256.Sum256(blob)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch for %s, file not installed", name)
	}
	return unpack(blob, strings.HasSuffix(name, ".zip"))
}

func sumFor(sums []byte, name string) string {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

func unpack(blob []byte, isZip bool) ([]byte, error) {
	want := "hum1izer"
	if runtime.GOOS == "windows" {
		want = "hum1izer.exe"
	}

	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) != want {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = rc.Close() }()
			return io.ReadAll(io.LimitReader(rc, 200<<20))
		}
		return nil, fmt.Errorf("archive has no %s", want)
	}

	gz, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == want {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
	return nil, fmt.Errorf("archive has no %s", want)
}

// replaceSelf кладёт новый бинарник рядом со старым и переименовывает поверх:
// на unix это атомарно и работает даже на запущенном файле.
func replaceSelf(bin []byte) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".hum1izer-new-*")
	if err != nil {
		return "", fmt.Errorf("no write permission in %s: %w", filepath.Dir(exe), err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}

	// Windows не даёт переименовать поверх запущенного файла, поэтому старый
	// отводится в сторону; удалить его можно при следующем запуске.
	if runtime.GOOS == "windows" {
		_ = os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		if runtime.GOOS == "windows" {
			// Иначе на месте бинарника не останется ничего, кроме .old.
			_ = os.Rename(exe+".old", exe)
		}
		return "", fmt.Errorf("failed to replace %s: %w", exe, err)
	}
	return exe, nil
}

func fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hum1izer/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}
