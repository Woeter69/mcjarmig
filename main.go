// Package main implements mcjarmig, a concurrent CLI tool that automatically
// updates Minecraft mods (.jar files) from an older game version to a newer game
// version using the Modrinth v2 API.
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config holds the command-line flags and runtime configuration for mcjarmig.
type Config struct {
	ModsDir    string
	Version    string
	Loader     string
	Workers    int
	OldModsDir string
	Token      string
}

// UpdateRequest represents the JSON body sent to Modrinth's version_file update endpoint.
type UpdateRequest struct {
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions,omitempty"`
}

// ModrinthFile represents a downloadable file inside a Modrinth Version response.
type ModrinthFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
}

// VersionResponse represents the relevant fields returned by Modrinth's version_file update endpoint.
type VersionResponse struct {
	Files []ModrinthFile `json:"files"`
}

// ModJob represents a single mod .jar file to be processed by a worker.
type ModJob struct {
	FilePath string
	Filename string
}

// ResultStatus categorizes the outcome of a processed mod file.
type ResultStatus int

const (
	StatusUpdated ResultStatus = iota
	StatusUpToDate
	StatusNoUpdate
	StatusError
)

// ModResult holds the processing summary for a single mod.
type ModResult struct {
	Job         ModJob
	Status      ResultStatus
	NewFilename string
	Message     string
}

const (
	// modrinthUpdateAPI is the base URL template for the Modrinth version_file update endpoint.
	modrinthUpdateAPI = "https://api.modrinth.com/v2/version_file/%s/update"
	// userAgent is required by Modrinth API guidelines to identify our client.
	userAgent = "mcjarmig/1.0 (CLI Tool)"
)

// fileOpsMutex ensures thread-safe file writing and moving operations across concurrent workers.
// This prevents multiple goroutines from simultaneously writing or moving the same file path.
var fileOpsMutex sync.Mutex

func main() {
	// 1. Parse CLI flags
	cfg := parseFlags()

	// Validate required flags
	if cfg.Loader == "" {
		fmt.Println("Error: -loader flag cannot be empty.")
		fmt.Println("\nUsage example:")
		fmt.Println("  mcjarmig -dir ./mods -version latest -loader fabric -workers 5")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}

	// 2. Prepare directories and scan for .jar files
	cfg.OldModsDir = filepath.Join(cfg.ModsDir, "old_mods")
	if err := os.MkdirAll(cfg.OldModsDir, 0755); err != nil {
		fmt.Printf("[Fatal] Failed to create old_mods directory at %s: %v\n", cfg.OldModsDir, err)
		os.Exit(1)
	}

	jarFiles, err := scanModsDir(cfg.ModsDir)
	if err != nil {
		fmt.Printf("[Fatal] Failed to scan mods directory %s: %v\n", cfg.ModsDir, err)
		os.Exit(1)
	}

	if len(jarFiles) == 0 {
		fmt.Printf("No .jar files found in directory: %s\n", cfg.ModsDir)
		return
	}

	fmt.Printf("Starting mcjarmig...\n")
	targetVerDisplay := cfg.Version
	if strings.ToLower(cfg.Version) == "latest" || cfg.Version == "" {
		targetVerDisplay = "latest available (any game version)"
	}
	fmt.Printf("Target Version: %s | Target Loader: %s | Workers: %d\n", targetVerDisplay, cfg.Loader, cfg.Workers)
	fmt.Printf("Found %d mod(s) in %s\n\n", len(jarFiles), cfg.ModsDir)

	// Configure HTTP client with a sensible timeout (do not use default client without timeout)
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 3. Set up Worker Pool using channels and sync.WaitGroup
	jobs := make(chan ModJob, len(jarFiles))
	results := make(chan ModResult, len(jarFiles))
	var wg sync.WaitGroup

	// Launch worker goroutines
	for i := 1; i <= cfg.Workers; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg, cfg, httpClient)
	}

	// Enqueue jobs
	for _, job := range jarFiles {
		jobs <- job
	}
	close(jobs)

	// Wait for all workers in a separate goroutine and close results channel when done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect and aggregate results
	var allResults []ModResult
	for res := range results {
		allResults = append(allResults, res)
	}

	// Print comprehensive summary report
	printSummaryReport(allResults)
}

