package hfdownloadernested

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

const (
	RawFileURL      = "https://huggingface.co/%s/raw/%s/%s"
	LfsResolverURL  = "https://huggingface.co/%s/resolve/%s/%s"
	JsonFileTreeURL = "https://huggingface.co/api/models/%s/tree/%s/%s"
)

//I may use this coloring thing later on
var (
	infoColor      = color.New(color.FgGreen).SprintFunc()
	warningColor   = color.New(color.FgYellow).SprintFunc()
	errorColor     = color.New(color.FgRed).SprintFunc()
	NumConnections = 5
)

type hfmodel struct {
	Type        string `json:"type"`
	Oid         string `json:"oid"`
	Size        int    `json:"size"`
	Path        string `json:"path"`
	IsDirectory bool
	IsLFS       bool

	AppendedPath    string
	SkipDownloading bool
	DownloadLink    string
	Lfs             *hflfs `json:"lfs,omitempty"`
}

type hflfs struct {
	Oid_SHA265  string `json:"oid"` // in lfs, oid is sha256 of the file
	Size        int64  `json:"size"`
	PointerSize int    `json:"pointerSize"`
}

func DownloadModel(ModelName string, DestintionBasePath string, ModelBranch string, concurrentConnctionions int) error {
	NumConnections = concurrentConnctionions
	modelPath := path.Join(DestintionBasePath, strings.Replace(ModelName, "/", "_", -1))
	//Check StoragePath
	err := os.MkdirAll(modelPath, os.ModePerm)
	if err != nil {
		// fmt.Println("Error:", err)
		return err
	}
	//get root path files and folders
	err = processHFFolderTree(DestintionBasePath, ModelName, ModelBranch, "") // passing empty as foldername, because its the first root folder
	if err != nil {
		// fmt.Println("Error:", err)
		return err
	}
	return nil
}
func processHFFolderTree(DestintionBasePath string, ModelName string, ModelBranch string, fodlerName string) error {
	modelPath := path.Join(DestintionBasePath, strings.Replace(ModelName, "/", "_", -1))
	tempFolder := path.Join(modelPath, fodlerName, "tmp")
	if _, err := os.Stat(tempFolder); err == nil { //clear it if it exists before for any reason
		err = os.RemoveAll(tempFolder)
		if err != nil {
			return err
		}
	}
	err := os.MkdirAll(tempFolder, os.ModePerm)
	if err != nil {
		// fmt.Println("Error:", err)
		return err
	}
	defer os.RemoveAll(tempFolder) //delete tmp folder upon returning from this function
	branch := ModelBranch
	JsonFileListURL := fmt.Sprintf(JsonFileTreeURL, ModelName, branch, fodlerName)
	fmt.Printf("\nGetting File Download Files List Tree from: %s", JsonFileListURL)
	response, err := http.Get(JsonFileListURL)
	if err != nil {
		// fmt.Println("Error:", err)
		return err
	}
	defer response.Body.Close()

	// Read the response body into a byte slice
	content, err := ioutil.ReadAll(response.Body)
	if err != nil {
		// fmt.Println("Error:", err)
		return err
	}
	// fmt.Println(string(content))
	jsonFilesList := []hfmodel{}
	err = json.Unmarshal(content, &jsonFilesList)
	if err != nil {
		return err
	}
	for i := range jsonFilesList {
		jsonFilesList[i].AppendedPath = path.Join(modelPath, jsonFilesList[i].Path)
		if jsonFilesList[i].Type == "directory" {
			jsonFilesList[i].IsDirectory = true
			err := os.MkdirAll(path.Join(modelPath, jsonFilesList[i].Path), os.ModePerm)
			if err != nil {
				return err
			}
			jsonFilesList[i].SkipDownloading = true
			//now if this a folder, this whole function will be called again recursivley
			processHFFolderTree(DestintionBasePath, ModelName, ModelBranch, jsonFilesList[i].Path) //recursive call
			continue
		}
		jsonFilesList[i].DownloadLink = fmt.Sprintf(RawFileURL, ModelName, branch, jsonFilesList[i].Path)
		if jsonFilesList[i].Lfs != nil {
			jsonFilesList[i].IsLFS = true
			resolverURL := fmt.Sprintf(LfsResolverURL, ModelName, branch, jsonFilesList[i].Path)
			getLink, err := getRedirectLink(resolverURL)
			if err != nil {
				return err
			}
			jsonFilesList[i].DownloadLink = getLink
		}
	}
	// UNCOMMENT BELOW TWO LINES TO DEBUG THIS FOLDER JSON STRUCTURE
	// s, _ := json.MarshalIndent(jsonFilesList, "", "  ")
	// fmt.Println(string(s))
	//2nd loop through the files, checking exists/non-exists
	for i := range jsonFilesList {
		//check if the file exists before
		// Check if the file exists
		if jsonFilesList[i].IsDirectory {
			continue
		}
		filename := jsonFilesList[i].AppendedPath
		if _, err := os.Stat(filename); err == nil {
			// File exists, get its size
			fileInfo, _ := os.Stat(filename)
			size := fileInfo.Size()
			fmt.Printf("\nChecking Existsing file: %s", jsonFilesList[i].AppendedPath)
			//  for non-lfs files, I can only compare size, I don't there is a sha256 hash for them
			if size == int64(jsonFilesList[i].Size) {
				jsonFilesList[i].SkipDownloading = true
				if jsonFilesList[i].IsLFS {
					err := verifyChecksum(jsonFilesList[i].AppendedPath, jsonFilesList[i].Lfs.Oid_SHA265)
					if err != nil {
						err := os.Remove(jsonFilesList[i].AppendedPath)
						if err != nil {
							return err
						}
						jsonFilesList[i].SkipDownloading = false
					}
					fmt.Printf("\nHash Matched for LFS file: %s", jsonFilesList[i].AppendedPath)
				} else {
					fmt.Printf("\nfile size matched for non LFS file: %s", jsonFilesList[i].AppendedPath)
				}
			}

		}

	}
	//3ed loop through the files, downloading missing/failed files
	for i := range jsonFilesList {
		if jsonFilesList[i].IsDirectory {
			continue
		}
		if jsonFilesList[i].SkipDownloading {
			fmt.Printf("\nSkipping: %s", jsonFilesList[i].AppendedPath)
			continue
		}
		// fmt.Printf("Downloading: %s\n", jsonFilesList[i].Path)
		if jsonFilesList[i].IsLFS {
			err := downloadFileMultiThread(tempFolder, jsonFilesList[i].DownloadLink, jsonFilesList[i].AppendedPath)
			if err != nil {
				return err
			}
			//lfs file, verify by checksum
			fmt.Printf("\nChecking SHA256 Hash for LFS file: %s", jsonFilesList[i].AppendedPath)
			err = verifyChecksum(jsonFilesList[i].AppendedPath, jsonFilesList[i].Lfs.Oid_SHA265)
			if err != nil {
				err := os.Remove(jsonFilesList[i].AppendedPath)
				if err != nil {
					return err
				}
				//jsonFilesList[i].SkipDownloading = false
			}
			fmt.Printf("\nHash Matched for LFS file: %s\n", jsonFilesList[i].AppendedPath)

		} else {
			err = downloadFileMultiThread(tempFolder, jsonFilesList[i].DownloadLink, jsonFilesList[i].AppendedPath) //no checksum available for small non-lfs files
			if err != nil {
				return err
			}
			//non-lfs file, verify by size matching
			fmt.Printf("\nChecking file size matching: %s", jsonFilesList[i].AppendedPath)
			if _, err := os.Stat(jsonFilesList[i].AppendedPath); err == nil {
				fileInfo, _ := os.Stat(jsonFilesList[i].AppendedPath)
				size := fileInfo.Size()
				if size != int64(jsonFilesList[i].Size) {
					return fmt.Errorf("\nFile size mismatch: %s, filesize: %d, Needed Size: %d", jsonFilesList[i].AppendedPath, size, jsonFilesList[i].Size)
				}
			} else {
				return fmt.Errorf("\nFile does not exist: %s", jsonFilesList[i].AppendedPath)
			}
		}
	}

	return nil
}

