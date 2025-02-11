# Victor - A statsd server that can rate limit metrics by cardinality

---

**:construction_worker: IMPORTANT NOTE: :construction_worker: This is a work in progress. Stay tuned!**

---

## Overview

Victor is a statsd server (using the one from [Attlasian](https://github.com/atlassian/gostatsd)) that can rate limit metrics based on their cardinality (unique combinations of metric name and tags). By limiting high-cardinality metrics, Victor can dramatically reduce costs in your metrics infrastructure. High-cardinality metrics, if left unchecked, can lead to exponential growth in storage requirements and processing overhead.

Key features:

- Acts as a statsd proxy server that can forward metrics to other statsd-compatible backends
- Rate limits metrics based on unique tag combinations using HyperLogLog for cardinality estimation
- Significant cost savings by preventing cardinality explosions in your metrics backend
- Configurable rate limits using a default limit and, optionally, a limit per metric name
- Automatic clearing of cardinality tracking after a configurable duration. This is useful to control costs in SaaS that measure costs by metric + tag cardinality in a fixed time window (e.g. 1 hour)
- Support for multiple backend types

## How it works

Victor uses a HyperLogLog algorithm to estimate the cardinality of metric tags. This allows it to accurately count the number of unique tag combinations for each metric name, and thus apply rate limits accordingly. Also, this allows us to use a single HyperLogLog counter for each metric name, which reduces memory usage.

You can use Victor either as a standalone server or as a proxy to send metrics to other statsd-compatible backends. Also, you can use it as a standalone service or as a sidecar for each of your applications. The decision depends in the amount of metrics you expect to receive and the resources available.

## Quick Start

1. Create a configuration file. For example, `config/config.yaml`:

```yaml
metrics-addr: ":8125"
backends:
  - statsdaemon
statsdaemon:
  address: "your-statsd-server:8125"
  rate-limit:
    enabled: true
    default-limit: 1000
    clear-after-duration: 1h
```

2. Run Victor:

```bash
docker run -ti -p "8125:8125/udp" -v ./config/config.yaml:/app/config/config.yaml ironedge/victor:latest
```

## Docker Image

You can also use our Docker image. For example, using Docker Compose:

```yaml
services:
  victor:
    image: ironedge/victor:latest
    restart: unless-stopped
    ports:
      - "8125:8125/udp"
    volumes:
      - "your-config.yaml:/app/config/config.yaml"
```

## Simulate Metrics Locally

You can use the following command to simulate metrics locally against Victor (or any other statsd-compatible server):

```bash
go run cmd/test/main.go 127.0.0.1:8125
```

Being the first argument the address of the statsd server you want to send metrics to.