// parseFlags sets up and parses standard CLI flags.
func parseFlags() *Config {
	cfg := &Config{}
	defaultDir := defaultModsDir()
	flag.StringVar(&cfg.ModsDir, "dir", defaultDir, "Path to the mods folder")
	flag.StringVar(&cfg.Version, "version", "latest", "Target Minecraft version to migrate to (e.g., '1.21.1' or 'latest')")
	flag.StringVar(&cfg.Loader, "loader", "fabric", "Target mod loader (e.g., 'fabric', 'forge', 'neoforge')")
	flag.IntVar(&cfg.Workers, "workers", 5, "Number of concurrent workers for API querying and downloading")
	flag.StringVar(&cfg.Token, "token", os.Getenv("MODRINTH_TOKEN"), "Optional Modrinth API token for private mods or higher rate limits (env: MODRINTH_TOKEN)")
	flag.Parse()
	cfg.ModsDir = resolvePath(cfg.ModsDir)
	return cfg
}

// defaultModsDir determines the default Minecraft mods path, prioritizing the Windows APPDATA path:
// Windows: %APPDATA%/.minecraft/mods (e.g. C:\Users\<USER>\AppData\Roaming\.minecraft\mods)
// If APPDATA is set in the environment, it returns the expanded AppData/.minecraft/mods path.
// Otherwise (if not on Windows or APPDATA is unset), it returns "%APPDATA%/.minecraft/mods" as the universal default string.
func defaultModsDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, ".minecraft", "mods")
	}
	if userProfile := os.Getenv("USERPROFILE"); userProfile != "" && runtime.GOOS == "windows" {
		return filepath.Join(userProfile, "AppData", "Roaming", ".minecraft", "mods")
	}
	return "%APPDATA%/.minecraft/mods"
}

// resolvePath expands %APPDATA%, %USERPROFILE%, and ~ prefixes into absolute system paths if present.
func resolvePath(path string) string {
	if strings.HasPrefix(path, "%APPDATA%") || strings.HasPrefix(path, "$APPDATA") {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(path, "%APPDATA%"), "$APPDATA")
		trimmed = strings.TrimPrefix(trimmed, "/")
		trimmed = strings.TrimPrefix(trimmed, "\\")
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, trimmed)
		} else if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
			return filepath.Join(userProfile, "AppData", "Roaming", trimmed)
		} else if home, err := os.UserHomeDir(); err == nil && home != "" {
			// Fallback resolution on Linux/macOS when %APPDATA% path is passed or defaulted
			return filepath.Join(home, trimmed)
		}
	}
	if strings.HasPrefix(path, "~") {
		trimmed := strings.TrimPrefix(path, "~")
		trimmed = strings.TrimPrefix(trimmed, "/")
		trimmed = strings.TrimPrefix(trimmed, "\\")
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, trimmed)
		}
	}
	return os.ExpandEnv(path)
}

// scanModsDir finds all .jar files in the specified directory (non-recursive, ignoring subdirectories like old_mods).
func scanModsDir(dir string) ([]ModJob, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var jobs []ModJob
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".jar") {
			jobs = append(jobs, ModJob{
				FilePath: filepath.Join(dir, name),
				Filename: name,
			})
		}
	}
	return jobs, nil
}

// worker processes mod jobs from the jobs channel until the channel is closed.
func worker(id int, jobs <-chan ModJob, results chan<- ModResult, wg *sync.WaitGroup, cfg *Config, client *http.Client) {
	defer wg.Done()

	for job := range jobs {
		processMod(job, results, cfg, client)
	}
}

