package version

// Build-time variables (set via ldflags)
var (
	Version   = "1.0.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
	Edition   = "oss"
)

// GetEditionName returns edition name
func GetEditionName() string {
	return "Open Source"
}

// Info returns version information
func Info() map[string]interface{} {
	return map[string]interface{}{
		"version":      Version,
		"edition":      Edition,
		"edition_name": GetEditionName(),
		"build_time":   BuildTime,
		"git_commit":   GitCommit,
	}
}
