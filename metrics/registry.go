package metrics

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	defaultRegistry = newRegistry()

	analysisDurationBuckets = []float64{100, 300, 1_000, 3_000, 10_000, 30_000, 60_000, 180_000, 600_000}
	stepDurationBuckets     = []float64{10, 50, 100, 300, 1_000, 3_000, 10_000, 30_000, 60_000, 180_000}
	sqsDurationBuckets      = []float64{10, 50, 100, 300, 1_000, 3_000, 10_000, 30_000, 60_000, 180_000, 600_000}
)

type registry struct {
	startedAt time.Time

	analysisInflight *gaugeVec
	analysisTotal    *counterVec
	analysisDuration *histogramVec

	stepTotal    *counterVec
	stepDuration *histogramVec

	sqsTotal    *counterVec
	sqsDuration *histogramVec

	sqsPollErrors *counterVec

	notificationPublished *counterVec

	workerEvents *counterVec
}

func newRegistry() *registry {
	return &registry{
		startedAt: time.Now(),

		analysisInflight: newGaugeVec(),
		analysisTotal:    newCounterVec(),
		analysisDuration: newHistogramVec(analysisDurationBuckets),

		stepTotal:    newCounterVec(),
		stepDuration: newHistogramVec(stepDurationBuckets),

		sqsTotal:    newCounterVec(),
		sqsDuration: newHistogramVec(sqsDurationBuckets),

		sqsPollErrors: newCounterVec(),

		notificationPublished: newCounterVec(),

		workerEvents: newCounterVec(),
	}
}

type series struct {
	labels map[string]string
	value  float64
}

type counterVec struct {
	mu     sync.RWMutex
	values map[string]*series
}

func newCounterVec() *counterVec {
	return &counterVec{values: make(map[string]*series)}
}

func (v *counterVec) Inc(labels map[string]string) {
	v.Add(labels, 1)
}

func (v *counterVec) Add(labels map[string]string, delta float64) {
	key := labelKey(labels)

	v.mu.Lock()
	defer v.mu.Unlock()

	entry, ok := v.values[key]
	if !ok {
		entry = &series{labels: cloneLabels(labels)}
		v.values[key] = entry
	}
	entry.value += delta
}

func (v *counterVec) Snapshot() []series {
	v.mu.RLock()
	defer v.mu.RUnlock()

	keys := make([]string, 0, len(v.values))
	for key := range v.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]series, 0, len(keys))
	for _, key := range keys {
		entry := v.values[key]
		items = append(items, series{
			labels: cloneLabels(entry.labels),
			value:  entry.value,
		})
	}
	return items
}

type gaugeVec struct {
	mu     sync.RWMutex
	values map[string]*series
}

func newGaugeVec() *gaugeVec {
	return &gaugeVec{values: make(map[string]*series)}
}

func (v *gaugeVec) Add(labels map[string]string, delta float64) {
	key := labelKey(labels)

	v.mu.Lock()
	defer v.mu.Unlock()

	entry, ok := v.values[key]
	if !ok {
		entry = &series{labels: cloneLabels(labels)}
		v.values[key] = entry
	}
	entry.value += delta
	if entry.value < 0 {
		entry.value = 0
	}
}

func (v *gaugeVec) Snapshot() []series {
	v.mu.RLock()
	defer v.mu.RUnlock()

	keys := make([]string, 0, len(v.values))
	for key := range v.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]series, 0, len(keys))
	for _, key := range keys {
		entry := v.values[key]
		items = append(items, series{
			labels: cloneLabels(entry.labels),
			value:  entry.value,
		})
	}
	return items
}

type histogramSeries struct {
	labels  map[string]string
	buckets []uint64
	count   uint64
	sum     float64
}

type histogramSnapshot struct {
	labels  map[string]string
	buckets []uint64
	count   uint64
	sum     float64
}

type histogramVec struct {
	mu     sync.RWMutex
	bounds []float64
	series map[string]*histogramSeries
}

func newHistogramVec(bounds []float64) *histogramVec {
	cloned := append([]float64(nil), bounds...)
	sort.Float64s(cloned)
	return &histogramVec{
		bounds: cloned,
		series: make(map[string]*histogramSeries),
	}
}

