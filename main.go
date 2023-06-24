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
		datasetName                   string
		branch                        string
		destinationPath               string
		numberOfConcurrentConnections int
		HuggingFaceAccessToken        string
	)
	rootCmd := &cobra.Command{
		Use:   "hfdowloader",
		Short: "a Simple HuggingFace Models Downloader Utility",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate the ModelName parameter
			// if !hfdn.IsValidModelName(modelName) { Just realized there are indeed models that don't follow this format :)
			// 	// fmt.Println("Error:", err)
			// 	return fmt.Errorf("Invailid Model Name, it should follow the pattern: ModelAuthor/ModelName")
			// }
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
			fmt.Println("DestinationPath:", destinationPath)
			fmt.Println("NumberOfConcurrentConnections:", numberOfConcurrentConnections)
			fmt.Println("Token:", HuggingFaceAccessToken)

			err := hfdn.DownloadModel(ModelOrDataSet, IsDataset, destinationPath, branch, numberOfConcurrentConnections, HuggingFaceAccessToken)
			if err != nil {
				return err
			}
			return nil
		},
	}
	rootCmd.SilenceUsage = true // I'll manually print help them while validating the parameters above
	rootCmd.Flags().SortFlags = false
	// Define flags for command-line parameters
	rootCmd.Flags().StringVarP(&modelName, "model", "m", "", "Model/Dataset name (required if dataset not set)")

	rootCmd.Flags().StringVarP(&datasetName, "dataset", "d", "", "Model/Dataset name (required if model not set)")

	rootCmd.Flags().StringVarP(&branch, "branch", "b", "main", "ModModel/Datasetel branch (optional)")

	rootCmd.Flags().StringVarP(&destinationPath, "storage", "s", "Storage", "Destination path (optional)")

	rootCmd.Flags().IntVarP(&numberOfConcurrentConnections, "concurrent", "c", 5, "Number of LFS concurrent connections (optional)")

	rootCmd.Flags().StringVarP(&HuggingFaceAccessToken, "token", "t", "", "HuggingFace Access Token, required for some Models/Datasets, you still need to manually accept agreement if model requires it (optional)")

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln("Error:", err)
	}

	os.Exit(0)
}
