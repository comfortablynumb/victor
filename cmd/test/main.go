package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/cactus/go-statsd-client/v5/statsd"
	"golang.org/x/exp/rand"
)

func main() {
	addr := "127.0.0.1:8127"
	metricCount := "1000"

	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	if os.Getenv("METRIC_COUNT") != "" {
		metricCount = os.Getenv("METRIC_COUNT")
	}

	metricCountInt, err := strconv.Atoi(metricCount)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(" - Starting test metrics sender against: %s\n", addr)

	config := &statsd.ClientConfig{
		Address: addr,
		Prefix:  "test-client",
	}

	// Now create the client

	statsdClient, err := statsd.NewClientWithConfig(config)

	// and handle any initialization errors
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(" - Statsd client created - Sending metrics...")

	defer func() {
		statsdClient.Close()
	}()

	signalChan := make(chan os.Signal, 1)

	signal.Notify(signalChan, os.Interrupt)

	for {
		select {
		case <-signalChan:
			fmt.Println(" - Stopping test metrics sender")

			return
		default:
			statsdClient.Inc(
				fmt.Sprintf("test.metrics.sender.%d", rand.Intn(metricCountInt)),
				int64(rand.Intn(10000)),
				1.0,
				statsd.Tag{"some_tag", "some_value"},
				statsd.Tag{"some_tag_2", fmt.Sprintf("some_value_%d", rand.Intn(10000))},
				statsd.Tag{"some_tag_3", fmt.Sprintf("some_value_%d", rand.Intn(10000))},
				statsd.Tag{"some_tag_4", fmt.Sprintf("some_value_%d", rand.Intn(10000))},
				statsd.Tag{"some_tag_5", fmt.Sprintf("some_value_%d", rand.Intn(10000))},
				statsd.Tag{"some_tag_6", fmt.Sprintf("some_value_%d", rand.Intn(10000))},
			)

			time.Sleep(1 * time.Millisecond)

		}
	}

}
