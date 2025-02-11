package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lxe/hfdownloader/hfclient"
	"github.com/spf13/cobra"
)

const VERSION = "2.0.0"

type Config struct {
	Token          string
	NumConnections int
	SkipVerify     bool
	Filters        []string
	DestinationMap map[string]string
	Repo           string
	Branch         string
	AutoConfirm    bool
}

func main() {
	config := &Config{
		NumConnections: 5,
		DestinationMap: make(map[string]string),
		Branch:         "main", // default branch
		AutoConfirm:    false,
	}

	var filterMappings []string

	rootCmd := &cobra.Command{
		Use:   "hfdownloader",
		Short: fmt.Sprintf("HuggingFace Fast Downloader v%s", VERSION),
		Long: `A fast and efficient tool for downloading files from HuggingFace repositories.
Use -r flag to specify repository in the format 'owner/name'.`,
		Example: `  hfdownloader -r runwayml/stable-diffusion-v1-5 list
  hfdownloader -r runwayml/stable-diffusion-v1-5 download -f "*.safetensors"`,
		SilenceErrors: false,
		SilenceUsage:  false,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// List command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List files in the repository",
		Example: `  hfdownloader -r runwayml/stable-diffusion-v1-5 list
  hfdownloader -r runwayml/stable-diffusion-v1-5 list -b main`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if config.Repo == "" {
				return fmt.Errorf("repository must be specified with -r flag")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return listFiles(config)
		},
	}

	// Download command
	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from the repository",
		Long: `Download files from the repository using patterns and optional destination paths.
Use -f flag multiple times to specify file patterns and destinations.
Pattern format: "pattern[:destination]"

Examples:
  hfdownloader -r runwayml/stable-diffusion-v1-5 download -f "*.safetensors"
  hfdownloader -r org/model download -b main \
    -f "model.safetensors:models/my-model.safetensors" \
    -f "vae.pt:models/vae/"

Pattern examples:
  - "*.safetensors"                              # Download all safetensors files
  - "model.safetensors:models/my-model.safetensors"  # Download with new name
  - "model.pt:models/checkpoints/"               # Keep original name in directory`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if config.Repo == "" {
				return fmt.Errorf("repository must be specified with -r flag")
			}
			if len(filterMappings) == 0 {
				return fmt.Errorf("at least one filter (-f) must be specified")
			}
			for _, mapping := range filterMappings {
				pattern, dest, err := parseFilterMapping(mapping)
				if err != nil {
					return err
				}
				config.Filters = append(config.Filters, pattern)
				if dest != "" {
					config.DestinationMap[pattern] = dest
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return downloadFiles(config)
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&config.Token, "token", "t", os.Getenv("HF_TOKEN"), "HuggingFace API token")
	rootCmd.PersistentFlags().StringVarP(&config.Repo, "repo", "r", "", "Repository name (required, format: owner/name)")
	rootCmd.PersistentFlags().StringVarP(&config.Branch, "branch", "b", "main", "Repository branch or commit hash")
	rootCmd.PersistentFlags().IntVarP(&config.NumConnections, "connections", "c", 5, "Number of concurrent download connections")
	rootCmd.PersistentFlags().BoolVarP(&config.SkipVerify, "skip-verify", "s", false, "Skip SHA verification")
	rootCmd.PersistentFlags().BoolVarP(&config.AutoConfirm, "yes", "y", false, "Auto confirm all prompts")

	// Download command specific flags
	downloadCmd.Flags().StringArrayVarP(&filterMappings, "filter", "f", []string{}, "File filter with optional destination (pattern[:destination])")

	// Add commands to root command
	rootCmd.AddCommand(listCmd, downloadCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFilterMapping(mapping string) (pattern string, destination string, err error) {
	parts := strings.Split(mapping, ":")
	if len(parts) == 1 {
		return parts[0], "", nil
	}
	if len(parts) == 2 {
		pattern = parts[0]
		destination = parts[1]

		// Check if destination exists
		destInfo, err := os.Stat(destination)
		if err == nil && destInfo.IsDir() {
			// If destination exists and is a directory, we'll use the original filename later
			// when creating the download task, not here
			fmt.Printf("Warning: destination '%s' is a directory - writing into it\n", parts[1])
		} else if strings.HasSuffix(destination, "/") {
			// If destination ends with /, we'll treat it as a directory
			// and use the original filename later when creating the download task
			destination = strings.TrimSuffix(destination, "/")
		}
		// Otherwise, use the destination as the full file path

		return pattern, destination, nil
	}
	return "", "", fmt.Errorf("invalid filter mapping format: %s", mapping)
}

func parseRepo(repo string, branch string) (*hfclient.RepoRef, error) {
	// Split repo path (owner/repo)
	repoParts := strings.Split(repo, "/")
	if len(repoParts) != 2 {
		return nil, fmt.Errorf("invalid repository format. Expected 'owner/name', got '%s'", repo)
	}

	return &hfclient.RepoRef{
		Owner: repoParts[0],
		Name:  repoParts[1],
		Ref:   branch,
	}, nil
}

func listFiles(config *Config) error {
	repoRef, err := parseRepo(config.Repo, config.Branch)
	if err != nil {
		return err
	}

	client := hfclient.NewClient(config.Token)
	files, err := client.ListFiles(repoRef)
	if err != nil {
		return err
	}

	// Print files in a tree-like format
	hfclient.PrintFileTree(files)
	return nil
}

func downloadFiles(config *Config) error {
	repoRef, err := parseRepo(config.Repo, config.Branch)
	if err != nil {
		return err
	}

	client := hfclient.NewClient(config.Token)
	files, err := client.ListFiles(repoRef)
	if err != nil {
		return err
	}

	// Filter files based on patterns
	matchedFiles := hfclient.FilterFiles(files, config.Filters)
	if len(matchedFiles) == 0 {
		return fmt.Errorf("no files matched the specified filters: %v", config.Filters)
	}

	// Print what we're going to download
	fmt.Println("Files to download:")
	for _, file := range matchedFiles {
		dest := config.DestinationMap[file.Pattern]
		if dest == "" {
			dest = file.Path
		} else {
			// If destination is or should be a directory, append the original filename
			destInfo, err := os.Stat(dest)
			if (err == nil && destInfo.IsDir()) || strings.HasSuffix(dest, "/") {
				dest = filepath.Join(dest, filepath.Base(file.Path))
			}
		}

		// Create destination directory
		destDir := filepath.Dir(dest)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", destDir, err)
		}

		fmt.Printf("  %s -> %s (%s)\n", file.Path, dest, formatSize(file.Size))
	}

	// Confirm with user unless auto-confirm is enabled
	if !config.AutoConfirm {
		fmt.Print("\nProceed with download? [y/N] ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			return fmt.Errorf("download cancelled by user")
		}
	} else {
		fmt.Println("\nAuto-confirming download...")
	}

	// Create download tasks
	var tasks []hfclient.DownloadTask
	for _, file := range matchedFiles {
		dest := config.DestinationMap[file.Pattern]
		if dest == "" {
			dest = file.Path
		} else {
			// If destination is or should be a directory, append the original filename
			destInfo, err := os.Stat(dest)
			if (err == nil && destInfo.IsDir()) || strings.HasSuffix(dest, "/") {
				dest = filepath.Join(dest, filepath.Base(file.Path))
			}
		}
		tasks = append(tasks, hfclient.DownloadTask{
			File:        file,
			Destination: dest,
		})
	}

	// Start the download manager with the specified number of connections
	dm := hfclient.NewDownloadManager(client, config.NumConnections, config.SkipVerify)
	return dm.Download(tasks)
}

// Helper function to format file sizes
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
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

	// Check if we need sudo for either removing the existing binary or copying the new one
	needsSudo := false

	// Check if the binary already exists
	if _, err := os.Stat(dst); err == nil {
		// Try to remove the existing binary
		err := os.Remove(dst)
		if err != nil {
			if os.IsPermission(err) {
				needsSudo = true
			} else {
				return err
			}
		}
	}

	// Open the source file
	srcFile, err := os.Open(exePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Try to copy the file
	err = copyFile(dst, srcFile)
	if err != nil {
		if os.IsPermission(err) {
			needsSudo = true
		} else {
			return err
		}
	}

	// If we need sudo, handle both removal and copy with elevated privileges
	if needsSudo {
		fmt.Printf("Require sudo privileges to complete installation at: %s\n", installPath)
		cmd := exec.Command("sudo", "sh", "-c", fmt.Sprintf("rm -f %s && cp %s %s", dst, exePath, dst))
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	log.Printf("The binary has been successfully installed to %s", dst)
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
