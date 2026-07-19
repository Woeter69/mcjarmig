# Changelog

All notable changes to this project will be documented in this file.

## [0.1.1] - 2026-07-19

### Changed
- **Project Renaming**: Renamed project, CLI binary, Go module, User-Agent, and documentation from `modmigrator` to `mcjarmig`.

## [0.1.0] - 2026-07-19

### Added
- **Project Initialization**: Initialized Go module `mcjarmig` (`go.mod`) with Go 1.26 compatibility.
- **Documentation (`README.md`)**: Added comprehensive project README detailing features, cross-platform paths, CLI flags, usage examples, and architecture.
- **Core CLI Architecture (`main.go`)**:
  - Implemented command-line flag parsing (`-dir`, `-version`, `-loader`, `-workers`) using standard `flag` package.
  - Added concurrent worker pool pattern using `chan ModJob` and `sync.WaitGroup` to process mod `.jar` files in parallel.

### Changed
- **Default Mod Directory Path (`-dir`)**:
  - Updated `-dir` flag default value from `"./mods"` to automatically detect the OS-specific default Minecraft mod directory (`defaultModsDir()`):
    - **Windows**: `%APPDATA%/.minecraft/mods`
    - **macOS**: `~/Library/Application Support/minecraft/mods`
    - **Linux**: `~/.minecraft/mods`
  - Users can still explicitly override the directory by passing `-dir <path>`.
- **Default Mod Loader (`-loader`)**:
  - Set default value to `"fabric"` so `-loader` is no longer required when using Fabric.
- **Default Minecraft Version (`-version`)**:
  - Set default value to `"latest"`, updating mods from whatever current version exists to the absolute latest version available on Modrinth across any Minecraft game version (omitting `game_versions` filter when `latest`). Users can still target a specific game version (e.g. `-version 1.21.1`) when needed.
- **File Hashing Module**:
  - Implemented memory-efficient SHA-1 hashing (`calculateSHA1`) streaming files in chunks via `io.Copy`.
- **Modrinth v2 API Integration**:
  - Added POST client to `https://api.modrinth.com/v2/version_file/{hash}/update` with custom `User-Agent` and `Content-Type: application/json` headers.
  - Implemented structured JSON request (`UpdateRequest`) and response (`VersionResponse`, `ModrinthFile`) models with proper struct tags.
  - Handled HTTP timeouts explicitly via configured `net/http.Client`.
- **Thread-Safe File Management & Migration Workflow**:
  - Added automatic creation of `old_mods/` backup folder inside the target directory.
  - Implemented `downloadAndReplace` to download updated `.jar` files using temporary files and atomically move old `.jar` files to `old_mods/`.
  - Added `fileOpsMutex` (`sync.Mutex`) across disk modification paths to guarantee thread-safe file operations and prevent race conditions or simultaneous writes/renames across concurrent goroutines.
  - Added fallback cross-filesystem file copy/move helper (`moveFile`) to handle across-device boundaries safely.
