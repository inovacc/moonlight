package downloader

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

const (
	goUrl = "https://go.dev/dl"
)

func DownloadGoVersion(filename, hash, dest string) error {
	downloadUrl := fmt.Sprintf("%s/%s", goUrl, filename)
	u, err := url.Parse(downloadUrl)
	if err != nil {
		return fmt.Errorf("error parsing url: %w", err)
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: %s, status code: %d", filename, resp.StatusCode)
	}

	destPath := filepath.Join(dest, filename)

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	h := sha256.New()

	teeReader := io.TeeReader(resp.Body, h)

	if _, err = io.Copy(out, teeReader); err != nil {
		return err
	}

	if hash != fmt.Sprintf("%x", h.Sum(nil)) {
		return fmt.Errorf("hash mismatch: expected %s, got %x", hash, h.Sum(nil))
	}

	return nil
}
