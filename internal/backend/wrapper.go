package backend

import (
	"github.com/atlassian/gostatsd"
	"github.com/comfortablynumb/victor/internal/config"
	"github.com/comfortablynumb/victor/internal/util"
	"github.com/spf13/viper"
)

// Static functions

func NewWrappedBackend(backend gostatsd.Backend, v *viper.Viper) gostatsd.Backend {
	rateLimitViper := util.GetSubViper(v, config.ParamRateLimit)

	if rateLimitViper.GetBool(config.ParamEnabled) {
		return NewRateLimitedBackend(backend, rateLimitViper)
	}

	return backend
}
