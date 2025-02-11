package hfclient

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type DownloadManager struct {
	client         *Client
	numConnections int
	skipVerify     bool
}

type downloadProgress struct {
	bytesRead int64
	err       error
	startTime time.Time // Added for speed calculation
}

type progressMonitor struct {
	file     *File
	progress chan downloadProgress
	done     chan struct{}
}

func NewDownloadManager(client *Client, numConnections int, skipVerify bool) *DownloadManager {
	return &DownloadManager{
		client:         client,
		numConnections: numConnections,
		skipVerify:     skipVerify,
	}
}

// formatSpeed formats bytes/second into human readable format
func formatSpeed(bytesPerSec float64) string {
	return formatSize(int64(bytesPerSec)) + "/s"
}

func (dm *DownloadManager) Download(tasks []DownloadTask) error {
	var errs []error
	for _, task := range tasks {
		if err := dm.downloadFile(task); err != nil {
			errs = append(errs, fmt.Errorf("error downloading %s: %w", task.File.Path, err))
		} else {
			fmt.Printf("\nCompleted download of %s\n", task.File.Path)
		}
	}

	if len(errs) > 0 {
		var errStr strings.Builder
		for _, err := range errs {
			errStr.WriteString(err.Error() + "\n")
		}
		return fmt.Errorf("%s", errStr.String())
	}
	return nil
}

func (dm *DownloadManager) downloadFile(task DownloadTask) error {
	// Create destination directory
	destDir := filepath.Dir(task.Destination)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", destDir, err)
	}

	// Check if destination exists
	destInfo, err := os.Stat(task.Destination)
	if err == nil {
		// Destination exists
		if destInfo.IsDir() {
			return fmt.Errorf("destination '%s' is a directory, but expected a file", task.Destination)
		}

		// File exists - verify if it matches
		if ok, err := dm.verifyExistingFile(task); err == nil && ok {
			return nil
		}
	} else if !os.IsNotExist(err) {
		// Some other error occurred
		return fmt.Errorf("failed to check destination: %v", err)
	}

	// Get download URL
	url, err := dm.client.getDownloadURL(task.File)
	if err != nil {
		return err
	}

	// Create temporary file
	tmpFile := task.Destination + ".download"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// Download with progress
	if err := dm.downloadWithProgress(url, f, task.File); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return err
	}

	// Make sure all data is written to disk
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to sync file: %v", err)
	}

	// Close the file before verifying and moving
	f.Close()

	// Verify downloaded file
	if !dm.skipVerify {
		expectedSha := task.File.GetSha()
		if expectedSha != "" {
			actualSha, err := dm.calculateFileSHA(tmpFile)
			if err != nil {
				os.Remove(tmpFile)
				return fmt.Errorf("failed to calculate SHA: %v", err)
			}
			if actualSha != expectedSha {
				os.Remove(tmpFile)
				return fmt.Errorf("SHA mismatch for %s:\nExpected: %s\nActual:   %s",
					task.File.Path, expectedSha, actualSha)
			}
		}
	} else {
		// When skipping SHA verification, verify file size instead
		tmpInfo, err := os.Stat(tmpFile)
		if err != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("failed to get downloaded file size: %v", err)
		}
		if tmpInfo.Size() != task.File.Size {
			os.Remove(tmpFile)
			return fmt.Errorf("size mismatch for %s:\nExpected: %d\nActual:   %d",
				task.File.Path, task.File.Size, tmpInfo.Size())
		}
	}

	// Move temporary file to final destination
	if err := os.Rename(tmpFile, task.Destination); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to move file to destination: %v", err)
	}

	return nil
}

func (dm *DownloadManager) calculateFileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func (dm *DownloadManager) verifyExistingFile(task DownloadTask) (bool, error) {
	// Check if file exists
	destInfo, err := os.Stat(task.Destination)
	if err != nil {
		return false, err
	}

	// If verification is skipped, only check file size
	if dm.skipVerify {
		if destInfo.Size() == task.File.Size {
			fmt.Printf("File %s already exists and size matches\n", task.Destination)
			return true, nil
		}
		return false, nil
	}

	// Otherwise do full SHA verification
	expectedSha := task.File.GetSha()
	if expectedSha == "" {
		return false, nil
	}

	fmt.Printf("Verifying existing file %s... ", task.Destination)

	// Calculate SHA of existing file
	actualSha, err := dm.calculateFileSHA(task.Destination)
	if err != nil {
		fmt.Println("failed!")
		return false, err
	}

	// Compare SHAs
	if actualSha != expectedSha {
		fmt.Println("mismatch!")
		fmt.Printf("SHA mismatch for %s, will redownload\nExpected: %s\nActual:   %s\n",
			task.File.Path, expectedSha, actualSha)
		return false, nil
	}

	fmt.Println("ok!")
	return true, nil
}

