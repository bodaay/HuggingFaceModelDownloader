package main

import (
	"fmt"
	hfdn "hfdownloader/hfdownloadernested"

	"log"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	var (
		modelName                     string
		modelBranch                   string
		destinationPath               string
		numberOfConcurrentConnections int
	)
	rootCmd := &cobra.Command{
		Use:   "hfdowloader",
		Short: "a Simple HuggingFace Models Downloader Utility",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate the ModelName parameter
			if !hfdn.IsValidModelName(modelName) {
				// fmt.Println("Error:", err)
				return fmt.Errorf("Invailid Model Name, it should follow the pattern: ModelAuthor/ModelName")
			}

			// Print the parameter values
			fmt.Println("ModelName:", modelName)
			fmt.Println("DestinationPath:", destinationPath)

			fmt.Println("NumberOfConcurrentConnections:", numberOfConcurrentConnections)
			err := hfdn.DownloadModel(modelName, destinationPath, modelBranch, numberOfConcurrentConnections)
			if err != nil {
				return err
			}
			return nil
		},
	}
	// Define flags for command-line parameters
	rootCmd.Flags().StringVarP(&modelName, "model-name", "m", "", "Model name (required, pattern: ModelAuthor/ModelName)")
	rootCmd.MarkFlagRequired("model-name")

	rootCmd.Flags().StringVarP(&modelBranch, "model-branch", "b", "main", "Model branch (optional)")

	rootCmd.Flags().StringVarP(&destinationPath, "destination-path", "d", "Models", "Destination path (optional)")

	rootCmd.Flags().IntVarP(&numberOfConcurrentConnections, "concurrent", "c", 5, "Number of LFS concurrent connections (optional)")

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln("Error:", err)
	}

	os.Exit(0)
}
