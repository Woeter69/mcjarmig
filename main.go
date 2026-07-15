package main

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	ModsDir    string
	Version    string
	Loader     string
	Workers    int
	OldModsDir string
}

func main() {
	cfg := parseFlags()
	if cfg.Version == "" || cfg.Loader == "" {
		fmt.Println("Error: both -version and -loader flags are required.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	fmt.Printf("Starting ModMigrator for version %s with loader %s
", cfg.Version, cfg.Loader)
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
