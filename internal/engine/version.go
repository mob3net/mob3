package engine

// BuildVersion is the application version injected by the app. Built-in pages
// display it; the loader does not choose it.
var BuildVersion = "dev"

// SetBuildVersion installs the application version.
func SetBuildVersion(v string) { BuildVersion = v }

// HelpPageDocument renders the built-in help page with the current version.
func HelpPageDocument() string { return HelpPageHTML(BuildVersion) }
