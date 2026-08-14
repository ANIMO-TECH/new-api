package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type HealthProbeSetting struct {
	Enabled               bool    `json:"enabled"`
	ScanIntervalSeconds   int     `json:"scan_interval_seconds"`
	RequestTimeoutSeconds int     `json:"request_timeout_seconds"`
	Concurrency           int     `json:"concurrency"`
	InitialDelaySeconds   int     `json:"initial_delay_seconds"`
	BackoffMultiplier     float64 `json:"backoff_multiplier"`
	MaxBackoffSeconds     int     `json:"max_backoff_seconds"`
	MaxAttempts           int     `json:"max_attempts"`
	NotifyOnRecovery      bool    `json:"notify_on_recovery"`
	NotifyOnExhausted     bool    `json:"notify_on_exhausted"`
}

var healthProbeSetting = HealthProbeSetting{
	Enabled:               false,
	ScanIntervalSeconds:   30,
	RequestTimeoutSeconds: 120,
	Concurrency:           1,
	InitialDelaySeconds:   60,
	BackoffMultiplier:     2,
	MaxBackoffSeconds:     3600,
	MaxAttempts:           0,
	NotifyOnRecovery:      true,
	NotifyOnExhausted:     true,
}

func init() {
	config.GlobalConfig.Register("health_probe_setting", &healthProbeSetting)
}

func GetHealthProbeSetting() HealthProbeSetting {
	setting := healthProbeSetting
	if setting.ScanIntervalSeconds < 15 {
		setting.ScanIntervalSeconds = 15
	}
	if setting.RequestTimeoutSeconds < 1 {
		setting.RequestTimeoutSeconds = 120
	}
	if setting.RequestTimeoutSeconds > 600 {
		setting.RequestTimeoutSeconds = 600
	}
	if setting.Concurrency < 1 {
		setting.Concurrency = 1
	}
	if setting.Concurrency > 20 {
		setting.Concurrency = 20
	}
	if setting.InitialDelaySeconds < 1 {
		setting.InitialDelaySeconds = 60
	}
	if setting.BackoffMultiplier < 1 {
		setting.BackoffMultiplier = 1
	}
	if setting.BackoffMultiplier > 10 {
		setting.BackoffMultiplier = 10
	}
	if setting.MaxBackoffSeconds < setting.InitialDelaySeconds {
		setting.MaxBackoffSeconds = setting.InitialDelaySeconds
	}
	if setting.MaxAttempts < 0 {
		setting.MaxAttempts = 0
	}
	return setting
}
