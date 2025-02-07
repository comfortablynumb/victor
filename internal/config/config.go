package config

import "time"

// Constants

const (
	ParamBackends  = "backends"
	ParamRateLimit = "rate-limit"

	// Rate Limit Configs

	ParamClearAfterDuration = "clear-after-duration"
	ParamDefaultLimit       = "default-limit"
	ParamLimitByMetricName  = "limit-by-metric-name"
	ParamEnabled            = "enabled"

	DefaultClearAfterDuration = 1 * time.Hour
	DefaultLimit              = 10000
)