// ***********************************************   All the functions below generated by ChatGPT 3.5, and ChatGPT 4 , with some modifications ***********************************************
func IsValidModelName(modelName string) bool {
	pattern := `^[A-Za-z0-9_\-]+/[A-Za-z0-9\._\-]+$`
	match, _ := regexp.MatchString(pattern, modelName)
	return match
}

func getRedirectLink(url string) (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		redirectURL := resp.Header.Get("Location")
		return redirectURL, nil
	}

	return "", fmt.Errorf("No redirect found")
}

func verifyChecksum(fileName string, expectedChecksum string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	sum := hasher.Sum(nil)
	if hex.EncodeToString(sum) != expectedChecksum {
		return fmt.Errorf("checksums do not match")
	}

	return nil
}

func downloadChunk(tempFolder string, outputFileName string, idx int, url string, start, end int64, wg *sync.WaitGroup, progress chan<- int64) error {
	defer wg.Done()

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	rangeHeader := fmt.Sprintf("bytes=%d-%d", start, end-1)
	req.Header.Add("Range", rangeHeader)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmpFileName := fmt.Sprintf("%s_%d_*.tmp", outputFileName, idx)
	tempFile, err := ioutil.TempFile(tempFolder, tmpFileName)
	if err != nil {
		return err
	}
	defer tempFile.Close()

	buffer := make([]byte, 1024)
	for {
		bytesRead, err := resp.Body.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}

		if bytesRead == 0 {
			break
		}

		_, err = tempFile.Write(buffer[:bytesRead])
		if err != nil {
			return err
		}
		progress <- int64(bytesRead)
	}

	return nil
}

