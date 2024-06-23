package downloader

import (
	"fmt"
	"os"
	"testing"
)

func TestDownloader(t *testing.T) {
	filename := "go1.22.4.windows-amd64.zip"
	sha256 := "26321c4d945a0035d8a5bc4a1965b0df401ff8ceac66ce2daadabf9030419a98"
	dest := os.TempDir()

	if err := DownloadGoVersion(filename, sha256, dest); err != nil {
		fmt.Printf("Error downloading file: %v\n", err)
	} else {
		fmt.Println("File downloaded successfully")
	}
}
