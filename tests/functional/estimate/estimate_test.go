// Package estimate exercises ModelCatalog.EstimateMaxCost through the public API.
package estimate

import (
	"testing"

	"github.com/neria-cloud/meridian-base/core/schemas"
	"github.com/neria-cloud/meridian-base/framework/configstore/tables"
	"github.com/neria-cloud/meridian-base/framework/modelcatalog"
	"github.com/neria-cloud/meridian-base/tests/functional/internal/fixtures"
)

const openai = schemas.OpenAI

var none = modelcatalog.EstimateOptions{}

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }

func chatRow(in, out float64, maxIn, maxOut int) tables.TableModelPricing {
	row := tables.TableModelPricing{Mode: "chat", InputCostPerToken: f(in), OutputCostPerToken: f(out)}
	if maxIn > 0 {
		row.MaxInputTokens = i(maxIn)
	}
	if maxOut > 0 {
		row.MaxOutputTokens = i(maxOut)
	}
	return row
}

func chatCatalog(t *testing.T, row tables.TableModelPricing) *modelcatalog.ModelCatalog {
	t.Helper()
	return fixtures.NewCatalog(t, fixtures.Row{Model: "gpt-test", Provider: "openai", RequestType: schemas.ChatCompletionRequest, Pricing: row})
}
