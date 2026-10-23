package modelcatalog

import (
	"context"

	"github.com/neria-cloud/meridian-base/framework/modelcatalog/datasheet"
)

func resolveMCPLibrarySource(configuredURL string) string {
	return datasheet.ResolveSource(datasheet.SourceMCPLibrary, configuredURL)
}

// toleratedStartupErr keeps a failed startup load or sync from failing Init while a
// source resolver owns the catalog sources.
func (mc *ModelCatalog) toleratedStartupErr(err error) error {
	if err == nil || !datasheet.SourceResolverInstalled() {
		return err
	}
	mc.logger.Warn("model catalog startup continues without the dataset: %v", err)
	return nil
}

// loadIntoMemory runs a store-less startup load unless the resolver skipped the dataset (url "").
func (mc *ModelCatalog) loadIntoMemory(ctx context.Context, url string, load func(context.Context) error) error {
	if url == "" {
		return nil
	}
	return mc.toleratedStartupErr(load(ctx))
}
