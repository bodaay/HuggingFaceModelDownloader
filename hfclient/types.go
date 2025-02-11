package hfclient

// RepoRef represents a HuggingFace repository reference
type RepoRef struct {
	Owner string
	Name  string
	Ref   string // Can be branch name, commit SHA, or empty (defaults to "main")
}

// FullName returns the full repository name in the format "owner/name"
func (r *RepoRef) FullName() string {
	return r.Owner + "/" + r.Name
}
