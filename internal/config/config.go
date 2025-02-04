package config

import "time"

// Constants

const (
	ParamBackends           = "backends"
	ParamRateLimit          = "rate-limit"
	
	// Rate Limit Configs
	
	ParamClearAfterDuration = "clear-after-duration"
	ParamLimit              = "limit"
	ParamEnabled            = "enabled"

	DefaultClearAfterDuration = 1 * time.Hour
	DefaultLimit              = 10000
)
