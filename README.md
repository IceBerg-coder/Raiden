# Raiden
Raiden — Blazingly fast parallel downloader for the modern terminal. `rdn` gets your files in a flash using optimized goroutines.

⚡ **Lightning Concurrency**: Uses a pool of worker goroutines to pull segments simultaneously.

🎯 **Smart Segmenting**: Automatically calculates optimal chunk sizes based on file size and server capabilities.

🧠 **Low Footprint**: Written in Go with zero-copy principles to ensure minimal CPU and RAM usage even at Gigabit speeds.

🕹️ **Intuitive CLI**: Just use `rdn <url>` and let the lightning do the work.

## Features

- **Parallel Segmented Downloads**: Splits files into chunks and downloads them concurrently for maximum speed
- **Automatic Mode Selection**: Uses parallel mode for large files, single connection for small ones
- **Real-time Progress Bar**: Shows download progress, speed, and ETA
- **HTTP Range Requests**: Leverages server capabilities for efficient segmented downloading
- **File Verification**: Validates downloaded file size matches expected size
- **Configurable Workers**: Adjust the number of concurrent download workers (default: 8)

## Installation

### Build from source

```bash
git clone https://github.com/IceBerg-coder/Raiden.git
cd Raiden
go build -o rdn .
```

### Usage

```bash
# Basic download
./rdn https://example.com/file.zip

# Download with custom output filename
./rdn https://example.com/file.zip myfile.zip

# Make it available system-wide
sudo mv rdn /usr/local/bin/
rdn https://example.com/file.zip
```

### Install via Ubuntu PPA

```bash
sudo add-apt-repository ppa:broodzeus/raiden
sudo apt update
sudo apt install raiden
```

## How It Works

1. **File Info Check**: Sends a `HEAD` request to determine file size and server capabilities
2. **Mode Selection**: 
   - Files > 1MB with `Accept-Ranges: bytes` → **Parallel Mode** (8 workers by default)
   - Small files or servers without range support → **Single Connection Mode**
3. **Chunk Calculation**: Divides the file into optimal chunks for each worker
4. **Concurrent Download**: Each worker downloads its chunk via HTTP Range requests
5. **Merge**: Combines all chunks into the final output file
6. **Verify**: Confirms the final file size matches expectations

## Architecture

```
rdn <url>
    │
    ├─▶ HEAD request → Get file size & capabilities
    │
    ├─▶ Mode Decision
    │     ├─ Parallel (large files with range support)
    │     └─ Single (small files or no range support)
    │
    ├─▶ Download
    │     ├─ Split into N chunks (N = workers)
    │     ├─ Worker 1 → Chunk 1 (bytes 0-1249999)
    │     ├─ Worker 2 → Chunk 2 (bytes 1250000-2499999)
    │     └─ ...
    │
    └─▶ Merge & Verify → Final file
```

## Configuration

Edit `downloader.go` to customize:

```go
const (
    DefaultWorkers     = 8      // Number of concurrent workers
    MinChunkSize int64 = 1048576 // 1 MB minimum chunk size
)
```

## Requirements

- Go 1.21 or higher (for building)
- No external dependencies

## License

MIT License - see LICENSE file for details.
