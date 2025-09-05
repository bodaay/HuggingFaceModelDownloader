package hfdownloader

import "time"

type Job struct {
	Repo               string
	IsDataset          bool
	Revision           string
	Filters            []string
	AppendFilterSubdir bool
}
type Settings struct {
	OutputDir          string
	Concurrency        int
	MaxActiveDownloads int
	MultipartThreshold string
	Verify             string
	Retries            int
	BackoffInitial     string
	BackoffMax         string
	Token              string
}

type ProgressEvent struct {
	Time     time.Time `json:"time"`
	Level    string    `json:"level,omitempty"`
	Event    string    `json:"event"`
	Repo     string    `json:"repo,omitempty"`
	Revision string    `json:"revision,omitempty"`
	Path     string    `json:"path,omitempty"`
	Bytes    int64     `json:"bytes,omitempty"`
	Total    int64     `json:"total,omitempty"`
	Attempt  int       `json:"attempt,omitempty"`
	Message  string    `json:"message,omitempty"`
}

type ProgressFunc func(ProgressEvent)
