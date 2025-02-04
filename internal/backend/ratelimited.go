package backend

import (
	"context"
	"sync"
	"time"

	"github.com/atlassian/gostatsd"
	"github.com/comfortablynumb/victor/internal/config"
	"github.com/comfortablynumb/victor/internal/hyperloglog"
	"github.com/comfortablynumb/victor/internal/util"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Structs

type RateLimitedBackend struct {
	backend                 gostatsd.Backend
	hyperLogLogByMetricName map[string]*hyperloglog.HyperLogLog
	mutex                   *sync.RWMutex
	limit                   uint64
	clearAfterDuration      time.Duration
	lastClearTime           time.Time
}

func (b *RateLimitedBackend) SendMetricsAsync(ctx context.Context, metricMap *gostatsd.MetricMap, callback gostatsd.SendCallback) {
	b.rateLimit(metricMap)

	b.backend.SendMetricsAsync(ctx, metricMap, callback)
}

func (b *RateLimitedBackend) SendEvent(ctx context.Context, event *gostatsd.Event) error {
	return b.backend.SendEvent(ctx, event)
}

func (b *RateLimitedBackend) Name() string {
	return b.backend.Name()
}

func (b *RateLimitedBackend) rateLimit(metricMap *gostatsd.MetricMap) {
	metrics := metricMap.AsMetrics()

	// Check if we need to drop some metrics

	for _, metric := range metrics {
		if _, valid := b.estimate(metric.Name, metric.Tags.SortedString(), b.limit); !valid {
			if metric.Type == gostatsd.COUNTER {
				metricMap.Counters.Delete(metric.Name)
			} else if metric.Type == gostatsd.GAUGE {
				metricMap.Gauges.Delete(metric.Name)
			} else if metric.Type == gostatsd.TIMER {
				metricMap.Timers.Delete(metric.Name)
			} else if metric.Type == gostatsd.SET {
				metricMap.Sets.Delete(metric.Name)
			}
		}
	}
}

func (b *RateLimitedBackend) addMetricTags(metricName string, tags string) {
	b.mutex.Lock()

	defer b.mutex.Unlock()

	b.hyperLogLogByMetricName[metricName].Insert(tags)
}

func (b *RateLimitedBackend) estimate(metricName string, tags string, limit uint64) (uint64, bool) {
	b.mutex.RLock()

	val, found := b.hyperLogLogByMetricName[metricName]

	b.mutex.RUnlock()

	if !found {
		b.addNewMetric(metricName, tags)

		return 0, true
	}

	res := val.Estimate()

	if res < limit {
		val.Insert(tags)
		return res, true
	}

	return res, false
}

func (b *RateLimitedBackend) addNewMetric(metricName string, tags string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	hyperLogLog := hyperloglog.NewHyperLogLog(tags)

	b.hyperLogLogByMetricName[metricName] = hyperLogLog
}

// Static functions

func NewRateLimitedBackend(
	backendToRateLimit gostatsd.Backend,
	v *viper.Viper,
) *RateLimitedBackend {
	// Rate limits configs

	v = util.GetSubViper(v, "rate-limits")

	// Rate limit configs for this backend

	v = util.GetSubViper(v, backendToRateLimit.Name())

	v.SetDefault(config.ParamEnabled, false)
	v.SetDefault(config.ParamLimit, config.DefaultLimit)
	v.SetDefault(config.ParamClearAfterDuration, config.DefaultClearAfterDuration)

	limit := v.GetUint64(config.ParamLimit)
	clearAfterDuration := v.GetDuration(config.ParamClearAfterDuration)
	hyperLogLogByMetricName := make(map[string]*hyperloglog.HyperLogLog, 100)

	logrus.Infof("Rate limit is enabled for backend: %s - Limit: %d - Clear after duration: %s", backendToRateLimit.Name(), limit, clearAfterDuration)

	return &RateLimitedBackend{
		backend:                 backendToRateLimit,
		hyperLogLogByMetricName: hyperLogLogByMetricName,
		mutex:                   &sync.RWMutex{},
		limit:                   limit,
		clearAfterDuration:      clearAfterDuration,
		lastClearTime:           time.Now(),
	}
}