func (v *histogramVec) Observe(labels map[string]string, value float64) {
	key := labelKey(labels)

	v.mu.Lock()
	defer v.mu.Unlock()

	entry, ok := v.series[key]
	if !ok {
		entry = &histogramSeries{
			labels:  cloneLabels(labels),
			buckets: make([]uint64, len(v.bounds)+1),
		}
		v.series[key] = entry
	}

	entry.count++
	entry.sum += value
	for i, bound := range v.bounds {
		if value <= bound {
			entry.buckets[i]++
		}
	}
	entry.buckets[len(v.bounds)]++
}

func (v *histogramVec) Snapshot() ([]float64, []histogramSnapshot) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	keys := make([]string, 0, len(v.series))
	for key := range v.series {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]histogramSnapshot, 0, len(keys))
	for _, key := range keys {
		entry := v.series[key]
		items = append(items, histogramSnapshot{
			labels:  cloneLabels(entry.labels),
			buckets: append([]uint64(nil), entry.buckets...),
			count:   entry.count,
			sum:     entry.sum,
		})
	}

	return append([]float64(nil), v.bounds...), items
}

func RecordAnalysisStarted(analysisType string) {
	normalizedType := normalizeLabelValue(analysisType)
	labels := map[string]string{
		"analysis_type": normalizedType,
		"event_type":    normalizedType,
	}
	defaultRegistry.analysisInflight.Add(labels, 1)
	defaultRegistry.analysisTotal.Inc(map[string]string{
		"analysis_type": normalizedType,
		"event_type":    normalizedType,
		"status":        "started",
	})
}

func RecordAnalysisFinished(analysisType, status string, durationMs int64) {
	normalizedType := normalizeLabelValue(analysisType)
	normalizedStatus := normalizeLabelValue(status)

	defaultRegistry.analysisInflight.Add(map[string]string{
		"analysis_type": normalizedType,
		"event_type":    normalizedType,
	}, -1)
	defaultRegistry.analysisTotal.Inc(map[string]string{
		"analysis_type": normalizedType,
		"event_type":    normalizedType,
		"status":        normalizedStatus,
	})
	defaultRegistry.analysisDuration.Observe(map[string]string{
		"analysis_type": normalizedType,
		"event_type":    normalizedType,
		"status":        normalizedStatus,
	}, float64(durationMs))
}

func RecordStepStarted(stepName string) {
	defaultRegistry.stepTotal.Inc(map[string]string{
		"step_name": normalizeLabelValue(stepName),
		"status":    "started",
	})
}

func RecordStepFinished(stepName, status string, durationMs int64) {
	normalizedStep := normalizeLabelValue(stepName)
	normalizedStatus := normalizeLabelValue(status)

	defaultRegistry.stepTotal.Inc(map[string]string{
		"step_name": normalizedStep,
		"status":    normalizedStatus,
	})
	defaultRegistry.stepDuration.Observe(map[string]string{
		"step_name": normalizedStep,
		"status":    normalizedStatus,
	}, float64(durationMs))
}

func RecordSQSReceived(messageType string) {
	normalizedType := normalizeLabelValue(messageType)
	defaultRegistry.sqsTotal.Inc(map[string]string{
		"message_type": normalizedType,
		"event_type":   normalizedType,
		"status":       "received",
	})
}

func RecordSQSFinished(messageType, status string, durationMs int64) {
	normalizedType := normalizeLabelValue(messageType)
	normalizedStatus := normalizeLabelValue(status)

	defaultRegistry.sqsTotal.Inc(map[string]string{
		"message_type": normalizedType,
		"event_type":   normalizedType,
		"status":       normalizedStatus,
	})
	defaultRegistry.sqsDuration.Observe(map[string]string{
		"message_type": normalizedType,
		"event_type":   normalizedType,
		"status":       normalizedStatus,
	}, float64(durationMs))
}

func RecordSQSPollError() {
	defaultRegistry.sqsPollErrors.Inc(nil)
}

func RecordNotificationPublished(eventType, status string) {
	defaultRegistry.notificationPublished.Inc(map[string]string{
		"event_type": normalizeLabelValue(eventType),
		"status":     normalizeLabelValue(status),
	})
}

func RecordWorkerEvent(eventType string) {
	defaultRegistry.workerEvents.Inc(map[string]string{
		"event_type": normalizeLabelValue(eventType),
	})
}

