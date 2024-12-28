package hfdownloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	AgreementModelURL      = "https://huggingface.co/%s"
	AgreementDatasetURL    = "https://huggingface.co/datasets/%s"
	RawModelFileURL        = "https://huggingface.co/%s/raw/%s/%s"
	RawDatasetFileURL      = "https://huggingface.co/datasets/%s/raw/%s/%s"
	LfsModelResolverURL    = "https://huggingface.co/%s/resolve/%s/%s"
	LfsDatasetResolverURL  = "https://huggingface.co/datasets/%s/resolve/%s/%s"
	JsonModelsFileTreeURL  = "https://huggingface.co/api/models/%s/tree/%s/%s"
	JsonDatasetFileTreeURL = "https://huggingface.co/api/datasets/%s/tree/%s/%s"
)

var (
	infoColor      = color.New(color.FgGreen).SprintFunc()
	successColor   = color.New(color.FgHiGreen).SprintFunc()
	warningColor   = color.New(color.FgYellow).SprintFunc()
	errorColor     = color.New(color.FgRed).SprintFunc()
	NumConnections = 5
	RequiresAuth   = false
	AuthToken      = ""
)

type hfmodel struct {
	Type          string `json:"type"`
	Oid           string `json:"oid"`
	Size          int    `json:"size"`
	Path          string `json:"path"`
	LocalSize     int64
	NeedsDownload bool
	IsDirectory   bool
	IsLFS         bool

	AppendedPath    string
	SkipDownloading bool
	FilterSkip      bool
	DownloadLink    string
	Lfs             *hflfs `json:"lfs,omitempty"`
}

type hflfs struct {
	Oid_SHA265  string `json:"oid"` // in lfs, oid is sha256 of the file
	Size        int64  `json:"size"`
	PointerSize int    `json:"pointerSize"`
}

// Add this new struct to track download progress
type downloadProgress struct {
	chunkIndex int
	bytesRead  int64
}

