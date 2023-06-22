package hfdownloader

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

//To get File List Tree in Json

// https://huggingface.co/api/models/{Model}/tree/{branch}

//To get LFS file path, follow this link
//  https://huggingface.co/{Model}/resolve/{branch}/{filename}

func DownloadModel(ModelName string, DestintionBasePath string, silent bool) error {
	// Send a GET request to the URL

	branch := "main"
	JsonFileListURL := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/%s", ModelName, branch)
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
	fmt.Println(content)
	return nil
}
