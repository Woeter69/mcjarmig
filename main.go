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
	"strings"
	"sync"
	"time"
)

type Config struct {
	ModsDir    string
	Version    string
	Loader     string
	Workers    int
	OldModsDir string
}

type UpdateRequest struct {
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
}

type ModrinthFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
}

type VersionResponse struct {
	Files []ModrinthFile `json:"files"`
}

type ModJob struct {
	FilePath string
	Filename string
}

const (
	modrinthUpdateAPI = "https://api.modrinth.com/v2/version_file/%s/update"
	userAgent         = "ModMigrator/1.0 (CLI Tool)"
)

var fileOpsMutex sync.Mutex

func main() {
	cfg := parseFlags()
	if cfg.Version == "" || cfg.Loader == "" {
		fmt.Println("Error: both -version and -loader flags are required.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	cfg.OldModsDir = filepath.Join(cfg.ModsDir, "old_mods")
	if err := os.MkdirAll(cfg.OldModsDir, 0755); err != nil {
		fmt.Printf("[Fatal] Failed to create old_mods directory: %v
", err)
		os.Exit(1)
	}
	jarFiles, err := scanModsDir(cfg.ModsDir)
	if err != nil {
		fmt.Printf("[Fatal] Failed to scan mods directory: %v
", err)
		os.Exit(1)
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	jobs := make(chan ModJob, len(jarFiles))
	var wg sync.WaitGroup
	for i := 1; i <= cfg.Workers; i++ {
		wg.Add(1)
		go worker(i, jobs, &wg, cfg, httpClient)
	}
	for _, job := range jarFiles {
		jobs <- job
	}
	close(jobs)
	wg.Wait()
}

func parseFlags() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.ModsDir, "dir", "./mods", "Path to the mods folder")
	flag.StringVar(&cfg.Version, "version", "", "Target Minecraft version to migrate to [Required]")
	flag.StringVar(&cfg.Loader, "loader", "", "Target mod loader [Required]")
	flag.IntVar(&cfg.Workers, "workers", 5, "Number of concurrent workers")
	flag.Parse()
	return cfg
}

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
			jobs = append(jobs, ModJob{FilePath: filepath.Join(dir, name), Filename: name})
		}
	}
	return jobs, nil
}

func worker(id int, jobs <-chan ModJob, wg *sync.WaitGroup, cfg *Config, client *http.Client) {
	defer wg.Done()
	for job := range jobs {
		processMod(job, cfg, client)
	}
}

func processMod(job ModJob, cfg *Config, client *http.Client) {
	hash, err := calculateSHA1(job.FilePath)
	if err != nil {
		return
	}
	updateResp, status, err := queryModrinthUpdate(client, hash, cfg.Version, cfg.Loader)
	if err != nil || status != http.StatusOK || updateResp == nil || len(updateResp.Files) == 0 {
		return
	}
	targetFile := selectTargetFile(updateResp.Files)
	if targetFile.Filename == job.Filename {
		return
	}
	newFilePath := filepath.Join(cfg.ModsDir, targetFile.Filename)
	oldDestPath := filepath.Join(cfg.OldModsDir, job.Filename)
	_ = downloadAndReplace(client, targetFile.URL, job.FilePath, newFilePath, oldDestPath)
}

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

func queryModrinthUpdate(client *http.Client, hash, version, loader string) (*VersionResponse, int, error) {
	url := fmt.Sprintf(modrinthUpdateAPI, hash)
	reqBody := UpdateRequest{
		Loaders:      []string{loader},
		GameVersions: []string{version},
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
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var verResp VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&verResp); err != nil {
		return nil, resp.StatusCode, err
	}
	return &verResp, resp.StatusCode, nil
}

func selectTargetFile(files []ModrinthFile) ModrinthFile {
	for _, f := range files {
		if f.Primary {
			return f
		}
	}
	return files[0]
}

func downloadAndReplace(client *http.Client, downloadURL, oldFilePath, newFilePath, oldDestPath string) error {
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return err
	}
	defer resp.Body.Close()
	tempFile, err := os.CreateTemp(filepath.Dir(oldDestPath), "mod_download_*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		return err
	}
	tempFile.Close()
	fileOpsMutex.Lock()
	defer fileOpsMutex.Unlock()
	if err := moveFile(oldFilePath, oldDestPath); err != nil {
		return err
	}
	return moveFile(tempPath, newFilePath)
}

func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
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
	sourceFile.Close()
	destFile.Close()
	return os.Remove(src)
}