// processMod handles the full workflow for a single mod: hashing, querying Modrinth API,
// downloading the updated jar, and moving the old jar into old_mods/.
func processMod(job ModJob, results chan<- ModResult, cfg *Config, client *http.Client) {
	// 1. Calculate SHA-1 hash in chunks
	hash, err := calculateSHA1(job.FilePath)
	if err != nil {
		msg := fmt.Sprintf("Failed to calculate SHA-1: %v", err)
		fmt.Printf("[Error] %s: %s\n", job.Filename, msg)
		results <- ModResult{Job: job, Status: StatusError, Message: msg}
		return
	}

	// 2. Query Modrinth API for updated version
	updateResp, status, err := queryModrinthUpdate(client, hash, cfg)
	if err != nil {
		msg := fmt.Sprintf("API request failed (hash: %s): %v", hash, err)
		fmt.Printf("[Error] %s: %s\n", job.Filename, msg)
		results <- ModResult{Job: job, Status: StatusError, Message: msg}
		return
	}

	// Handle 404 No update found
	if status == http.StatusNotFound {
		fmt.Printf("[Skip] No update found for %s\n", job.Filename)
		results <- ModResult{Job: job, Status: StatusNoUpdate}
		return
	}

	// Handle unexpected HTTP status codes
	if status != http.StatusOK {
		msg := fmt.Sprintf("Unexpected API status %d", status)
		fmt.Printf("[Error] %s: %s\n", job.Filename, msg)
		results <- ModResult{Job: job, Status: StatusError, Message: msg}
		return
	}

	// Validate response has downloadable files
	if updateResp == nil || len(updateResp.Files) == 0 {
		fmt.Printf("[Skip] No downloadable files in update response for %s\n", job.Filename)
		results <- ModResult{Job: job, Status: StatusNoUpdate, Message: "No downloadable files in update response"}
		return
	}

	// Pick the primary file or default to the first file in the array
	targetFile := selectTargetFile(updateResp.Files)
	if targetFile.URL == "" || targetFile.Filename == "" {
		msg := "Invalid file metadata received for mod"
		fmt.Printf("[Error] %s: %s\n", job.Filename, msg)
		results <- ModResult{Job: job, Status: StatusError, Message: msg}
		return
	}

	// If the updated filename is identical to the current file, we still may want to verify or skip,
	// but normally Modrinth returns new filenames. If it happens to be the exact same file path,
	// we avoid moving it onto itself.
	if targetFile.Filename == job.Filename {
		fmt.Printf("[Skip] %s is already the latest version matching criteria.\n", job.Filename)
		results <- ModResult{Job: job, Status: StatusUpToDate}
		return
	}

	// 3. Download the updated mod file and move the old mod file securely using thread-safe operations
	newFilePath := filepath.Join(cfg.ModsDir, targetFile.Filename)
	oldDestPath := filepath.Join(cfg.OldModsDir, job.Filename)

	// Perform download using temporary buffer or temp file to avoid partial writes locking destination
	if err := downloadAndReplace(client, targetFile.URL, job.FilePath, newFilePath, oldDestPath, cfg); err != nil {
		msg := fmt.Sprintf("Failed during download/update: %v", err)
		fmt.Printf("[Error] %s: %s\n", job.Filename, msg)
		results <- ModResult{Job: job, Status: StatusError, Message: msg}
		return
	}

	fmt.Printf("[Success] Updated %s -> %s\n", job.Filename, targetFile.Filename)
	results <- ModResult{Job: job, Status: StatusUpdated, NewFilename: targetFile.Filename}
}

// printSummaryReport formats and displays the categorized results of the migration process.
func printSummaryReport(results []ModResult) {
	var updated, upToDate, noUpdate, errors []ModResult

	for _, r := range results {
		switch r.Status {
		case StatusUpdated:
			updated = append(updated, r)
		case StatusUpToDate:
			upToDate = append(upToDate, r)
		case StatusNoUpdate:
			noUpdate = append(noUpdate, r)
		case StatusError:
			errors = append(errors, r)
		}
	}

	// Sort slices alphabetically by original filename for clean presentation
	sortByName := func(slice []ModResult) {
		sort.Slice(slice, func(i, j int) bool {
			return strings.ToLower(slice[i].Job.Filename) < strings.ToLower(slice[j].Job.Filename)
		})
	}
	sortByName(updated)
	sortByName(upToDate)
	sortByName(noUpdate)
	sortByName(errors)

	fmt.Println("\n================================================================================")
	fmt.Println("                           MIGRATION SUMMARY REPORT                             ")
	fmt.Println("================================================================================")
	fmt.Printf("Total Mods Scanned : %d\n", len(results))
	fmt.Printf("Updated            : %d\n", len(updated))
	fmt.Printf("Already Up-to-Date : %d\n", len(upToDate))
	fmt.Printf("No Update Found    : %d\n", len(noUpdate))
	fmt.Printf("Errors / Failed    : %d\n", len(errors))
	fmt.Println("--------------------------------------------------------------------------------")

	if len(updated) > 0 {
		fmt.Printf("\n[✔] UPDATED (%d)\n", len(updated))
		for _, r := range updated {
			fmt.Printf("  • %s -> %s\n", r.Job.Filename, r.NewFilename)
		}
	}

	if len(upToDate) > 0 {
		fmt.Printf("\n[=] ALREADY UP-TO-DATE (%d)\n", len(upToDate))
		for _, r := range upToDate {
			fmt.Printf("  • %s\n", r.Job.Filename)
		}
	}

	if len(noUpdate) > 0 {
		fmt.Printf("\n[∅] NO UPDATE FOUND ON MODRINTH (%d)\n", len(noUpdate))
		for _, r := range noUpdate {
			if r.Message != "" && r.Message != "No downloadable files in update response" {
				fmt.Printf("  • %s (%s)\n", r.Job.Filename, r.Message)
			} else {
				fmt.Printf("  • %s\n", r.Job.Filename)
			}
		}
	}

	if len(errors) > 0 {
		fmt.Printf("\n[✖] ERRORS / FAILED (%d)\n", len(errors))
		for _, r := range errors {
			fmt.Printf("  • %s: %s\n", r.Job.Filename, r.Message)
		}
	}

	fmt.Println("\n================================================================================")
}

