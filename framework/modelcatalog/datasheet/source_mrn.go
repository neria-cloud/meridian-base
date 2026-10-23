package datasheet

import "sync/atomic"

// SourceKind names one catalog dataset.
type SourceKind string

const (
	SourcePricing         SourceKind = "pricing"
	SourceModelParameters SourceKind = "model_parameters"
	SourceMCPLibrary      SourceKind = "mcp_library"
)

// SourceResolver maps the configured URL of a dataset to the URL to fetch; "" skips the dataset.
type SourceResolver func(kind SourceKind, configuredURL string) string

var sourceResolver atomic.Pointer[SourceResolver]

// SetSourceResolver installs r for the process; it MUST run before the catalog is built.
// While a resolver is installed, a failed startup load or sync is logged, not fatal.
// nil restores the configured URLs.
func SetSourceResolver(r SourceResolver) {
	if r == nil {
		sourceResolver.Store(nil)
		return
	}
	sourceResolver.Store(&r)
}

// ResolveSource returns the URL to fetch for kind; "" when the dataset is skipped.
func ResolveSource(kind SourceKind, configuredURL string) string {
	if r := sourceResolver.Load(); r != nil {
		return (*r)(kind, configuredURL)
	}
	return configuredURL
}

// SourceResolverInstalled reports whether a resolver owns the catalog sources.
func SourceResolverInstalled() bool { return sourceResolver.Load() != nil }
