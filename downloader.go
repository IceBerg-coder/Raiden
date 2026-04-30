package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultWorkers     = 8
	MinChunkSize int64 = 1024 * 1024 // 1 MB
	UserAgent          = "Raiden/1.0"
)

type Downloader struct {
	URL         string
	OutputFile  string
	Workers     int
	TotalSize   int64
	Downloaded  int64
	StartTime   time.Time
	mu          sync.Mutex
}

type chunk struct {
	start int64
	end   int64
	index int
}

func NewDownloader(url, outputFile string) *Downloader {
	return &Downloader{
		URL:        url,
		OutputFile: outputFile,
		Workers:    DefaultWorkers,
		StartTime:  time.Now(),
	}
}

func (d *Downloader) Download() error {
	// Get file size and check server support
	size, acceptRanges, err := d.getFileInfo()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	d.TotalSize = size

	// Determine output filename
	outputFile := d.OutputFile
	if outputFile == "" {
		outputFile = d.extractFilename()
	}

	fmt.Printf("Downloading: %s\n", d.URL)
	fmt.Printf("Output:      %s\n", outputFile)
	fmt.Printf("Size:        %s\n", formatSize(d.TotalSize))

	// Perform download
	if acceptRanges && d.TotalSize > MinChunkSize {
		fmt.Printf("Workers:     %d\n", d.Workers)
		fmt.Println("Mode:        Parallel (segmented)")
		if err := d.downloadParallel(outputFile); err != nil {
			return err
		}
	} else {
		fmt.Println("Mode:        Single connection")
		if err := d.downloadSingle(outputFile); err != nil {
			return err
		}
	}

	// Verify file
	if err := d.verifyFile(outputFile); err != nil {
		return err
	}

	elapsed := time.Since(d.StartTime)
	fmt.Printf("\n✓ Download complete in %v\n", elapsed.Round(time.Second))
	fmt.Printf("  Saved to: %s\n", outputFile)
	return nil
}

func (d *Downloader) getFileInfo() (int64, bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("HEAD", d.URL, nil)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		// Fall back to GET request
		return d.getFileInfoViaGET()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return d.getFileInfoViaGET()
	}

	size := resp.ContentLength
	acceptRanges := resp.Header.Get("Accept-Ranges") == "bytes" || resp.Header.Get("Content-Range") != ""

	return size, acceptRanges, nil
}

func (d *Downloader) getFileInfoViaGET() (int64, bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", d.URL, nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Range", "bytes=0-0")

	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()

	contentRange := resp.Header.Get("Content-Range")
	if contentRange != "" {
		// Parse Content-Range: bytes 0-0/123456
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil && total > 0 {
				return total, true, nil
			}
		}
	}

	// Fall back to downloading with no size info
	return 0, false, nil
}

func (d *Downloader) extractFilename() string {
	// Extract from URL path
	parts := strings.Split(d.URL, "/")
	filename := parts[len(parts)-1]
	if filename == "" || strings.Contains(filename, "?") {
		filename = "download"
	}
	// Clean query params
	if idx := strings.Index(filename, "?"); idx != -1 {
		filename = filename[:idx]
	}
	return filename
}

func (d *Downloader) downloadParallel(outputFile string) error {
	// Calculate chunks
	chunks := d.calculateChunks()
	numChunks := len(chunks)

	// Create temp files for each chunk
	tempFiles := make([]string, numChunks)
	for i := range tempFiles {
		tempFiles[i] = fmt.Sprintf("%s.part%d", outputFile, i)
	}

	// Download chunks concurrently
	var wg sync.WaitGroup
	chunkChan := make(chan chunk, numChunks)
	errChan := make(chan error, numChunks)

	// Start workers
	for i := 0; i < d.Workers; i++ {
		wg.Add(1)
		go d.worker(chunkChan, errChan, &wg)
	}

	// Send chunks
	go func() {
		for _, c := range chunks {
			chunkChan <- c
		}
		close(chunkChan)
	}()

	// Progress ticker
	go d.progressTicker()

	// Wait for completion
	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// Merge chunks
	return d.mergeChunks(outputFile, tempFiles)
}

func (d *Downloader) calculateChunks() []chunk {
	chunkSize := d.TotalSize / int64(d.Workers)
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
		if d.TotalSize/chunkSize < 1 {
			chunkSize = d.TotalSize
		}
	}

	var chunks []chunk
	var start int64 = 0
	idx := 0

	for start < d.TotalSize {
		end := start + chunkSize - 1
		if end >= d.TotalSize-1 || idx == d.Workers-1 {
			end = d.TotalSize - 1
		}
		chunks = append(chunks, chunk{start: start, end: end, index: idx})
		start = end + 1
		idx++
		if start >= d.TotalSize {
			break
		}
	}

	return chunks
}

func (d *Downloader) worker(chunkChan <-chan chunk, errChan chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()

	client := &http.Client{Timeout: 30 * time.Second}

	for c := range chunkChan {
		tempFile := fmt.Sprintf("%s.part%d", d.getOutputBase(), c.index)
		if err := d.downloadChunk(client, c, tempFile); err != nil {
			errChan <- fmt.Errorf("chunk %d failed: %w", c.index, err)
			return
		}
	}
}

func (d *Downloader) getOutputBase() string {
	if idx := strings.LastIndex(d.OutputFile, "."); idx != -1 {
		return d.OutputFile[:idx]
	}
	return d.OutputFile
}

func (d *Downloader) downloadChunk(client *http.Client, c chunk, tempFile string) error {
	req, _ := http.NewRequest("GET", d.URL, nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", c.start, c.end))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.Downloaded += n
	d.mu.Unlock()

	return nil
}

func (d *Downloader) mergeChunks(outputFile string, tempFiles []string) error {
	out, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer out.Close()

	for _, tf := range tempFiles {
		data, err := os.ReadFile(tf)
		if err != nil {
			return err
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
		os.Remove(tf)
	}

	return nil
}

func (d *Downloader) downloadSingle(outputFile string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("GET", d.URL, nil)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer out.Close()

	// Progress ticker
	go d.progressTicker()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.Downloaded = n
	d.mu.Unlock()

	return nil
}

func (d *Downloader) progressTicker() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		d.mu.Lock()
		downloaded := d.Downloaded
		total := d.TotalSize
		d.mu.Unlock()

		if total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			speed := float64(downloaded) / time.Since(d.StartTime).Seconds()
			fmt.Printf("\rProgress: [%-50s] %6.2f%%  %s/s  ",
				strings.Repeat("█", int(pct/2)),
				pct,
				formatSize(int64(speed)))
		} else if downloaded > 0 {
			speed := float64(downloaded) / time.Since(d.StartTime).Seconds()
			fmt.Printf("\rDownloaded: %s  %s/s  ",
				formatSize(downloaded),
				formatSize(int64(speed)))
		}

		if downloaded >= total && total > 0 {
			fmt.Println()
			return
		}
	}
}

func (d *Downloader) verifyFile(outputFile string) error {
	info, err := os.Stat(outputFile)
	if err != nil {
		return fmt.Errorf("failed to stat output file: %w", err)
	}

	if d.TotalSize > 0 && info.Size() != d.TotalSize {
		return fmt.Errorf("file size mismatch: expected %d, got %d", d.TotalSize, info.Size())
	}

	return nil
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
