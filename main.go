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

const (
	modrinthUpdateAPI = "https://api.modrinth.com/v2/version_file/%s/update"
	userAgent         = "ModMigrator/1.0 (CLI Tool)"
)

func main() {
	cfg := parseFlags()
	if cfg.Version == "" || cfg.Loader == "" {
		fmt.Println("Error: both -version and -loader flags are required.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	fmt.Printf("Starting ModMigrator for version %s with loader %s (client ready)
", cfg.Version, cfg.Loader, httpClient.Timeout)
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