func renderPrometheus() string {
	var b strings.Builder

	writeGaugeMetric(&b, "git_worker_analysis_inflight", "In-flight analysis jobs.", defaultRegistry.analysisInflight.Snapshot())
	writeCounterMetric(&b, "git_worker_analysis_jobs_total", "Analysis job lifecycle events.", defaultRegistry.analysisTotal.Snapshot())
	writeHistogramMetric(&b, "git_worker_analysis_duration_ms", "Analysis duration in milliseconds.", defaultRegistry.analysisDuration)

	writeCounterMetric(&b, "git_worker_job_steps_total", "Worker step lifecycle events.", defaultRegistry.stepTotal.Snapshot())
	writeHistogramMetric(&b, "git_worker_job_step_duration_ms", "Worker step duration in milliseconds.", defaultRegistry.stepDuration)

	writeCounterMetric(&b, "git_worker_sqs_messages_total", "SQS message lifecycle events.", defaultRegistry.sqsTotal.Snapshot())
	writeHistogramMetric(&b, "git_worker_sqs_message_duration_ms", "SQS message processing duration in milliseconds.", defaultRegistry.sqsDuration)
	writeCounterMetric(&b, "git_worker_sqs_poll_errors_total", "SQS receive polling errors.", defaultRegistry.sqsPollErrors.Snapshot())

	writeCounterMetric(&b, "git_worker_notification_published_total", "Notification publish lifecycle events.", defaultRegistry.notificationPublished.Snapshot())

	writeCounterMetric(&b, "git_worker_worker_events_total", "Worker lifecycle events.", defaultRegistry.workerEvents.Snapshot())
	writeRuntimeMetrics(&b, defaultRegistry.startedAt)

	return b.String()
}

func writeCounterMetric(b *strings.Builder, name, help string, items []series) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " counter\n")
	for _, item := range items {
		writeSample(b, name, item.labels, formatFloat(item.value))
	}
}

func writeGaugeMetric(b *strings.Builder, name, help string, items []series) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " gauge\n")
	for _, item := range items {
		writeSample(b, name, item.labels, formatFloat(item.value))
	}
}

func writeHistogramMetric(b *strings.Builder, name, help string, histogram *histogramVec) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " histogram\n")

	bounds, items := histogram.Snapshot()
	for _, item := range items {
		for i, count := range item.buckets {
			labels := cloneLabels(item.labels)
			if i < len(bounds) {
				labels["le"] = formatFloat(bounds[i])
			} else {
				labels["le"] = "+Inf"
			}
			writeSample(b, name+"_bucket", labels, fmt.Sprintf("%d", count))
		}
		writeSample(b, name+"_sum", item.labels, formatFloat(item.sum))
		writeSample(b, name+"_count", item.labels, fmt.Sprintf("%d", item.count))
	}
}

func writeRuntimeMetrics(b *strings.Builder, startedAt time.Time) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	writeGaugeMetric(b, "go_goroutines", "Number of goroutines that currently exist.", []series{
		{value: float64(runtime.NumGoroutine())},
	})
	writeGaugeMetric(b, "go_memstats_alloc_bytes", "Number of bytes allocated and still in use.", []series{
		{value: float64(mem.Alloc)},
	})
	writeGaugeMetric(b, "go_memstats_heap_alloc_bytes", "Number of heap bytes allocated and still in use.", []series{
		{value: float64(mem.HeapAlloc)},
	})
	writeGaugeMetric(b, "go_memstats_heap_inuse_bytes", "Number of heap bytes in use.", []series{
		{value: float64(mem.HeapInuse)},
	})
	writeCounterMetric(b, "go_gc_cycles_total", "Count of completed GC cycles.", []series{
		{value: float64(mem.NumGC)},
	})
	writeGaugeMetric(b, "process_start_time_seconds", "Start time of the process since unix epoch in seconds.", []series{
		{value: float64(startedAt.Unix())},
	})
	writeGaugeMetric(b, "process_uptime_seconds", "Uptime of the process in seconds.", []series{
		{value: time.Since(startedAt).Seconds()},
	})
}

func writeSample(b *strings.Builder, name string, labels map[string]string, value string) {
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(key)
			b.WriteString(`="`)
			b.WriteString(escapeLabelValue(labels[key]))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
}

func labelKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(labels[key])
		b.WriteByte('|')
	}
	return b.String()
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func normalizeLabelValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
	)
	return replacer.Replace(value)
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%g", value)
}
