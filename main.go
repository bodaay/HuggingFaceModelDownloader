package main

import (
	"errors"
	"fmt"
	hfdn "hfdownloader/hfdownloadernested"

	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	var (
		modelName   string
		storagePath string
		modelBranch string
	)
	rootCmd := &cobra.Command{
		Use:   "hfdowloader modelname [storagepath] [Branch]",
		Short: "a Simple HuggingFace Models Downloader Utility",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				modelName = args[0]
			}
			storagePath = "Models"
			if len(args) > 1 {
				storagePath = args[1]
			}
			modelBranch = "main"
			if len(args) > 2 {
				modelBranch = strings.ToLower(args[2])
			}

			if len(args) == 0 && modelName == "" {
				return errors.New("Model name is required")
			}

			if modelName != "" && !hfdn.IsValidModelName(modelName) {
				return fmt.Errorf("Invalid model name format '%s'. It should follow the pattern 'ModelAuthor/ModelName'", modelName)
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			err := hfdn.DownloadModel(modelName, storagePath, modelBranch)
			if err != nil {
				return err
			}
			return nil
		},
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln("Error:", err)
		os.Exit(1)
	}

	os.Exit(0)
}
