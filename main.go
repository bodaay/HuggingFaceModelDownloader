package main

import (
	"errors"
	"fmt"
	hfd "hfdownloader/hfdownloader"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"

	"log"
	"os"

	"github.com/spf13/cobra"
)

const VERSION = "1.2.5"

func main() {
	var (
		modelName                     string
		datasetName                   string
		branch                        string
		storage                       string
		numberOfConcurrentConnections int
		HuggingFaceAccessToken        string
		OneFolderPerFilter            bool
		SkipSHA                       bool
		install                       bool
		installPath                   string
	)
	ShortString := fmt.Sprintf("a Simple HuggingFace Models Downloader Utility\nVersion: %s", VERSION)
	currentPath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get execuable path, %s", err)
	}
	if currentPath != "" {
		ShortString = fmt.Sprintf("%s\nRunning on: %s", ShortString, currentPath)
	}
	rootCmd := &cobra.Command{
		Use:   "hfdowloader",
		Short: ShortString,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate the ModelName parameter
			// if !hfdn.IsValidModelName(modelName) { Just realized there are indeed models that don't follow this format :)
			// 	// fmt.Println("Error:", err)
			// 	return fmt.Errorf("Invailid Model Name, it should follow the pattern: ModelAuthor/ModelName")
			// }
			if install {
				err := installBinary(installPath)
				if err != nil {
					log.Fatal(err)
				}
				os.Exit(0)
			}
			var IsDataset bool
			if (modelName == "" && datasetName == "") || (modelName != "" && datasetName != "") {
				cmd.Help()
				return fmt.Errorf("Error: You must set either modelName or datasetName, not both or neither.")

			}
			ModelOrDataSet := modelName
			// Print the parameter values
			if modelName != "" {
				fmt.Println("Model:", modelName)
				IsDataset = false //no need to speicfy it here, just cleaner
				ModelOrDataSet = modelName
			}
			if datasetName != "" {
				fmt.Println("Dataset:", datasetName)
				IsDataset = true
				ModelOrDataSet = datasetName
			}
			fmt.Println("Branch:", branch)
			fmt.Println("Storage:", storage)
			fmt.Println("NumberOfConcurrentConnections:", numberOfConcurrentConnections)
			fmt.Println("Append Filter Names to Folder:", OneFolderPerFilter)
			fmt.Println("Skip SHA256 Check:", SkipSHA)
			fmt.Println("Token:", HuggingFaceAccessToken)

			err := hfd.DownloadModel(ModelOrDataSet, OneFolderPerFilter, SkipSHA, IsDataset, storage, branch, numberOfConcurrentConnections, HuggingFaceAccessToken)
			if err != nil {
				return err
			}
			fmt.Printf("\nDownload of %s completed successfully\n", ModelOrDataSet)
			return nil
		},
	}
	rootCmd.SilenceUsage = true // I'll manually print help them while validating the parameters above
	rootCmd.Flags().SortFlags = false
	// Define flags for command-line parameters
	rootCmd.Flags().StringVarP(&modelName, "model", "m", "", "Model/Dataset name (required if dataset not set)\nYou can supply filters for required LFS model files\nex:  ModelName:q4_0,q8_1\nex:  TheBloke/WizardLM-Uncensored-Falcon-7B-GGML:fp16")

	rootCmd.Flags().StringVarP(&datasetName, "dataset", "d", "", "Model/Dataset name (required if model not set)")

	rootCmd.Flags().StringVarP(&branch, "branch", "b", "main", "ModModel/Datasetel branch (optional)")

	rootCmd.Flags().StringVarP(&storage, "storage", "s", "Storage", "Storage path (optional)")

	rootCmd.Flags().BoolVarP(&SkipSHA, "skipSHA", "k", false, "Skip SHA256 Hash Check, sometimes you just need to download missing files without wasting time waiting (optional)")

	rootCmd.Flags().BoolVarP(&OneFolderPerFilter, "appendFilterFolder", "f", false, "This will append the filter name to the folder, use it for GGML qunatizatized filterd download only (optional)")

	rootCmd.Flags().IntVarP(&numberOfConcurrentConnections, "concurrent", "c", 5, "Number of LFS concurrent connections (optional)")

	rootCmd.Flags().StringVarP(&HuggingFaceAccessToken, "token", "t", "", "HuggingFace Access Token, required for some Models/Datasets, you still need to manually accept agreement if model requires it (optional)")

	rootCmd.Flags().BoolVarP(&install, "install", "i", false, "Install the binary to the OS default bin folder, Unix-like operating systems only")

	rootCmd.Flags().StringVarP(&installPath, "installPath", "p", "/usr/local/bin/", "install Path (optional)")

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln("Error:", err)
	}

	os.Exit(0)
}
func installBinary(installPath string) error {
	if runtime.GOOS == "windows" {
		return errors.New("the install command is not supported on Windows")
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	dst := path.Join(installPath, filepath.Base(exePath))

	// Check if the binary already exists and remove it
	if _, err := os.Stat(dst); err == nil {
		os.Remove(dst)
	}

	// Open source file
	srcFile, err := os.Open(exePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Try to copy the file
	err = copyFile(dst, srcFile)
	if err != nil {
		if os.IsPermission(err) {
			// If permission error, try to elevate privilege
			fmt.Printf("Require sudo privilages to install to: %s\n", installPath)
			cmd := exec.Command("sudo", "cp", exePath, dst)
			if err := cmd.Run(); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	log.Printf("The binary has been copied to %s", dst)
	return nil
}

// copyFile is a helper function to copy a file with specific permission
func copyFile(dst string, src *os.File) error {
	// Open destination file and ensure it gets closed
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy the file content
	if _, err := io.Copy(dstFile, src); err != nil {
		return err
	}
	return nil
}
