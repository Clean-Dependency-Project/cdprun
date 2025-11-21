package nodejs

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

)

const (
	shasumURL         = "https://nodejs.org/dist/v%s/SHASUMS256.txt"
	shasumURLSig      = "https://nodejs.org/dist/v%s/SHASUMS256.txt.sig"
	shasumURLAsc      = "https://nodejs.org/dist/v%s/SHASUMS256.txt.asc"
	classifier        = "nodejs"
	SHA256FileName    = "SHASUMS256.txt"
	SHA256FileNameSig = "SHASUMS256.txt.sig"
	SHA256FileNameAsc = "SHASUMS256.txt.asc"
	// HTTPTimeout is the timeout for all HTTP requests
	HTTPTimeout = 300// seconds
)

var httpClient = &http.Client{Timeout: HTTPTimeout * time.Second}

var semverRegex = regexp.MustCompile(`node-v(\d+\.\d+\.\d+)`)

type Version interface {
	GetVersion() string
}

// DownloadOptions represents the configuration for downloading Node.js
type DownloadOptions struct {
	Version  string
	Arch     string
	Platform string
	Output   string
}

// DownloadAndParseSHASUMS downloads and parses the SHASUMS256.txt file for a given version.
func DownloadAndParseSHASUMS(opts DownloadOptions) (map[string]string, string, error) {
	var wg sync.WaitGroup
	errChan := make(chan error, 3)
	filePaths := make(map[string]string)

	// Define URLs and corresponding file names
	urls := map[string]string{
		fmt.Sprintf(shasumURL, opts.Version):    SHA256FileName,
		fmt.Sprintf(shasumURLSig, opts.Version): SHA256FileNameSig,
		fmt.Sprintf(shasumURLAsc, opts.Version): SHA256FileNameAsc,
	}

	// Download files concurrently
	for url, fileName := range urls {
		wg.Add(1)
		go func(url, fileName string) {
			defer wg.Done()
			resp, err := httpClient.Get(url)
			if err != nil {
				errChan <- fmt.Errorf("failed to download %s: %w", fileName, err)
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				errChan <- fmt.Errorf("failed to get %s: %s", fileName, resp.Status)
				return
			}

			filePath := filepath.Join(opts.Output, fileName)
			out, err := os.Create(filePath)
			if err != nil {
				errChan <- fmt.Errorf("failed to create %s: %w", fileName, err)
				return
			}
			defer func() { _ = out.Close() }()

			if _, err := io.Copy(out, resp.Body); err != nil {
				errChan <- fmt.Errorf("failed to save %s: %w", fileName, err)
				return
			}

			filePaths[fileName] = filePath
		}(url, fileName)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return nil, "", err
		}
	}

	// Process SHASUMS256.txt
	shaSumFile := filePaths[SHA256FileName]
	file, err := os.Open(shaSumFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open SHASUMS256.txt: %w", err)
	}
	defer func() { _ = file.Close() }()

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 {
			checksums[parts[1]] = parts[0]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("error reading SHASUMS256.txt: %w", err)
	}

	return checksums, shaSumFile, nil
}

