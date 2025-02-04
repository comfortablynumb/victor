package wrapper

import (
	"context"
	"sync"

	"github.com/atlassian/gostatsd"
	"github.com/comfortablynumb/victor/internal/hyperloglog"
)

// Structs

type BackendWrapper struct {
	backend                 gostatsd.Backend
	hyperLogLogByMetricName map[string]*hyperloglog.HyperLogLog
	mutex                   *sync.RWMutex
}

func (b *BackendWrapper) SendMetricsAsync(ctx context.Context, metricMap *gostatsd.MetricMap, callback gostatsd.SendCallback) {
	metrics := metricMap.AsMetrics()

	// Check if we need to drop some metrics

	for _, metric := range metrics {
		if _, valid := b.Estimate(metric.Name, metric.Tags.SortedString(), 1000); !valid {
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

	b.backend.SendMetricsAsync(ctx, metricMap, callback)
}

func (b *BackendWrapper) SendEvent(ctx context.Context, event *gostatsd.Event) error {
	return b.backend.SendEvent(ctx, event)
}

func (b *BackendWrapper) Name() string {
	return b.backend.Name()
}

func (b *BackendWrapper) AddMetricTags(metricName string, tags string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.hyperLogLogByMetricName[metricName].Insert(tags)
}

func (b *BackendWrapper) Estimate(metricName string, tags string, limit uint64) (uint64, bool) {
	b.mutex.RLock()

	val, found := b.hyperLogLogByMetricName[metricName]

	b.mutex.RUnlock()

	if !found {
		b.AddNewMetric(metricName, tags)

		return 0, true
	}

	res := val.Estimate()

	if res < limit {
		val.Insert(tags)

		return res, true
	}

	return res, false
}

func (b *BackendWrapper) AddNewMetric(metricName string, tags string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	hyperLogLog := hyperloglog.NewHyperLogLog(tags)

	b.hyperLogLogByMetricName[metricName] = hyperLogLog
}

// Static functions

func NewBackendWrapper(backendToWrap gostatsd.Backend) *BackendWrapper {
	hyperLogLogByMetricName := make(map[string]*hyperloglog.HyperLogLog, 100)

	return &BackendWrapper{
		backend:                 backendToWrap,
		hyperLogLogByMetricName: hyperLogLogByMetricName,
		mutex:                   &sync.RWMutex{},
	}
}
