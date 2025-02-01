package main

import (
	"context"
	_ "expvar"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/time/rate"

	"github.com/atlassian/gostatsd"
	"github.com/atlassian/gostatsd/pkg/backends"
	"github.com/atlassian/gostatsd/pkg/cachedinstances"
	"github.com/atlassian/gostatsd/pkg/cachedinstances/cloudprovider"
	"github.com/atlassian/gostatsd/pkg/cloudproviders"
	"github.com/atlassian/gostatsd/pkg/statsd"
	"github.com/atlassian/gostatsd/pkg/transport"
)

const (
	// ParamVerbose enables verbose logging.
	ParamVerbose = "verbose"
	// ParamProfile enables profiler endpoint on the specified address and port.
	ParamProfile = "profile"
	// ParamJSON makes logger log in JSON format.
	ParamJSON = "json"
	// ParamConfigPath provides file with configuration.
	ParamConfigPath = "config-path"
	// ParamVersion makes program output its version.
	ParamVersion = "version"

	// @TODO - Setup these constants

	Version   = "0.0.1"
	GitCommit = "1234567890"
	BuildDate = "2021-01-01"
	EnvPrefix = "STATS"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	v, version, err := setupConfiguration()
	if err != nil {
		if err == pflag.ErrHelp {
			return
		}
		logrus.Fatalf("Error while parsing configuration: %v", err)
	}
	if version {
		fmt.Printf("Version: %s - Commit: %s - Date: %s\n", Version, GitCommit, BuildDate)
		return
	}

	if err := run(v); err != nil {
		logrus.Fatalf("%v", err)
	}
}

func run(v *viper.Viper) error {
	logrus.Info("Starting server")
	s, err := constructServer(v)
	if err != nil {
		return err
	}

	profileAddr := v.GetString(ParamProfile)
	if profileAddr != "" {
		go func() {
			logrus.Errorf("Profiler server failed: %v", http.ListenAndServe(profileAddr, nil))
		}()
	}

	ctx, cancelFunc := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelFunc()

	go func() {
		for {
			select {
			case <-time.After(5 * time.Second):
				PrintMemUsage()
			}
		}
	}()

	logrus.Infof("Server started on %s", v.GetString(gostatsd.ParamMetricsAddr))

	if err := s.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("server error: %v", err)
	}
	return nil
}

