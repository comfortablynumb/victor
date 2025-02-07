package backend

import (
	"context"
	"sync"
	"sync/atomic"
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
	lastClearTime int64

	backend                 gostatsd.Backend
	backendRunner           gostatsd.Runner
	backendMetricsRunner    gostatsd.MetricsRunner
	hyperLogLogByMetricName map[string]*hyperloglog.HyperLogLog
	mutex                   *sync.RWMutex
	limit                   uint64
	clearAfterDuration      time.Duration
	limitByTag              map[string]uint64
}

func (b *RateLimitedBackend) SendMetricsAsync(ctx context.Context, metricMap *gostatsd.MetricMap, callback gostatsd.SendCallback) {
	if atomic.LoadInt64(&b.lastClearTime) < time.Now().Add(-b.clearAfterDuration).Unix() {
		b.clearHyperLogLogs()
	}

	b.rateLimit(metricMap)

	b.backend.SendMetricsAsync(ctx, metricMap, callback)
}

func (b *RateLimitedBackend) SendEvent(ctx context.Context, event *gostatsd.Event) error {
	return b.backend.SendEvent(ctx, event)
}

func (b *RateLimitedBackend) Run(ctx context.Context) {
	if b.backendRunner != nil {
		b.backendRunner.Run(ctx)
	}
}

func (b *RateLimitedBackend) RunMetricsContext(ctx context.Context) {
	if b.backendMetricsRunner != nil {
		b.backendMetricsRunner.RunMetricsContext(ctx)
	}
}

func (b *RateLimitedBackend) Name() string {
	return b.backend.Name()
}

func (b *RateLimitedBackend) clearHyperLogLogs() {
	atomic.StoreInt64(&b.lastClearTime, time.Now().Unix())

	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.hyperLogLogByMetricName = make(map[string]*hyperloglog.HyperLogLog)
}

func (b *RateLimitedBackend) rateLimit(metricMap *gostatsd.MetricMap) {
	// Check if we need to drop some metrics

	// :: Counters

	metricMap.Counters.Each(func(metricName string, tagsKey string, c gostatsd.Counter) {
		limit := b.limit

		if _, ok := b.limitByTag[metricName]; ok {
			limit = b.limitByTag[metricName]
		}

		if _, valid := b.estimate(metricName, tagsKey, limit); !valid {
			metricMap.Counters.Delete(metricName)
		}
	})

	// :: Gauges

	metricMap.Gauges.Each(func(metricName string, tagsKey string, g gostatsd.Gauge) {
		limit := b.limit

		if _, ok := b.limitByTag[metricName]; ok {
			limit = b.limitByTag[metricName]
		}

		if _, valid := b.estimate(metricName, tagsKey, limit); !valid {
			metricMap.Gauges.Delete(metricName)
		}
	})

	// :: Timers

	metricMap.Timers.Each(func(metricName string, tagsKey string, t gostatsd.Timer) {
		limit := b.limit

		if _, ok := b.limitByTag[metricName]; ok {
			limit = b.limitByTag[metricName]
		}

		if _, valid := b.estimate(metricName, tagsKey, limit); !valid {
			metricMap.Timers.Delete(metricName)
		}
	})
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

	v = util.GetSubViper(v, config.ParamRateLimit)

	v.SetDefault(config.ParamDefaultLimit, config.DefaultLimit)
	v.SetDefault(config.ParamClearAfterDuration, config.DefaultClearAfterDuration)
	v.SetDefault(config.ParamLimitByTag, make(map[string]uint64))

	limit := v.GetUint64(config.ParamDefaultLimit)
	clearAfterDuration := v.GetDuration(config.ParamClearAfterDuration)
	limitByTag, err := util.ConvertMap[string, uint64](v.GetStringMap(config.ParamLimitByTag))

	if err != nil {
		logrus.WithError(err).Fatal("Failed to convert limit-by-tag to map[string]uint64")
	}

	hyperLogLogByMetricName := make(map[string]*hyperloglog.HyperLogLog, 100)

	var backendRunner gostatsd.Runner
	var backendMetricsRunner gostatsd.MetricsRunner

	if castedBackendRunner, ok := backendToRateLimit.(gostatsd.Runner); ok {
		backendRunner = castedBackendRunner
	}

	if castedBackendMetricsRunner, ok := backendToRateLimit.(gostatsd.MetricsRunner); ok {
		backendMetricsRunner = castedBackendMetricsRunner
	}

	logrus.WithField("backend", backendToRateLimit.Name()).
		WithField(config.ParamDefaultLimit, limit).
		WithField(config.ParamClearAfterDuration, clearAfterDuration).
		Info("Rate limit is enabled for backend")

	return &RateLimitedBackend{
		backend:                 backendToRateLimit,
		backendRunner:           backendRunner,
		backendMetricsRunner:    backendMetricsRunner,
		hyperLogLogByMetricName: hyperLogLogByMetricName,
		mutex:                   &sync.RWMutex{},
		limit:                   limit,
		clearAfterDuration:      clearAfterDuration,
		limitByTag:              limitByTag,
		lastClearTime:           time.Now().Unix(),
	}
}
