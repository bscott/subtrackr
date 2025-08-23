package version

var (
	// GitCommit is the git commit SHA that will be set at build time
	GitCommit = "unknown"
	// Version is the semantic version (if needed)
	Version = "dev"
)

// GetVersion returns the current version string
func GetVersion() string {
	if GitCommit != "unknown" && GitCommit != "" {
		return GitCommit
	}
	return Version
}