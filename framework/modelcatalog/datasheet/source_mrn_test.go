package datasheet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceResolver(t *testing.T) {
	require.False(t, SourceResolverInstalled())
	require.Equal(t, DefaultURL, Config{}.resolved().URL)

	SetSourceResolver(func(kind SourceKind, _ string) string {
		if kind == SourcePricing {
			return "file:///intranet/pricing.json"
		}
		return ""
	})
	t.Cleanup(func() { SetSourceResolver(nil) })

	require.True(t, SourceResolverInstalled())
	c := Config{URL: "https://example.invalid/pricing"}.resolved()
	require.Equal(t, "file:///intranet/pricing.json", c.URL)
	require.Empty(t, c.ModelParametersURL, "a skipped dataset resolves to an empty URL")
}
