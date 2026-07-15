Act as an expert Go developer. I want to build a CLI tool called "ModMigrator" that automatically updates Minecraft mods from an older game version to a newer game version using the Modrinth API. 

Here are the complete technical requirements and specifications for the Go application:

1. CLI Flags & Inputs
- Use the standard `flag` package.
- `-dir`: The path to the mods folder (default: "./mods").
- `-version`: The target Minecraft version to migrate to (e.g., "1.21.1"). Required.
- `-loader`: The target mod loader (e.g., "fabric", "forge", "neoforge"). Required.
- `-workers`: Number of concurrent workers for API querying and downloading (default: 5).

2. Core Workflow
- Scan the directory provided in `-dir` for all `.jar` files.
- Ensure an `old_mods/` subdirectory exists (create it if it doesn't).
- Process the `.jar` files concurrently using a worker pool pattern (channels and `sync.WaitGroup`).

3. File Hashing (Crucial)
- For each `.jar` file, calculate its SHA-1 hash. Modrinth requires SHA-1 to identify the file.
- Use `crypto/sha1`. Load the file in chunks (e.g., using `io.Copy`) so you don't load massive files entirely into memory.

4. Modrinth API Integration
- Endpoint: POST `https://api.modrinth.com/v2/version_file/{hash}/update`
- Headers: Must include `Content-Type: application/json` and a custom `User-Agent: ModMigrator/1.0 (CLI Tool)`.
- Request Body (JSON): 
  {
    "loaders": ["<loader_from_flag>"],
    "game_versions": ["<version_from_flag>"]
  }
- Expected Response (200 OK): Contains a JSON object. We need to extract the primary file's download URL and filename.
  Relevant structure:
  {
    "files": [
      {
        "url": "https://cdn.modrinth.com/data/...",
        "filename": "new-mod-name-1.21.jar"
      }
    ]
  }

5. Processing API Results
- If the API returns 404: Print "[Skip] No update found for <filename>".
- If the API returns 200:
  a. Download the file from `files[0].url`.
  b. Save it to the `-dir` folder using `files[0].filename`.
  c. Move the original (old) `.jar` file into the `old_mods/` folder.
  d. Print "[Success] Updated <old_filename> -> <new_filename>".

6. Go Best Practices to Enforce
- Use standard library HTTP client (`net/http`). 
- Define proper struct tags for the JSON marshaling/unmarshaling.
- Handle HTTP timeouts (do not use the default HTTP client without a timeout).
- Ensure thread-safe file operations. Do not let two goroutines attempt to write/move the same file simultaneously.
- Cleanly handle errors (e.g., network failures, file permission issues) without crashing the whole program.

Write the complete `main.go` file. Ensure the code is heavily commented, properly formatted, and ready to be compiled.