func mergeFiles(tempFodler, outputFileName string, numChunks int) error {
	outputFile, err := os.Create(outputFileName)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	for i := 0; i < numChunks; i++ {
		tmpFileName := fmt.Sprintf("%s_%d_*.tmp", path.Base(outputFileName), i)
		tempFileName := path.Join(tempFodler, tmpFileName)
		tempFiles, err := ioutil.ReadDir(tempFodler)
		if err != nil {
			return err
		}
		for _, file := range tempFiles {

			if matched, _ := filepath.Match(tempFileName, path.Join(tempFodler, file.Name())); matched {
				tempFile, err := os.Open(path.Join(tempFodler, file.Name()))
				if err != nil {
					return err
				}
				_, err = io.Copy(outputFile, tempFile)
				if err != nil {
					return err
				}
				err = tempFile.Close()
				if err != nil {
					return err
				}
				err = os.Remove(path.Join(tempFodler, file.Name()))
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func downloadFileMultiThread(tempFolder, url, outputFileName string) error {
	resp, err := http.Head(url)
	if err != nil {
		return err
	}

	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		return err
	}

	chunkSize := int64(contentLength / NumConnections)

	progress := make(chan int64, NumConnections)
	wg := &sync.WaitGroup{}
	wg.Add(NumConnections)

	for i := 0; i < NumConnections; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize

		if i == NumConnections-1 {
			end = int64(contentLength)
		}
		go func(i int, start, end int64) {
			err := downloadChunk(tempFolder, path.Base(outputFileName), i, url, start, end, wg, progress)
			if err != nil {
				fmt.Printf("\nError downloading chunk %d: %v\n", i, err)
			}
		}(i, start, end)
	}
	// Mark the start time of the download
	startTime := time.Now()
	go func() {
		var totalDownloaded int64

		// Calculate speed in megabytes per second
		for chunkSize := range progress {
			totalDownloaded += chunkSize
			elapsed := time.Since(startTime).Seconds()
			speed := float64(totalDownloaded) / 1024 / 1024 / elapsed
			fmt.Printf("\rDownloading %s Speed: %.2f MB/sec, %.2f%% ", outputFileName, speed, float64(totalDownloaded*100)/float64(contentLength))
		}
	}()

	wg.Wait()
	close(progress)

	// fmt.Print("\nDownload completed")
	fmt.Printf("\nMerging %s Chunks", outputFileName)
	err = mergeFiles(tempFolder, outputFileName, NumConnections)
	if err != nil {
		return err
	}

	return nil
}
func downloadSingleThreaded(url, outputFileName string) error {
	outputFile, err := os.Create(outputFileName)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(outputFile, resp.Body)
	if err != nil {
		return err
	}

	// fmt.Println("\nDownload completed")
	return nil
}