// Download downloads the specified Node.js binary and returns (verified, checksum, error)
func Download(opts DownloadOptions) (bool, string, error) {
	url, remoteFileName := BuildDownloadURL(opts)
	if url == "" {
		return false, "", fmt.Errorf("unsupported platform/arch combination")
	}
	fileName := filepath.Join(opts.Output, remoteFileName)

	checksums, _, err := DownloadAndParseSHASUMS(opts)
	if err != nil {
		return false, "", fmt.Errorf("failed to download and parse shasums %w", err)
	}
	checksum := checksums[remoteFileName]
	if checksum == "" {
		return false, "", nil
	}

	if err := os.MkdirAll(opts.Output, 0755); err != nil {
		return false, "", fmt.Errorf("failed to create output directory: %w", err)
	}

	resp, err := httpClient.Head(url)
	if err != nil {
		return false, "", fmt.Errorf("failed to get file info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, "", fmt.Errorf("version %s not found for %s-%s", opts.Version, opts.Platform, opts.Arch)
	}

	out, err := os.Create(fileName)
	if err != nil {
		return false, "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = out.Close() }()

	resp, err = httpClient.Get(url)
	if err != nil {
		return false, "", fmt.Errorf("failed to download Node.js: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("bad status: %s", resp.Status)
	}

	h := sha256.New()
	w := io.MultiWriter(out, h)

	var downloaded int64
	reader := &ProgressReader{
		Reader: resp.Body,
		Reporter: func(r int64) {
			downloaded += r
		},
	}

	if _, err := io.Copy(w, reader); err != nil {
		return false, "", fmt.Errorf("failed to save file: %w", err)
	}

	if checksum != "" {
		actualChecksum := hex.EncodeToString(h.Sum(nil))
		if actualChecksum != checksum {
			return false, actualChecksum, fmt.Errorf("checksum verification failed. Expected: %s Actual: %s", checksum, actualChecksum)
		}
		return true, actualChecksum, nil
	}

	return true, checksum, nil
}

// Updated to handle correct platform names and exclude unsupported architectures
func BuildDownloadURL(opts DownloadOptions) (string, string) {
	var remoteFileName string

	switch opts.Platform {
	case "win":
		if opts.Arch == "x64" {
			remoteFileName = fmt.Sprintf("node-v%s-x64.msi", opts.Version)
		} else {
			return "", ""
		}
	case "mac":
		if opts.Arch == "x64" {
			remoteFileName = fmt.Sprintf("node-v%s.pkg", opts.Version)
		} else {
			return "", ""
		}
	case "linux":
		if opts.Arch == "x64" {
			remoteFileName = fmt.Sprintf("node-v%s.tar.gz", opts.Version)
		} else {
			return "", ""
		}
	default:
		return "", ""
	}

	url := fmt.Sprintf("https://nodejs.org/dist/v%s/%s", opts.Version, remoteFileName)
	return url, remoteFileName
}

// ProgressReader wraps an io.Reader to provide progress updates
type ProgressReader struct {
	Reader   io.Reader
	Reporter func(r int64)
}

func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	if n > 0 {
		pr.Reporter(int64(n))
	}
	return
}

// Updated to exclude unsupported architectures for macOS and Windows
func DownloadNodeJS(version, outputDir, platform, arch string, all bool) error {
	if all {
		supportedPlatforms := []string{"linux", "mac", "win"}
		supportedArchitectures := []string{"x64", "arm64"} // Exclude `aarch64` for macOS and Windows

		type result struct {
			platform string
			arch     string
			err      error
		}
		results := make(chan result, len(supportedPlatforms)*len(supportedArchitectures))
		var wg sync.WaitGroup

		for _, plat := range supportedPlatforms {
			for _, architecture := range supportedArchitectures {
				url, _ := BuildDownloadURL(DownloadOptions{Version: version, Platform: plat, Arch: architecture})
				if url == "" {
					continue // Skip unsupported combinations
				}

				wg.Add(1)
				go func(plat, architecture string) {
					defer wg.Done()
					options := DownloadOptions{
						Version:  version,
						Arch:     architecture,
						Platform: plat,
						Output:   outputDir,
					}

					verified, checksum, err := Download(options)
					results <- result{platform: plat, arch: architecture, err: err}
					if err == nil && verified {
						fmt.Printf("Checksum verified for %s-%s. Checksum: %s\n", plat, architecture, checksum)
					}
				}(plat, architecture)
			}
		}
		wg.Wait()
		close(results)

		// Optionally, aggregate errors or print summary
		for res := range results {
			if res.err != nil {
				return fmt.Errorf("error downloading for %s-%s: %v", res.platform, res.arch, res.err)
			}
		}
		return nil
	}

	options := DownloadOptions{
		Version:  version,
		Arch:     arch,
		Platform: platform,
		Output:   outputDir,
	}

	verified, checksum, err := Download(options)
	if err == nil && verified {
		fmt.Printf("Checksum verified for %s-%s. Checksum: %s\n", platform, arch, checksum)
	}
	return err
}

func GetVersion(fileName string) (string, error) {
	// Extracts semver like 22.15.0 from node-v22.15.0-x64.msi, node-v22.15.0.pkg, etc.
	matches := semverRegex.FindStringSubmatch(fileName)
	if len(matches) > 1 {
		return matches[1], nil
	}
	return "", fmt.Errorf("version not found in filename: %s", fileName)
}