func constructServer(v *viper.Viper) (*statsd.Server, error) {
	var runnables []gostatsd.Runnable
	// Logger
	logger := logrus.StandardLogger()

	// HTTP client pool
	pool := transport.NewTransportPool(logger, v)

	// Cached instances
	var cachedInstances gostatsd.CachedInstances
	cloudProviderName := v.GetString(gostatsd.ParamCloudProvider)
	if cloudProviderName == "" {
		logger.Info("No cloud provider specified")
	} else {
		var err error
		// See if requested cloud provider is a native CachedInstances implementation
		cachedInstances, err = cachedinstances.Get(logger, cloudProviderName, v, Version)
		switch err {
		case nil:
		case cachedinstances.ErrUnknownProvider:
			// See if requested cloud provider is a CloudProvider implementation
			cloudProvider, err := cloudproviders.Get(logger, cloudProviderName, v, Version)
			if err != nil {
				return nil, err
			}
			runnables = gostatsd.MaybeAppendRunnable(runnables, cloudProvider)
			cachedInstances = newCachedInstancesFromViper(logger, cloudProvider, v)
		default:
			return nil, err
		}
		runnables = gostatsd.MaybeAppendRunnable(runnables, cachedInstances)
	}
	// Backends
	backendNames := v.GetStringSlice(gostatsd.ParamBackends)
	backendsList := make([]gostatsd.Backend, 0, len(backendNames))
	for _, backendName := range backendNames {
		logrus.Infof("Initializing backend: %s", backendName)

		backend, errBackend := backends.InitBackend(backendName, v, logger, pool)
		if errBackend != nil {
			return nil, errBackend
		}
		backendsList = append(backendsList, NewBackendWrapper(backend))
		runnables = gostatsd.MaybeAppendRunnable(runnables, backend)
	}
	// Percentiles
	pt, err := getPercentiles(v.GetStringSlice(gostatsd.ParamPercentThreshold))
	if err != nil {
		return nil, err
	}

	// Set defaults for expiry from the main expiry setting
	v.SetDefault(gostatsd.ParamExpiryIntervalCounter, v.GetDuration(gostatsd.ParamExpiryInterval))
	v.SetDefault(gostatsd.ParamExpiryIntervalGauge, v.GetDuration(gostatsd.ParamExpiryInterval))
	v.SetDefault(gostatsd.ParamExpiryIntervalSet, v.GetDuration(gostatsd.ParamExpiryInterval))
	v.SetDefault(gostatsd.ParamExpiryIntervalTimer, v.GetDuration(gostatsd.ParamExpiryInterval))

	// Create server
	return &statsd.Server{
		Runnables:             runnables,
		Backends:              backendsList,
		CachedInstances:       cachedInstances,
		InternalTags:          v.GetStringSlice(gostatsd.ParamInternalTags),
		InternalNamespace:     v.GetString(gostatsd.ParamInternalNamespace),
		DefaultTags:           v.GetStringSlice(gostatsd.ParamDefaultTags),
		Hostname:              gostatsd.Source(v.GetString(gostatsd.ParamHostname)),
		ExpiryIntervalCounter: v.GetDuration(gostatsd.ParamExpiryIntervalCounter),
		ExpiryIntervalGauge:   v.GetDuration(gostatsd.ParamExpiryIntervalGauge),
		ExpiryIntervalSet:     v.GetDuration(gostatsd.ParamExpiryIntervalSet),
		ExpiryIntervalTimer:   v.GetDuration(gostatsd.ParamExpiryIntervalTimer),
		FlushInterval:         v.GetDuration(gostatsd.ParamFlushInterval),
		FlushOffset:           v.GetDuration(gostatsd.ParamFlushOffset),
		FlushAligned:          v.GetBool(gostatsd.ParamFlushAligned),
		IgnoreHost:            v.GetBool(gostatsd.ParamIgnoreHost),
		MaxReaders:            v.GetInt(gostatsd.ParamMaxReaders),
		MaxParsers:            v.GetInt(gostatsd.ParamMaxParsers),
		MaxWorkers:            v.GetInt(gostatsd.ParamMaxWorkers),
		MaxQueueSize:          v.GetInt(gostatsd.ParamMaxQueueSize),
		MaxConcurrentEvents:   v.GetInt(gostatsd.ParamMaxConcurrentEvents),
		EstimatedTags:         v.GetInt(gostatsd.ParamEstimatedTags),
		MetricsAddr:           v.GetString(gostatsd.ParamMetricsAddr),
		Namespace:             v.GetString(gostatsd.ParamNamespace),
		StatserType:           v.GetString(gostatsd.ParamStatserType),
		PercentThreshold:      pt,
		HeartbeatEnabled:      v.GetBool(gostatsd.ParamHeartbeatEnabled),
		ReceiveBatchSize:      v.GetInt(gostatsd.ParamReceiveBatchSize),
		ConnPerReader:         v.GetBool(gostatsd.ParamConnPerReader),
		ServerMode:            v.GetString(gostatsd.ParamServerMode),
		LogRawMetric:          v.GetBool(gostatsd.ParamLogRawMetric),
		HeartbeatTags: gostatsd.Tags{
			fmt.Sprintf("version:%s", Version),
			fmt.Sprintf("commit:%s", GitCommit),
		},
		DisabledSubTypes:          gostatsd.DisabledSubMetrics(v),
		BadLineRateLimitPerSecond: rate.Limit(v.GetFloat64(gostatsd.ParamBadLinesPerMinute) / 60.0),
		HistogramLimit:            v.GetUint32(gostatsd.ParamTimerHistogramLimit),
		DisableInternalEvents:     v.GetBool(gostatsd.ParamDisableInternalEvents),
		Viper:                     v,
		TransportPool:             pool,
	}, nil
}

func getPercentiles(s []string) ([]float64, error) {
	percentThresholds := make([]float64, len(s))
	for i, sPercentThreshold := range s {
		pt, err := strconv.ParseFloat(sPercentThreshold, 64)
		if err != nil {
			return nil, err
		}
		percentThresholds[i] = pt
	}
	return percentThresholds, nil
}

func setupConfiguration() (*viper.Viper, bool, error) {
	v := viper.New()
	defer setupLogger(v) // Apply logging configuration in case of early exit
	InitViper(v, "")

	var version bool

	cmd := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)

	cmd.BoolVar(&version, ParamVersion, false, "Print the version and exit")
	cmd.Bool(ParamVerbose, false, "Verbose")
	cmd.Bool(ParamJSON, false, "Log in JSON format")
	cmd.String(ParamProfile, "", "Enable profiler endpoint on the specified address and port")
	cmd.String(ParamConfigPath, "", "Path to the configuration file")

	gostatsd.AddFlags(cmd)

	cmd.VisitAll(func(flag *pflag.Flag) {
		if err := v.BindPFlag(flag.Name, flag); err != nil {
			panic(err) // Should never happen
		}
	})

	if err := cmd.Parse(os.Args[1:]); err != nil {
		return nil, false, err
	}

	configPath := v.GetString(ParamConfigPath)
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, false, err
		}
	}

	return v, version, nil
}

