package browser

import "browser/internal/engine"

// buildVersion is the app's build version, set at startup with SetBuildVersion.
// It is used by session-generated pages, the config view, the copy header and
// MCP metadata so they agree with -version.
var buildVersion = "dev"

// SetBuildVersion supplies the build version used by session-generated pages.
func SetBuildVersion(v string) {
	if v == "" {
		v = "dev"
	}
	buildVersion = v
}

// HelpPageHTML renders the built-in help page with the current build version.
func HelpPageHTML() string { return engine.HelpPageHTML(buildVersion) }