func DownloadModel(ModelDatasetName string, AppendFilterToPath bool, SkipSHA bool, IsDataset bool, DestinationBasePath string, ModelBranch string, concurrentConnections int, token string, silentMode bool) error {
	NumConnections = concurrentConnections

	// make sure we dont include dataset filter within folder creation
	modelP := ModelDatasetName
	HasFilter := false
	if strings.Contains(modelP, ":") {
		modelP = strings.Split(ModelDatasetName, ":")[0]
		HasFilter = true
	}
	modelPath := path.Join(DestinationBasePath, strings.Replace(modelP, "/", "_", -1))
	if token != "" {
		RequiresAuth = true
		AuthToken = token
	}

	if HasFilter && AppendFilterToPath { // for this feature, I'll just simple re-run the script and apply one filter at a time
		filters := strings.Split(strings.Split(ModelDatasetName, ":")[1], ",")
		for _, ff := range filters {
			// create folders

			ffpath := fmt.Sprintf("%s_f_%s", modelPath, ff)
			err := os.MkdirAll(ffpath, os.ModePerm)
			if err != nil {
				if !silentMode {
					fmt.Println(errorColor("Error:"), err)
				}
				return err
			}
			newModelDatasetName := fmt.Sprintf("%s:%s", modelP, ff)
			err = processHFFolderTree(ffpath, IsDataset, SkipSHA, newModelDatasetName, ModelBranch, "", silentMode) // passing empty as foldername, because its the first root folder
			if err != nil {
				if !silentMode {
					fmt.Println(errorColor("Error:"), err)
				}
				return err
			}
		}
	} else {
		err := os.MkdirAll(modelPath, os.ModePerm)
		if err != nil {
			if !silentMode {
				fmt.Println(errorColor("Error:"), err)
			}
			return err
		}
		// ok we need to add some logic here now to analyze the model/dataset before we go into downloading

		// get root path files and folders
		err = processHFFolderTree(modelPath, IsDataset, SkipSHA, ModelDatasetName, ModelBranch, "", silentMode) // passing empty as foldername, because its the first root folder
		if err != nil {
			if !silentMode {
				fmt.Println(errorColor("Error:"), err)
			}
			return err
		}
	}

	return nil
}
func processHFFolderTree(ModelPath string, IsDataset bool, SkipSHA bool, ModelDatasetName string, Branch string, folderName string, silentMode bool) error {
	JsonTreeVariable := JsonModelsFileTreeURL // we assume its Model first
	RawFileURL := RawModelFileURL
	LfsResolverURL := LfsModelResolverURL
	AgreementURL := fmt.Sprintf(AgreementModelURL, ModelDatasetName)
	HasFilter := false
	var FilterBinFileString []string
	originalDataSetName := ModelDatasetName // fix a bug where filters will be skipped when we call the function recursiley
	if strings.Contains(ModelDatasetName, ":") && !IsDataset {
		HasFilter = true
		// remove the filtered content from Model Name
		f := strings.Split(ModelDatasetName, ":")
		ModelDatasetName = f[0]
		FilterBinFileString = strings.Split(strings.ToLower(f[1]), ",")
		if !silentMode {
			fmt.Printf("\n%s", infoColor("Filter Has been applied, will include LFS Model Files that contains: ", FilterBinFileString))
		}
	}
	if IsDataset {
		JsonTreeVariable = JsonDatasetFileTreeURL // set this to true if it its set to Dataset
		RawFileURL = RawDatasetFileURL
		LfsResolverURL = LfsDatasetResolverURL
		AgreementURL = fmt.Sprintf(AgreementDatasetURL, ModelDatasetName)
	}

	tempFolder := path.Join(ModelPath, folderName, "tmp")
	// updated ver: 1.2.5; I cannot clear it if I'm trying to implement resume broken downloads based on a single file
	// if _, err := os.Stat(tempFolder); err == nil { //clear it if it exists before for any reason
	// 	err = os.RemoveAll(tempFolder)
	// 	if err != nil {
	// 		return err
	// 	}
	// }
	err := os.MkdirAll(tempFolder, os.ModePerm)
	if err != nil {
		if !silentMode {
			fmt.Println(errorColor("Error:", err))
		}
		return err
	}
	// updated ver: 1.2.5; I cannot clear it if I'm trying to implement resume broken downloads based on a single file
	// defer os.RemoveAll(tempFolder) //delete tmp folder upon returning from this function
	branch := Branch
	JsonFileListURL := fmt.Sprintf(JsonTreeVariable, ModelDatasetName, branch, folderName)
	jsonFilesList := []hfmodel{}
	for _, file := range jsonFilesList {
		filePath := path.Join(ModelPath, file.Path)
		if file.IsDirectory {
			// Directory handling remains unchanged
			if err := os.MkdirAll(filePath, os.ModePerm); err != nil {
				return err
			}
			//here we should pass the original name with filters, other wise the filter will be applied

			if err := processHFFolderTree(ModelPath, IsDataset, SkipSHA, originalDataSetName, Branch, file.Path, silentMode); err != nil {
				return err
			}
		} else {
			// Use NeedsDownload flag to determine if the file should be downloaded
			if file.NeedsDownload {
				if file.IsLFS || needsDownload(filePath, file.Size) {
					tempFolder := filepath.Join(ModelPath, "tmp")
					downloadErr := downloadFileMultiThread(tempFolder, file.DownloadLink, filePath, silentMode)
					if downloadErr != nil {
						if !silentMode {
							fmt.Printf("\n%s", errorColor("Error downloading file with multi-threading: ", downloadErr))
						}
						return downloadErr
					}
				} else {
					// For smaller files or if not using multi-threading, a single-threaded download can be used
					downloadErr := downloadSingleThreaded(file.DownloadLink, filePath)
					if downloadErr != nil {
						if !silentMode {
							fmt.Printf("\n%s", errorColor("Error downloading file with single-threading: ", downloadErr))
						}
						return downloadErr
					}
				}
			}
		}
	}
	if !silentMode {
		fmt.Printf("\n%s", infoColor("Getting File Download Files List Tree from: ", JsonFileListURL))
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", JsonFileListURL, nil)
	if err != nil {
		return err
	}
	if RequiresAuth {
		// Set the authorization header with the Bearer token
		bearerToken := AuthToken
		req.Header.Add("Authorization", "Bearer "+bearerToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		if !silentMode {
			fmt.Println(errorColor("Error:"), err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 && !RequiresAuth {
		return fmt.Errorf("\n%s", errorColor("Repo requires access token, generate an access token form huggingface, and pass it using flag: -t TOKEN"))
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("\n%s", errorColor("You need to manually accept the agreement for this model/dataset: ", AgreementURL, " on HuggingFace site, No bypass will be implemented"))
	}
	// Read the response body into a byte slice
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		if !silentMode {
			fmt.Println(errorColor("Error:"), err)
		}
		return err

	}

	err = json.Unmarshal(content, &jsonFilesList)
	if err != nil {
		return err
	}

	// Replace \n with %0A in the jsonFilesList
	for i := range jsonFilesList {
		jsonFilesList[i].Path = strings.ReplaceAll(jsonFilesList[i].Path, "\n", "%0A")
	}

	for i := range jsonFilesList {
		jsonFilesList[i].AppendedPath = path.Join(ModelPath, jsonFilesList[i].Path)
		if jsonFilesList[i].Type == "directory" {
			jsonFilesList[i].IsDirectory = true
			err := os.MkdirAll(path.Join(ModelPath, jsonFilesList[i].Path), os.ModePerm)
			if err != nil {
				return err
			}
			jsonFilesList[i].SkipDownloading = true
			// now if this a folder, this whole function will be called again recursively
			//here we should pass the original name with filters, other wise the filter will be applied

			err = processHFFolderTree(ModelPath, IsDataset, SkipSHA, originalDataSetName, Branch, jsonFilesList[i].Path, silentMode) // recursive call
			if err != nil {
				return err
			}
			continue
		}

		jsonFilesList[i].DownloadLink = fmt.Sprintf(RawFileURL, ModelDatasetName, branch, jsonFilesList[i].Path)
		if jsonFilesList[i].Lfs != nil {
			jsonFilesList[i].IsLFS = true
			resolverURL := fmt.Sprintf(LfsResolverURL, ModelDatasetName, branch, jsonFilesList[i].Path)
			getLink, err := getRedirectLink(resolverURL)
			if err != nil {
				return err
			}
			// Check for filter
			if HasFilter {
				filenameLowerCase := strings.ToLower(jsonFilesList[i].Path)
				if strings.HasSuffix(filenameLowerCase, ".act") || strings.HasSuffix(filenameLowerCase, ".bin") ||
					strings.Contains(filenameLowerCase, ".gguf") || // either *.gguf or *.gguf-split-{a, b, ...}
					strings.HasSuffix(filenameLowerCase, ".safetensors") || strings.HasSuffix(filenameLowerCase, ".pt") || strings.HasSuffix(filenameLowerCase, ".meta") ||
					strings.HasSuffix(filenameLowerCase, ".zip") || strings.HasSuffix(filenameLowerCase, ".z01") || strings.HasSuffix(filenameLowerCase, ".onnx") || strings.HasSuffix(filenameLowerCase, ".data") ||
					strings.HasSuffix(filenameLowerCase, ".onnx_data") ||
					strings.HasSuffix(filenameLowerCase, ".llamafile") {
					jsonFilesList[i].FilterSkip = true // we assume its skipped, unless below condition range match
					for _, ff := range FilterBinFileString {
						if strings.Contains(filenameLowerCase, ff) {
							jsonFilesList[i].FilterSkip = false
						}
					}

				}
			}
			jsonFilesList[i].DownloadLink = getLink
		}
	}
	// UNCOMMENT BELOW TWO LINES TO DEBUG THIS FOLDER JSON STRUCTURE
	// s, _ := json.MarshalIndent(jsonFilesList, "", "  ")
	// fmt.Println(string(s))
	// 2nd loop through the files, checking exists/non-exists
	for i := range jsonFilesList {
		// check if the file exists before
		// Check if the file exists
		if jsonFilesList[i].IsDirectory {
			continue
		}
		if jsonFilesList[i].FilterSkip {
			continue
		}
		filename := jsonFilesList[i].AppendedPath
		if _, err := os.Stat(filename); err == nil {
			// File exists, get its size
			fileInfo, _ := os.Stat(filename)
			size := fileInfo.Size()
			if !silentMode {
				fmt.Printf("\n%s", infoColor("Checking Existing file: ", jsonFilesList[i].AppendedPath))
			}
			//  for non-lfs files, I can only compare size, I don't there is a sha256 hash for them
			if size == int64(jsonFilesList[i].Size) {
				jsonFilesList[i].SkipDownloading = true
				if jsonFilesList[i].IsLFS {
					if !SkipSHA {
						err := verifyChecksum(jsonFilesList[i].AppendedPath, jsonFilesList[i].Lfs.Oid_SHA265)
						if err != nil {
							err := os.Remove(jsonFilesList[i].AppendedPath)
							if err != nil {
								return err
							}
							jsonFilesList[i].SkipDownloading = false
							if !silentMode {
								fmt.Printf("\n%s", warningColor("Hash failed for LFS file: ", jsonFilesList[i].AppendedPath, ", will redownload/resume"))
							}
							return err
						}
						if !silentMode {
							fmt.Printf("\n%s", successColor("Hash Matched for LFS file: ", jsonFilesList[i].AppendedPath))
						}
					} else {
						if !silentMode {
							fmt.Printf("\n%s", infoColor("Hash Matching SKIPPED for LFS file: ", jsonFilesList[i].AppendedPath))
						}
					}

				} else {
					if !silentMode {
						fmt.Printf("\n%s", successColor("file size matched for non LFS file: ", jsonFilesList[i].AppendedPath))
					}
				}
			}

		}

	}
	// 3ed loop through the files, downloading missing/failed files
	for i := range jsonFilesList {
		if jsonFilesList[i].IsDirectory {
			continue
		}
		if jsonFilesList[i].SkipDownloading {
			if !silentMode {
				fmt.Printf("\n%s", infoColor("Skipping: ", jsonFilesList[i].AppendedPath))
			}
			continue
		}
		if jsonFilesList[i].FilterSkip {
			if !silentMode {
				fmt.Printf("\n%s", infoColor("Filter Skipping: ", jsonFilesList[i].AppendedPath))
			}
			continue
		}
		// fmt.Printf("Downloading: %s\n", jsonFilesList[i].Path)
		if jsonFilesList[i].IsLFS {
			err := downloadFileMultiThread(tempFolder, jsonFilesList[i].DownloadLink, jsonFilesList[i].AppendedPath, silentMode)
			if err != nil {
				return err
			}
			// lfs file, verify by checksum
			if !silentMode {
				fmt.Printf("\n%s", infoColor("Checking SHA256 Hash for LFS file: ", jsonFilesList[i].AppendedPath))
			}
			if !SkipSHA {
				err = verifyChecksum(jsonFilesList[i].AppendedPath, jsonFilesList[i].Lfs.Oid_SHA265)
				if err != nil {
					err := os.Remove(jsonFilesList[i].AppendedPath)
					if err != nil {
						return err
					}
					// jsonFilesList[i].SkipDownloading = false
					if !silentMode {
						fmt.Printf("\n%s", errorColor("Hash failed for LFS file: ", jsonFilesList[i].AppendedPath, "will redownload/resume"))
					}
					return err
				}
				if !silentMode {
					fmt.Printf("\n%s", successColor("Hash Matched for LFS file: ", jsonFilesList[i].AppendedPath))
				}

			} else {
				if !silentMode {
					fmt.Printf("\n%s", warningColor("Hash Matching SKIPPED for LFS file: ", jsonFilesList[i].AppendedPath))
				}
			}

		} else {
			// err := downloadFileMultiThread(tempFolder, jsonFilesList[i].DownloadLink, jsonFilesList[i].AppendedPath) //maybe later I'll enable multithreading for all files, even non-lfs
			err = downloadSingleThreaded(jsonFilesList[i].DownloadLink, jsonFilesList[i].AppendedPath) // no checksum available for small non-lfs files
			if err != nil {
				return err
			}
			// non-lfs file, verify by size matching
			if !silentMode {
				fmt.Printf("\nChecking file size matching: %s", jsonFilesList[i].AppendedPath)
			}
			if _, err := os.Stat(jsonFilesList[i].AppendedPath); err == nil {
				fileInfo, _ := os.Stat(jsonFilesList[i].AppendedPath)
				size := fileInfo.Size()
				if size != int64(jsonFilesList[i].Size) {
					return fmt.Errorf("\n%s", errorColor("File size mismatch: ", jsonFilesList[i].AppendedPath, ", filesize: ", size, "Needed Size: ", jsonFilesList[i].Size))
				}
			} else {
				return fmt.Errorf("\n%s", errorColor("File does not exist: ", jsonFilesList[i].AppendedPath))
			}
		}
	}
	os.RemoveAll(tempFolder) // by here its safe to delete the temp folder
	return nil
}

func fetchFileList(JsonFileListURL string) ([]hfmodel, error) {
	var filesList []hfmodel

	client := &http.Client{}
	req, err := http.NewRequest("GET", JsonFileListURL, nil)
	if err != nil {
		return nil, err
	}
	if RequiresAuth {
		req.Header.Add("Authorization", "Bearer "+AuthToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&filesList); err != nil {
		return nil, err
	}

	return filesList, nil
}

func needsDownload(filePath string, remoteSize int) bool {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return true
	}
	return info.Size() != int64(remoteSize)
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
			if RequiresAuth {
				bearerToken := AuthToken
				req.Header.Add("Authorization", "Bearer "+bearerToken)
			}
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	if RequiresAuth {
		// Set the authorization header with the Bearer token
		bearerToken := AuthToken
		req.Header.Add("Authorization", "Bearer "+bearerToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 && !RequiresAuth {
		return "", fmt.Errorf("\n%s", errorColor("This Repo requires access token, generate an access token form huggingface, and pass it using flag: -t TOKEN"))
	}
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		redirectURL := resp.Header.Get("Location")
		return redirectURL, nil
	}

	return "", fmt.Errorf(errorColor("No redirect found"))
}

func verifyChecksum(filePath, expectedChecksum string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("\n%s", errorColor("checksum mismatch: expected ", expectedChecksum, "got ", actualChecksum))
	}

	return nil
}

// Modify downloadChunk to write directly to the target file
func downloadChunk(file *os.File, idx int, url string, start, end int64, progress chan<- downloadProgress) error {
	client := &http.Client{
		Transport: &http.Transport{},
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if RequiresAuth {
		req.Header.Add("Authorization", "Bearer "+AuthToken)
	}

	rangeHeader := fmt.Sprintf("bytes=%d-%d", start, end-1)
	req.Header.Add("Range", rangeHeader)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 && !RequiresAuth {
		return fmt.Errorf("\n%s", errorColor("This Repo requires an access token, generate an access token form huggingface, and pass it using flag: -t TOKEN"))
	}

	buffer := make([]byte, 32768)
	bytesDownloaded := int64(0)

	for {
		bytesRead, err := resp.Body.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}

		if bytesRead == 0 {
			break
		}

		_, err = file.WriteAt(buffer[:bytesRead], start+bytesDownloaded)
		if err != nil {
			return err
		}

		bytesDownloaded += int64(bytesRead)
		progress <- downloadProgress{
			chunkIndex: idx,
			bytesRead:  int64(bytesRead),
		}
	}

	return nil
}

// Replace downloadFileMultiThread with this improved version
func downloadFileMultiThread(tempFolder, url, outputFileName string, silentMode bool) error {
	client := &http.Client{}
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return err
	}
	if RequiresAuth {
		req.Header.Add("Authorization", "Bearer "+AuthToken)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == 401 && !RequiresAuth {
		return fmt.Errorf("\n%s", errorColor("This Repo requires access token, generate an access token form huggingface, and pass it using flag: -t TOKEN"))
	}
	
	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		return err
	}

	// Create the output file
	outputFile, err := os.OpenFile(outputFileName, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	// Pre-allocate the file size
	err = outputFile.Truncate(int64(contentLength))
	if err != nil {
		return err
	}

	chunkSize := int64(contentLength / NumConnections)
	progress := make(chan downloadProgress, NumConnections)
	wg := &sync.WaitGroup{}
	errChan := make(chan error, NumConnections)

	for i := 0; i < NumConnections; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize
		if i == NumConnections-1 {
			end = int64(contentLength)
		}

		wg.Add(1)
		go func(i int, start, end int64) {
			defer wg.Done()
			if err := downloadChunk(outputFile, i, url, start, end, progress); err != nil {
				errChan <- fmt.Errorf("error downloading chunk %d: %v", i, err)
			}
		}(i, start, end)
	}

	// Monitor progress
	if !silentMode {
		fmt.Printf("\nStart Downloading: %s", outputFileName)
	}
	
	startTime := time.Now()
	go func() {
		totalBytes := make([]int64, NumConnections)
		lastPrintTime := time.Now()

		for prog := range progress {
			now := time.Now()
			totalBytes[prog.chunkIndex] += prog.bytesRead

			totalDownloaded := int64(0)
			for _, bytes := range totalBytes {
				totalDownloaded += bytes
			}

			if !silentMode && (now.Sub(lastPrintTime) >= 100*time.Millisecond) {
				elapsed := time.Since(startTime).Seconds()
				if elapsed > 0 {
					speed := float64(totalDownloaded) / (1024 * 1024) / elapsed
					percent := float64(totalDownloaded*100) / float64(contentLength)
					fmt.Printf("\rDownloading %s Speed: %.2f MB/sec, %.2f%% ", 
						outputFileName, speed, percent)
					lastPrintTime = now
				}
			}
		}
	}()

	// Wait for completion and check for errors
	go func() {
		wg.Wait()
		close(progress)
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			if !silentMode {
				fmt.Println(err)
			}
			return err
		}
	}

	if !silentMode {
		fmt.Printf("\nFinished Downloading: %s\n", outputFileName)
	}
	return nil
}

func downloadSingleThreaded(url, outputFileName string) error {
	outputFile, err := os.Create(outputFileName)

	if err != nil {
		return err
	}
	defer outputFile.Close()

	// Set the authorization header with the Bearer token

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err // gracefully handle request err
	}
	if RequiresAuth {
		// Set the authorization header with the Bearer token
		bearerToken := AuthToken
		req.Header.Add("Authorization", "Bearer "+bearerToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode == 401 && !RequiresAuth {
		return fmt.Errorf(errorColor("This Repo requires access token, generate an access token form huggingface, and pass it using flag: -t TOKEN"))

	}
	_, err = io.Copy(outputFile, resp.Body)
	if err != nil {
		return err
	}

	// fmt.Println("\nDownload completed")
	return nil
}
