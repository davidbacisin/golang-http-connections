package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const spaces = " "

var (
	MetricNetstatDuration = must(meter.Float64Histogram("netstat.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration to perform netstat operation"),
	))
	MetricNumOpenConnections = must(meter.Int64Gauge("http.client.netstat_connections"))
)

type netstatLine struct {
	Protocol, Local, Remote, State string
	PID                            int64
}

func netstat(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "netstat", "-ano")
}

func RecordActiveConnectionCount(ctx context.Context) (int64, error) {
	start := time.Now()
	defer func() {
		MetricNetstatDuration.Record(ctx, time.Since(start).Seconds())
	}()

	self := int64(os.Getpid())

	n := netstat(ctx)
	out, err := n.Output()
	if err != nil {
		return 0, errors.Wrap(err, "error running netstat")
	}

	counts := make(map[string]int64, 9) // there are 9 possible connection states
	raw := strings.Split(string(out), "\n")
	for _, line := range raw {
		var field string
		fields := make([]string, 0, 5)
		for i, ok := 0, true; i < 5 && ok; i++ {
			field, line, ok = strings.Cut(strings.TrimLeft(line, spaces), spaces)
			fields = append(fields, field)
		}

		if len(fields) == 5 {
			state := fields[3]
			pid, err := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64)
			if err != nil {
				continue
			}

			if pid == self {
				if _, ok := counts[state]; !ok {
					counts[state] = 0
				}
				counts[state]++

				logger.DebugContext(ctx, "netstat", "conn", netstatLine{
					Protocol: fields[0],
					Local:    fields[1],
					Remote:   fields[2],
					State:    state,
					PID:      pid,
				})
			}
		}
	}

	var total int64
	for state, count := range counts {
		total += count
		MetricNumOpenConnections.Record(ctx, count, metric.WithAttributes(attribute.String("state", state)))
	}

	return total, nil
}
