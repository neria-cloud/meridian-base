package modelcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	meridian "github.com/neria-cloud/meridian-base/core"
	"github.com/neria-cloud/meridian-base/core/schemas"
	"github.com/neria-cloud/meridian-base/framework/modelcatalog/datasheet"
)

func resolveSourcesTo(t *testing.T, urls map[datasheet.SourceKind]string) {
	datasheet.SetSourceResolver(func(kind datasheet.SourceKind, _ string) string { return urls[kind] })
	t.Cleanup(func() { datasheet.SetSourceResolver(nil) })
}

func TestInit_ResolvedSources(t *testing.T) {
	pricing := filepath.Join(t.TempDir(), "pricing.json")
	require.NoError(t, os.WriteFile(pricing, []byte(`{"m1": {"provider":"openai","mode":"chat","base_model":"m1"}}`), 0o600))
	resolveSourcesTo(t, map[datasheet.SourceKind]string{datasheet.SourcePricing: "file://" + pricing})

	mc, err := Init(t.Context(), nil, nil, meridian.NewDefaultLogger(schemas.LogLevelError))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mc.Cleanup() })
	require.NotNil(t, mc.datasheet.GetPricingEntryForModel("m1", schemas.OpenAI), "pricing comes from the resolved URL")
	require.NoError(t, mc.runParamsSync(t.Context()), "a skipped dataset is never fetched")
	require.Empty(t, resolveMCPLibrarySource(DefaultMCPLibraryURL))
}

func TestInit_ToleratesAFailedStartupLoadOnlyWhenResolved(t *testing.T) {
	missing := "file://" + filepath.Join(t.TempDir(), "absent.json")
	cfg := &Config{PricingURL: &missing, ModelParametersURL: &missing}
	logger := meridian.NewDefaultLogger(schemas.LogLevelError)
	quick := func() context.Context {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond) // cuts the retry backoff
		t.Cleanup(cancel)
		return ctx
	}

	_, err := Init(quick(), cfg, nil, logger)
	require.Error(t, err)

	resolveSourcesTo(t, map[datasheet.SourceKind]string{datasheet.SourcePricing: missing, datasheet.SourceModelParameters: missing})
	mc, err := Init(quick(), cfg, nil, logger)
	require.NoError(t, err)
	require.NoError(t, mc.Cleanup())
}
