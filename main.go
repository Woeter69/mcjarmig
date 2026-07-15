package main

import (
	"fmt"
)

type Config struct {
	ModsDir    string
	Version    string
	Loader     string
	Workers    int
	OldModsDir string
}

func main() {
	fmt.Println("ModMigrator initializing...")
}
