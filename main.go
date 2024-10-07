package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	hfd "github.com/bodaay/HuggingFaceModelDownloader/hfdownloader"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

const VERSION = "1.3.8"

type Config struct {
	NumConnections     int    `json:"num_connections"`
	RequiresAuth       bool   `json:"requires_auth"`
	AuthToken          string `json:"auth_token"`
	ModelName          string `json:"model_name"`
	DatasetName        string `json:"dataset_name"`
	Branch             string `json:"branch"`
	Storage            string `json:"storage"`
	OneFolderPerFilter bool   `json:"one_folder_per_filter"`
	SkipSHA            bool   `json:"skip_sha"`
	Install            bool   `json:"install"`
	InstallPath        string `json:"install_path"`
	MaxRetries         int    `json:"max_retries"`
	RetryInterval      int    `json:"retry_interval"`
	JustDownload       bool   `json:"just_download"`
	SilentMode         bool   `json:"silent_mode"`
	EndPoint           string `json:"end_point"`
}

// DefaultConfig returns a config instance populated with default values.
func DefaultConfig() Config {
	return Config{
		NumConnections: 5,
		Branch:         "main",
		Storage:        "./downloads",
		MaxRetries:     3,
		RetryInterval:  5,
		EndPoint:       "https://huggingface.co",
	}
}

func LoadConfig() (*Config, error) {
	config := DefaultConfig() // Use defaults as a base
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(homeDir, ".config", "hfdownloader.json")

	file, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return &config, nil // Return defaults if file does not exist
	} else if err == nil {
		if err := json.Unmarshal(file, &config); err != nil {
			return nil, err
		}
	}

	// Check if an environment variable to always enable the 'just download' feature is enabled
	envVar := os.Getenv("HFDOWNLOADER_JUST_DOWNLOAD")
	if envVar == "1" || envVar == "true" {
		config.Storage = "./" // Set storage to current directory
	}

	return &config, nil
}

func generateConfigFile() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(homeDir, ".config", "hfdownloader.json")

	config := DefaultConfig()

	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return err
	}

	fmt.Printf("Generated config file at: %s\n", configPath)
	return nil
}

