package wrapper

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

type BackendWrapper struct {
	backend                 gostatsd.Backend
	hyperLogLogByMetricName map[string]*hyperloglog.HyperLogLog
	mutex                   *sync.RWMutex
	limit                   uint64
	clearAfterDuration      time.Duration
	lastClearTime           time.Time
	enableRateLimit         bool
}

func (b *BackendWrapper) SendMetricsAsync(ctx context.Context, metricMap *gostatsd.MetricMap, callback gostatsd.SendCallback) {
	if b.enableRateLimit {
		b.rateLimit(metricMap)
	}

	b.backend.SendMetricsAsync(ctx, metricMap, callback)
}

func (b *BackendWrapper) SendEvent(ctx context.Context, event *gostatsd.Event) error {
	return b.backend.SendEvent(ctx, event)
}

func (b *BackendWrapper) Name() string {
	return b.backend.Name()
}

func (b *BackendWrapper) rateLimit(metricMap *gostatsd.MetricMap) {
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

func (b *BackendWrapper) addMetricTags(metricName string, tags string) {
	b.mutex.Lock()

	defer b.mutex.Unlock()

	b.hyperLogLogByMetricName[metricName].Insert(tags)
}

func (b *BackendWrapper) estimate(metricName string, tags string, limit uint64) (uint64, bool) {
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

func (b *BackendWrapper) addNewMetric(metricName string, tags string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	hyperLogLog := hyperloglog.NewHyperLogLog(tags)

	b.hyperLogLogByMetricName[metricName] = hyperLogLog
}

// Static functions

func NewBackendWrapper(
	backendToWrap gostatsd.Backend,
	v *viper.Viper,
) *BackendWrapper {
	// Rate limits configs

	v = util.GetSubViper(v, "rate-limits")

	// Rate limit configs for this backend

	v = util.GetSubViper(v, backendToWrap.Name())

	v.SetDefault(config.ParamEnabled, false)
	v.SetDefault(config.ParamLimit, config.DefaultLimit)
	v.SetDefault(config.ParamClearAfterDuration, config.DefaultClearAfterDuration)

	limit := v.GetUint64(config.ParamLimit)
	clearAfterDuration := v.GetDuration(config.ParamClearAfterDuration)
	enableRateLimit := v.GetBool(config.ParamEnabled)

	var hyperLogLogByMetricName map[string]*hyperloglog.HyperLogLog

	if enableRateLimit {
		logrus.Infof("Rate limit is enabled for backend: %s - Limit: %d - Clear after duration: %s", backendToWrap.Name(), limit, clearAfterDuration)

		hyperLogLogByMetricName = make(map[string]*hyperloglog.HyperLogLog, 100)
	}

	return &BackendWrapper{
		backend:                 backendToWrap,
		hyperLogLogByMetricName: hyperLogLogByMetricName,
		mutex:                   &sync.RWMutex{},
		limit:                   limit,
		clearAfterDuration:      clearAfterDuration,
		enableRateLimit:         enableRateLimit,
		lastClearTime:           time.Now(),
	}
}
