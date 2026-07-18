# Changelog

All notable changes to this project will be documented in this file.

## [0.1.0-rc1] - 2026-07-18

### Added
- Thread-safe file download and replacement pipeline (`downloadAndReplace`, `moveFile`).
- Mutex protection for concurrent disk operations (`fileOpsMutex`).
