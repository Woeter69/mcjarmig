# mcjarmig

mcjarmig is a high-performance, concurrent CLI tool written in Go that automatically updates Minecraft mods (`.jar` files) from an older version to the newest version available on the **Modrinth v2 API**.

Whether you are upgrading an entire modpack to a new Minecraft update or just ensuring all your installed mods are on their latest release, mcjarmig handles file discovery, SHA-1 checksum identification, API querying, downloading, and backup migration seamlessly.

---

## Features

- **Cross-Platform Auto-Detection**: Automatically detects your OS-specific Minecraft mods folder (`%APPDATA%/.minecraft/mods` on Windows, `~/Library/Application Support/minecraft/mods` on macOS, and `~/.minecraft/mods` on Linux).
- **Concurrent Worker Pool**: Processes multiple mod files simultaneously across configurable worker threads (`-workers`) using channels and `sync.WaitGroup` for maximum speed.
- **Memory-Efficient Hashing**: Calculates SHA-1 hashes by streaming `.jar` files in chunks, ensuring minimal RAM usage even with large modpacks.
- **Modrinth v2 API Integration**: Queries `https://api.modrinth.com/v2/version_file/{hash}/update` to find the exact matching update for your mod loader and target game version.
- **Thread-Safe File Management**:
  - Automatically creates an `old_mods/` backup directory inside your mods folder and safely archives old `.jar` files before replacing them.
  - Uses atomic file locking (`sync.Mutex`) across workers to eliminate race conditions, corrupted downloads, or file write collisions.
  - Includes cross-filesystem copy/rename fallbacks to handle across-device boundaries.

---

## Installation

### Prerequisites
- [Go 1.21+](https://golang.org/dl/) (tested on Go 1.26+)

### Building from Source

Clone the repository and build using the Go CLI:

```bash
git clone https://github.com/Woeter/mcjarmig.git
cd mcjarmig
go build -o mcjarmig main.go
```

---

## Usage

mcjarmig comes with sensible defaults right out of the box. Simply running `./mcjarmig` will scan your system's default Minecraft mods folder and update all mods to their latest available Fabric releases.

```bash
# Update all mods in the default Minecraft folder to the latest available Fabric versions
./mcjarmig

# Update mods to a specific Minecraft game version (e.g., 1.21.1)
./mcjarmig -version 1.21.1

# Update mods for Forge or NeoForge
./mcjarmig -loader neoforge -version 1.21.1

# Specify a custom mods directory and use 10 concurrent download workers
./mcjarmig -dir /path/to/custom/mods -loader fabric -version latest -workers 10
```

### CLI Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-dir` | `%APPDATA%/.minecraft/mods` | Path to the target Minecraft mods folder (Windows `%APPDATA%/.minecraft/mods` by default). |
| `-loader` | `fabric` | Target mod loader (`fabric`, `forge`, `neoforge`, `quilt`). |
| `-version` | `latest` | Target Minecraft game version (`1.21.1`, `1.20.4`, or `latest` for any version). |
| `-workers` | `5` | Number of concurrent workers for API querying and downloading. |

---

## How It Works

1. **Scan**: mcjarmig scans the directory specified by `-dir` (ignoring subdirectories like `old_mods/`) for valid `.jar` files.
2. **Hash**: Each `.jar` file is streamed and hashed using `SHA-1`, which Modrinth uses to uniquely identify mod files.
3. **Query**: Each hash is queried against the Modrinth API (`POST /v2/version_file/{hash}/update`). If `-version` is set to `latest`, game version filtering is bypassed to return the absolute newest release for your loader.
4. **Download & Archive**: If an update is found, the new `.jar` file is downloaded to a temporary buffer, the old `.jar` file is moved into the `old_mods/` folder, and the new file is moved into place. All disk modifications are mutex-guaranteed for thread safety.

---

## License

This project is open-source and available under the MIT License.
