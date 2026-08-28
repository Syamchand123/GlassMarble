package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/product"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	Body    string        `json:"body"`
	HTMLURL string        `json:"html_url"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateJSON struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	UpToDate       bool   `json:"up_to_date"`
	Updated        bool   `json:"updated"`
	BinaryPath     string `json:"binary_path"`
	ReleaseURL     string `json:"release_url,omitempty"`
	Platform       string `json:"platform"`
	Message        string `json:"message"`
}

var (
	updateCheckFlag bool
	updateForceFlag bool
	updateTagFlag   string
	updateJSONFlag  bool
)

var updateCmd = &cobra.Command{
	Use:     "update",
	GroupID: GroupUtility.ID,
	Short:   "Check for and install the latest release of GlassMarble",
	Long: `Automatically checks GitHub releases, verifies SHA256 checksums,
and updates the local GlassMarble binary for your operating system and architecture.`,
	Example: `  # Update to latest available release
  gmb update

  # Check if a new version is available without installing
  gmb update --check

  # Force reinstall the latest version
  gmb update --force

  # Install a specific release tag
  gmb update --tag v1.0.0

  # Output update status as JSON
  gmb update --check --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		currentVer := product.Version
		cleanCurrent := strings.TrimPrefix(currentVer, "v")
		osName := runtime.GOOS
		archName := runtime.GOARCH
		platformStr := fmt.Sprintf("%s/%s", osName, archName)

		execPath, err := os.Executable()
		if err != nil {
			return producterrs.Annotate(fmt.Errorf("failed to locate running executable: %w", err), producterrs.ErrValidation)
		}
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}

		if !updateJSONFlag {
			fmt.Printf("Checking for GlassMarble updates (current: %s, platform: %s)...\n", currentVer, platformStr)
		}

		release, err := fetchRelease(updateTagFlag)
		if err != nil {
			return fmt.Errorf("failed to fetch release information: %w", err)
		}

		latestVer := release.TagName
		cleanLatest := strings.TrimPrefix(latestVer, "v")

		isUpToDate := (cleanCurrent == cleanLatest) && (cleanCurrent != "0.1.0" && cleanCurrent != "dev")

		if updateCheckFlag {
			if updateJSONFlag {
				out, _ := json.MarshalIndent(updateJSON{
					CurrentVersion: currentVer,
					LatestVersion:  latestVer,
					UpToDate:       isUpToDate,
					Updated:        false,
					BinaryPath:     execPath,
					ReleaseURL:     release.HTMLURL,
					Platform:       platformStr,
					Message:        fmt.Sprintf("Current: %s, Latest: %s", currentVer, latestVer),
				}, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			if isUpToDate && !updateForceFlag {
				fmt.Println(views.RenderUpdateAlreadyLatest(currentVer, execPath))
			} else {
				fmt.Println(views.RenderUpdateCheckAvailable(currentVer, latestVer, release.HTMLURL))
			}
			return nil
		}

		if isUpToDate && !updateForceFlag {
			if updateJSONFlag {
				out, _ := json.MarshalIndent(updateJSON{
					CurrentVersion: currentVer,
					LatestVersion:  latestVer,
					UpToDate:       true,
					Updated:        false,
					BinaryPath:     execPath,
					ReleaseURL:     release.HTMLURL,
					Platform:       platformStr,
					Message:        "Already on the latest version",
				}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Println(views.RenderUpdateAlreadyLatest(currentVer, execPath))
			return nil
		}

		// Locate target archive and checksums
		archiveExt := "tar.gz"
		if osName == "windows" {
			archiveExt = "zip"
		}
		expectedArchivePrefix := fmt.Sprintf("gmb_%s_%s_%s", cleanLatest, osName, archName)

		var archiveURL, checksumsURL string
		for _, asset := range release.Assets {
			if strings.HasPrefix(asset.Name, expectedArchivePrefix) && strings.HasSuffix(asset.Name, "."+archiveExt) {
				archiveURL = asset.BrowserDownloadURL
			}
			if asset.Name == "checksums.txt" {
				checksumsURL = asset.BrowserDownloadURL
			}
		}

		if archiveURL == "" {
			return producterrs.Tagged(fmt.Sprintf("no release asset found for platform %s (%s)", platformStr, expectedArchivePrefix+"."+archiveExt), producterrs.ErrValidation)
		}

		if !updateJSONFlag {
			fmt.Printf("Downloading %s from %s...\n", expectedArchivePrefix+"."+archiveExt, release.TagName)
		}

		tempDir, err := os.MkdirTemp("", "gmb-update-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tempDir)

		archivePath := filepath.Join(tempDir, expectedArchivePrefix+"."+archiveExt)
		if err := downloadFile(archiveURL, archivePath); err != nil {
			return fmt.Errorf("failed to download release archive: %w", err)
		}

		// Verify SHA256 against checksums.txt if available
		if checksumsURL != "" {
			if !updateJSONFlag {
				fmt.Println("Verifying SHA256 checksums...")
			}
			checksumsData, err := downloadBytes(checksumsURL)
			if err == nil {
				computedHash, hashErr := computeFileSHA256(archivePath)
				if hashErr == nil {
					verified := verifyChecksum(checksumsData, filepath.Base(archivePath), computedHash)
					if !verified {
						return producterrs.Tagged(fmt.Sprintf("SHA256 checksum mismatch for %s (computed: %s) — aborting for security", filepath.Base(archivePath), computedHash), producterrs.ErrValidation)
					}
					if !updateJSONFlag {
						fmt.Printf("SHA256 checksum verified: %s\n", computedHash)
					}
				}
			}
		}

		if !updateJSONFlag {
			fmt.Println("Extracting binary...")
		}

		extractedBin, err := extractBinary(archivePath, osName, tempDir)
		if err != nil {
			return fmt.Errorf("failed to extract binary: %w", err)
		}

		if !updateJSONFlag {
			fmt.Printf("Installing updated binary to %s...\n", execPath)
		}

		if err := replaceExecutable(execPath, extractedBin); err != nil {
			return fmt.Errorf("failed to replace executable: %w", err)
		}

		if updateJSONFlag {
			out, _ := json.MarshalIndent(updateJSON{
				CurrentVersion: currentVer,
				LatestVersion:  latestVer,
				UpToDate:       true,
				Updated:        true,
				BinaryPath:     execPath,
				ReleaseURL:     release.HTMLURL,
				Platform:       platformStr,
				Message:        "GlassMarble updated successfully",
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println("\n" + views.RenderUpdateSuccess(views.UpdateData{
			CurrentVersion: currentVer,
			LatestVersion:  latestVer,
			BinaryPath:     execPath,
			OS:             osName,
			Arch:           archName,
			ReleaseURL:     release.HTMLURL,
			ReleaseNotes:   release.Body,
		}))

		return nil
	},
}

func fetchRelease(tag string) (*githubRelease, error) {
	url := "https://api.github.com/repos/Syamchand123/GlassMarble/releases/latest"
	if tag != "" {
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		url = fmt.Sprintf("https://api.github.com/repos/Syamchand123/GlassMarble/releases/tags/%s", tag)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GlassMarble-CLI/"+product.Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d for %s", resp.StatusCode, url)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to parse release JSON: %w", err)
	}
	return &rel, nil
}

func downloadFile(url, destPath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "GlassMarble-CLI/"+product.Version)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP GET returned status %d for %s", resp.StatusCode, url)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func downloadBytes(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GlassMarble-CLI/"+product.Version)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func computeFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyChecksum(checksumsData []byte, filename, expectedHex string) bool {
	lines := strings.Split(string(checksumsData), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			hash := fields[0]
			file := strings.TrimPrefix(fields[1], "*")
			if strings.EqualFold(filepath.Base(file), filename) {
				return strings.EqualFold(hash, expectedHex)
			}
		}
	}
	return false
}

func extractBinary(archivePath, osName, destDir string) (string, error) {
	expectedBinName := "gmb"
	if osName == "windows" {
		expectedBinName = "gmb.exe"
	}

	if strings.HasSuffix(archivePath, ".zip") {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return "", err
		}
		defer r.Close()

		for _, f := range r.File {
			if strings.EqualFold(filepath.Base(f.Name), expectedBinName) {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				defer rc.Close()

				outPath := filepath.Join(destDir, expectedBinName)
				outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
				if err != nil {
					return "", err
				}
				defer outFile.Close()

				if _, err := io.Copy(outFile, rc); err != nil {
					return "", err
				}
				return outPath, nil
			}
		}
	} else if strings.HasSuffix(archivePath, ".tar.gz") {
		f, err := os.Open(archivePath)
		if err != nil {
			return "", err
		}
		defer f.Close()

		gzr, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if strings.EqualFold(filepath.Base(header.Name), expectedBinName) {
				outPath := filepath.Join(destDir, expectedBinName)
				outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
				if err != nil {
					return "", err
				}
				defer outFile.Close()

				if _, err := io.Copy(outFile, tr); err != nil {
					return "", err
				}
				return outPath, nil
			}
		}
	}

	return "", fmt.Errorf("could not find %s in release archive", expectedBinName)
}

func replaceExecutable(targetPath, newBinaryPath string) error {
	newBytes, err := os.ReadFile(newBinaryPath)
	if err != nil {
		return fmt.Errorf("failed to read extracted binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		// Windows file-locking workaround:
		// A running binary cannot be overwritten directly, but it CAN be renamed!
		oldPath := targetPath + ".old"
		_ = os.Remove(oldPath)

		if err := os.Rename(targetPath, oldPath); err != nil {
			return fmt.Errorf("failed to rename current executable: %w", err)
		}

		if err := os.WriteFile(targetPath, newBytes, 0755); err != nil {
			// Rollback rename if write fails
			_ = os.Rename(oldPath, targetPath)
			return fmt.Errorf("failed to write new binary: %w", err)
		}

		// Try removing .old; if locked, it's non-fatal and will be overwritten on next update
		_ = os.Remove(oldPath)
		return nil
	}

	// POSIX systems (Linux / macOS): write to temp and atomic rename
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, newBytes, 0755); err != nil {
		return fmt.Errorf("failed to write temp binary: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace binary via rename: %w", err)
	}

	return os.Chmod(targetPath, 0755)
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckFlag, "check", false, "Check for newer releases without downloading or installing")
	updateCmd.Flags().BoolVarP(&updateForceFlag, "force", "f", false, "Force reinstall even if already running the latest version")
	updateCmd.Flags().StringVarP(&updateTagFlag, "tag", "t", "", "Install a specific release tag (e.g. v1.0.0)")
	updateCmd.Flags().BoolVar(&updateJSONFlag, "json", false, "Emit machine-readable JSON output")

	rootCmd.AddCommand(updateCmd)
}