func setupLogger(v *viper.Viper) {
	if v.GetBool(ParamVerbose) {
		logrus.SetLevel(logrus.DebugLevel)
	}
	if v.GetBool(ParamJSON) {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}
}

// newCachedInstancesFromViper initialises a new cached instances.
func newCachedInstancesFromViper(logger logrus.FieldLogger, cloudProvider gostatsd.CloudProvider, v *viper.Viper) gostatsd.CachedInstances {
	// Set the defaults in Viper based on the cloud provider values before we manipulate things
	v.SetDefault(gostatsd.ParamCacheRefreshPeriod, gostatsd.DefaultCacheRefreshPeriod)
	v.SetDefault(gostatsd.ParamCacheEvictAfterIdlePeriod, gostatsd.DefaultCacheEvictAfterIdlePeriod)
	v.SetDefault(gostatsd.ParamCacheTTL, gostatsd.DefaultCacheTTL)
	v.SetDefault(gostatsd.ParamCacheNegativeTTL, gostatsd.DefaultCacheNegativeTTL)
	v.SetDefault(gostatsd.ParamMaxCloudRequests, gostatsd.DefaultMaxCloudRequests)
	v.SetDefault(gostatsd.ParamBurstCloudRequests, gostatsd.DefaultBurstCloudRequests)

	// Set the used values based on the defaults merged with any overrides
	cacheOptions := gostatsd.CacheOptions{
		CacheRefreshPeriod:        v.GetDuration(gostatsd.ParamCacheRefreshPeriod),
		CacheEvictAfterIdlePeriod: v.GetDuration(gostatsd.ParamCacheEvictAfterIdlePeriod),
		CacheTTL:                  v.GetDuration(gostatsd.ParamCacheTTL),
		CacheNegativeTTL:          v.GetDuration(gostatsd.ParamCacheNegativeTTL),
	}
	limiter := rate.NewLimiter(rate.Limit(v.GetInt(gostatsd.ParamMaxCloudRequests)), v.GetInt(gostatsd.ParamBurstCloudRequests))
	return cloudprovider.NewCachedCloudProvider(logger, limiter, cloudProvider, cacheOptions)
}

// InitViper sets up env var handling for a viper. This must be run on every created sub viper as these settings
// are not persisted to nested viper instances.
func InitViper(v *viper.Viper, subViperName string) {
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.SetEnvPrefix(EnvPrefix)
	v.SetTypeByDefaultValue(true)
	v.AutomaticEnv()
}

type HyperLogLog struct {
	sketch *hyperloglog.Sketch
	mutex  *sync.RWMutex
}

func (h *HyperLogLog) Insert(tags string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.sketch.Insert([]byte(tags))
}

func (h *HyperLogLog) Estimate() uint64 {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.sketch.Estimate()
}

type BackendWrapper struct {
	backend                 gostatsd.Backend
	hyperLogLogByMetricName map[string]*HyperLogLog
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

	hyperLogLog := NewHyperLogLog(tags)

	b.hyperLogLogByMetricName[metricName] = hyperLogLog
}

func NewBackendWrapper(backend gostatsd.Backend) *BackendWrapper {
	hyperLogLogByMetricName := make(map[string]*HyperLogLog, 100)

	return &BackendWrapper{
		backend:                 backend,
		hyperLogLogByMetricName: hyperLogLogByMetricName,
		mutex:                   &sync.RWMutex{},
	}
}

func NewHyperLogLog(tags string) *HyperLogLog {
	sketch := hyperloglog.New14()

	sketch.Insert([]byte(tags))

	return &HyperLogLog{
		sketch: sketch,
		mutex:  &sync.RWMutex{},
	}
}

// PrintMemUsage outputs the current, total and OS memory being used. As well as the number
// of garage collection cycles completed.
func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	logrus.Infof("Alloc = %v MiB - TotalAlloc = %v MiB - Sys = %v MiB - NumGC = %v", bToMb(m.Alloc), bToMb(m.TotalAlloc), bToMb(m.Sys), m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