func newProgressMonitor(file *File) *progressMonitor {
	return &progressMonitor{
		file:     file,
		progress: make(chan downloadProgress),
		done:     make(chan struct{}),
	}
}

func (pm *progressMonitor) start() {
	go func() {
		var totalRead int64
		startTime := time.Now()
		lastUpdate := startTime
		lastBytes := int64(0)

		for {
			select {
			case <-pm.done:
				fmt.Println() // New line after download completes
				return
			case p := <-pm.progress:
				if p.err != nil {
					continue
				}
				totalRead += p.bytesRead

				// Update progress every 100ms
				now := time.Now()
				if now.Sub(lastUpdate) >= 100*time.Millisecond {
					percent := float64(totalRead) * 100 / float64(pm.file.Size)
					speed := float64(totalRead-lastBytes) / now.Sub(lastUpdate).Seconds()

					// Create progress bar
					width := 40
					completed := int(width * int(percent) / 100)
					bar := strings.Repeat("=", completed) + strings.Repeat(" ", width-completed)

					// Calculate ETA
					if speed > 0 {
						remaining := float64(pm.file.Size-totalRead) / speed
						fmt.Printf("\r%s [%s] %.1f%% | %s / %s | %s | ETA: %.0fs",
							pm.file.Path,
							bar,
							percent,
							formatSize(totalRead),
							formatSize(pm.file.Size),
							formatSpeed(speed),
							remaining,
						)
					} else {
						fmt.Printf("\r%s [%s] %.1f%% | %s / %s",
							pm.file.Path,
							bar,
							percent,
							formatSize(totalRead),
							formatSize(pm.file.Size),
						)
					}

					lastUpdate = now
					lastBytes = totalRead
				}
			}
		}
	}()
}

func (pm *progressMonitor) stop() {
	close(pm.done)
}

func (dm *DownloadManager) downloadWithProgress(url string, f *os.File, file *File) error {
	// Get file size and calculate chunk sizes for concurrent downloads
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return err
	}

	if dm.client.token != "" {
		req.Header.Set("Authorization", "Bearer "+dm.client.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get file size: status %d", resp.StatusCode)
	}

	fileSize := file.Size
	if fileSize == 0 {
		return fmt.Errorf("invalid file size")
	}

	// Calculate chunk size and ranges
	chunkSize := fileSize / int64(dm.numConnections)
	if chunkSize < 1024*1024 { // If chunks are smaller than 1MB, don't use concurrent connections
		return dm.downloadSingleConnection(url, f, file)
	}

	// Pre-allocate the file to prevent fragmentation
	if err := f.Truncate(fileSize); err != nil {
		return fmt.Errorf("failed to allocate file: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, dm.numConnections)

	// Start progress monitoring
	monitor := newProgressMonitor(file)
	monitor.start()
	defer monitor.stop()

	// Create a mutex for file writes
	var writeMu sync.Mutex

	// Download chunks concurrently
	for i := 0; i < dm.numConnections; i++ {
		wg.Add(1)
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == dm.numConnections-1 {
			end = fileSize - 1 // Last chunk gets the remainder
		}

		go func(start, end int64) {
			defer wg.Done()

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				errs <- err
				return
			}

			if dm.client.token != "" {
				req.Header.Set("Authorization", "Bearer "+dm.client.token)
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusPartialContent {
				errs <- fmt.Errorf("chunk download failed with status %d", resp.StatusCode)
				return
			}

			buffer := make([]byte, 32*1024)
			for {
				n, err := resp.Body.Read(buffer)
				if n > 0 {
					writeMu.Lock()
					_, werr := f.WriteAt(buffer[:n], start)
					writeMu.Unlock()
					if werr != nil {
						errs <- werr
						return
					}
					start += int64(n)
					monitor.progress <- downloadProgress{bytesRead: int64(n)}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}(start, end)
	}

	// Wait for all chunks to complete
	wg.Wait()
	close(errs)

	// Check for any errors
	for err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

func (dm *DownloadManager) downloadSingleConnection(url string, f *os.File, file *File) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if dm.client.token != "" {
		req.Header.Set("Authorization", "Bearer "+dm.client.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Start progress monitoring
	monitor := newProgressMonitor(file)
	monitor.start()
	defer monitor.stop()

	// Download with progress updates
	buffer := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			_, werr := f.Write(buffer[:n])
			if werr != nil {
				monitor.progress <- downloadProgress{err: werr}
				return werr
			}
			monitor.progress <- downloadProgress{
				bytesRead: int64(n),
				startTime: time.Now(),
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			monitor.progress <- downloadProgress{err: err}
			return err
		}
	}

	return nil
}