// calculateSHA1 streams the file in chunks via io.Copy to compute the SHA-1 checksum without
// loading the entire file into memory at once.
func calculateSHA1(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// queryModrinthUpdate sends a POST request to Modrinth API's update endpoint with the target loaders and versions.
func queryModrinthUpdate(client *http.Client, hash string, cfg *Config) (*VersionResponse, int, error) {
	url := fmt.Sprintf(modrinthUpdateAPI, hash)

	reqBody := UpdateRequest{
		Loaders: []string{cfg.Loader},
	}
	if cfg.Version != "" && strings.ToLower(cfg.Version) != "latest" {
		reqBody.GameVersions = []string{cfg.Version}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if cfg != nil && cfg.Token != "" {
		req.Header.Set("Authorization", cfg.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, resp.StatusCode, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}

	var verResp VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&verResp); err != nil {
		return nil, resp.StatusCode, err
	}

	return &verResp, resp.StatusCode, nil
}

// selectTargetFile selects the primary file from the Modrinth files list if specified,
// otherwise falls back to files[0] as per specification.
func selectTargetFile(files []ModrinthFile) ModrinthFile {
	for _, f := range files {
		if f.Primary {
			return f
		}
	}
	return files[0]
}

// downloadAndReplace downloads the file from downloadURL to newFilePath and moves oldFilePath to oldDestPath.
// Thread safety is ensured via fileOpsMutex so concurrent workers do not collide on disk operations.
func downloadAndReplace(client *http.Client, downloadURL, oldFilePath, newFilePath, oldDestPath string, cfg *Config) error {
	// First, download the file into a temporary file or memory to verify successful network download
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("creating download request failed: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	if cfg != nil && cfg.Token != "" {
		req.Header.Set("Authorization", cfg.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned unexpected status %d", resp.StatusCode)
	}

	// Create a temporary file inside the old_mods folder or system temp to hold downloaded content
	tempFile, err := os.CreateTemp(filepath.Dir(oldDestPath), "mod_download_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // Clean up temp file on exit or error if not renamed

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed writing downloaded data to temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("closing temp file failed: %w", err)
	}

	// Critical section: thread-safe file operations on the mods directory and old_mods directory
	fileOpsMutex.Lock()
	defer fileOpsMutex.Unlock()

	// Move the old jar file to old_mods/ folder
	if err := moveFile(oldFilePath, oldDestPath); err != nil {
		return fmt.Errorf("failed to move old mod file: %w", err)
	}

	// Move downloaded temp file to newFilePath
	if err := moveFile(tempPath, newFilePath); err != nil {
		// Attempt rollback: if placing new file failed, try moving old file back from old_mods/
		_ = moveFile(oldDestPath, oldFilePath)
		return fmt.Errorf("failed to move new mod file to destination: %w", err)
	}

	return nil
}

// moveFile renames a file from src to dst across filesystems if needed.
// If os.Rename fails (e.g. across different mount points), it falls back to copy + remove.
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Fallback to copy if rename across devices or filesystem boundaries fails
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Close files before deleting source
	sourceFile.Close()
	destFile.Close()

	return os.Remove(src)
}