func main() {
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	var justDownload bool

	rootCmd := &cobra.Command{
		Use:           "hfdownloader [model]",
		Short:         fmt.Sprintf("A Simple HuggingFace Models Downloader Utility\nVersion: %s", VERSION),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(cmd *cobra.Command, args []string) error {
			if justDownload && len(args) < 1 {
				return errors.New("requires a model name argument when using -j")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if justDownload {
				config.ModelName = args[0] // Use the first argument as the model name
				config.Storage = "./"
			}
			// Validate the ModelName parameter
			// if !hfdn.IsValidModelName(modelName) { Just realized there are indeed models that don't follow this format :)
			// 	// fmt.Println("Error:", err)
			// 	return fmt.Errorf("Invailid Model Name, it should follow the pattern: ModelAuthor/ModelName")
			// }
			// Dynamic configuration updates (e.g., for AuthToken)
			if config.AuthToken == "" {
				config.AuthToken = os.Getenv("HF_TOKEN")
				if config.AuthToken == "" {
					config.AuthToken = os.Getenv("HUGGING_FACE_HUB_TOKEN")
					if config.AuthToken != "" {
						fmt.Println("DeprecationWarning: The environment variable 'HUGGING_FACE_HUB_TOKEN' is deprecated and will be removed in a future version. Please use 'HF_TOKEN' instead.")
					}
				}
			}
			if config.Install {
				if err := installBinary(config.InstallPath); err != nil {
					log.Fatal(err)
				}
				os.Exit(0)
			}
			var IsDataset bool
			ModelOrDataSet := config.ModelName
			if config.ModelName != "" {
				fmt.Println("Model:", config.ModelName)
				IsDataset = false
			} else if config.DatasetName != "" {
				fmt.Println("Dataset:", config.DatasetName)
				IsDataset = true
				ModelOrDataSet = config.DatasetName
			} else {
				cmd.Help()
				return fmt.Errorf("Error: You must set either modelName or datasetName.")
			}

			_ = godotenv.Load() // Load .env file if exists

			if config.AuthToken == "" {
				config.AuthToken = os.Getenv("HF_TOKEN")
				if config.AuthToken == "" {
					config.AuthToken = os.Getenv("HUGGING_FACE_HUB_TOKEN")
					if config.AuthToken != "" {
						fmt.Println("DeprecationWarning: The environment variable 'HUGGING_FACE_HUB_TOKEN' is deprecated and will be removed in a future version. Please use 'HF_TOKEN' instead.")
					}
				}
			}

			// compatible with huggingface-cli
			endPoint := os.Getenv("HF_ENDPOINT")
			if endPoint != "" {
				config.EndPoint = endPoint
			}

			fmt.Printf("Branch: %s\nStorage: %s\nNumberOfConcurrentConnections: %d\nAppend Filter Names to Folder: %t\nSkip SHA256 Check: %t\nToken: %s\n",
				config.Branch, config.Storage, config.NumConnections, config.OneFolderPerFilter, config.SkipSHA, config.AuthToken)

			for i := 0; i < config.MaxRetries; i++ {
				if err := hfd.DownloadModel(config.EndPoint, ModelOrDataSet, config.OneFolderPerFilter, config.SkipSHA, IsDataset, config.Storage, config.Branch, config.NumConnections, config.AuthToken, config.SilentMode); err != nil {
					fmt.Printf("Warning: attempt %d / %d failed, error: %s\n", i+1, config.MaxRetries, err)
					time.Sleep(time.Duration(config.RetryInterval) * time.Second)
					continue
				}
				fmt.Printf("\nDownload of %s completed successfully\n", ModelOrDataSet)
				return nil
			}
			return fmt.Errorf("failed to download %s after %d attempts", ModelOrDataSet, config.MaxRetries)
		},
	}

	// Setup flags and bind them to config properties
	rootCmd.PersistentFlags().StringVarP(&config.ModelName, "model", "m", config.ModelName, "Model name to download")
	rootCmd.PersistentFlags().StringVarP(&config.DatasetName, "dataset", "d", config.DatasetName, "Dataset name to download")
	rootCmd.PersistentFlags().StringVarP(&config.Branch, "branch", "b", config.Branch, "Branch of the model or dataset")
	rootCmd.PersistentFlags().StringVarP(&config.Storage, "storage", "s", config.Storage, "Storage path for downloads")
	rootCmd.PersistentFlags().IntVarP(&config.NumConnections, "concurrent", "c", config.NumConnections, "Number of concurrent connections")
	rootCmd.PersistentFlags().StringVarP(&config.AuthToken, "token", "t", config.AuthToken, "HuggingFace Auth Token")
	rootCmd.PersistentFlags().BoolVarP(&config.OneFolderPerFilter, "appendFilterFolder", "f", config.OneFolderPerFilter, "Append filter name to folder")
	rootCmd.PersistentFlags().BoolVarP(&config.SkipSHA, "skipSHA", "k", config.SkipSHA, "Skip SHA256 hash check")
	rootCmd.PersistentFlags().IntVar(&config.MaxRetries, "maxRetries", config.MaxRetries, "Maximum number of retries for downloads")
	rootCmd.PersistentFlags().IntVar(&config.RetryInterval, "retryInterval", config.RetryInterval, "Interval between retries in seconds")
	rootCmd.PersistentFlags().BoolVarP(&justDownload, "justDownload", "j", config.JustDownload, "Just download the model to the current directory and assume the first argument is the model name")
	rootCmd.PersistentFlags().BoolVarP(&config.SilentMode, "silentMode", "q", config.SilentMode, "Disable progress bar output printing")
	rootCmd.PersistentFlags().StringVarP(&config.EndPoint, "endPoint", "e", config.EndPoint, "HuggingFace end point url")

	// Add the generate-config command
	generateCmd := &cobra.Command{
		Use:   "generate-config",
		Short: "Generates an example configuration file with default values",
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateConfigFile()
		},
	}

	rootCmd.AddCommand(generateCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln("Error:", err)
	}
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

	return copyFile(dst, srcFile)
}

// Try to copy the file
func copyFile(dst string, src *os.File) error {
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, src)
	return err
}
