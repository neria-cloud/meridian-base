package oauth2

import "github.com/neria-cloud/meridian-base/core/schemas"

var logger schemas.Logger

func SetLogger(l schemas.Logger) {
	logger = l
}
