package monitor

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"github.com/inecas/kube-health/pkg/status"
)

type Metric struct {
	Labels prom.Labels
	Value  float64
}

type metricSet struct {
	mtx     sync.RWMutex
	metrics []Metric
	name    string
	help    string
}

// MetricSet is an expasion of prom.Collector interface that allows batch
// updates of metrics. Useful when processing a set of metrics that are later
// exposed to Prometheus via different metric.
type MetricSet interface {
	prom.Collector
	Update(metrics []Metric)
}

func NewMetricSet(name, help string) *metricSet {
	return &metricSet{name: name, help: help}
}

func (m *metricSet) Update(metrics []Metric) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.metrics = metrics
}

func (m *metricSet) Reset() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.metrics = nil
}

func (m *metricSet) Collect(ch chan<- prom.Metric) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	for _, metric := range m.metrics {
		labels := make([]string, 0, len(metric.Labels))
		values := make([]string, 0, len(metric.Labels))
		for k, v := range metric.Labels {
			if k == "__name__" {
				continue
			}

			labels = append(labels, k)
			values = append(values, v)
		}
		desc := prom.NewDesc(m.name, m.help, labels, nil)
		ch <- prom.MustNewConstMetric(desc, prom.GaugeValue, metric.Value, values...)
	}
}

func (m *metricSet) Describe(ch chan<- *prom.Desc) {
	ch <- prom.NewDesc(m.name, m.help, nil, nil)
}

// Server is the interface for serving the metrics.
type Server interface {
	// Handle registers a handler for the given pattern, similar to http.Handle.
	Handle(pattern string, handler http.Handler)

	// Start starts the server and blocks until the server is stopped.
	Start(ctx context.Context) error
}

type SimpleServer struct {
	host string
	port int
	mux  *http.ServeMux
}

func NewSimpleServer(host string, port int) *SimpleServer {
	return &SimpleServer{
		host: host,
		port: port,
		mux:  http.NewServeMux(),
	}
}

func (s *SimpleServer) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

func (s *SimpleServer) Start(ctx context.Context) error {
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.host, s.port),
		Handler: s.mux,
	}
	var err error
	stop := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			err = server.Shutdown(context.Background())
			close(stop)
		case <-stop:
			// Stopped outside of the context.
		}
	}()

	go func() {
		err = server.ListenAndServe()
		close(stop)
	}()

	<-stop
	return err
}

type Exporter struct {
	updatesChan <-chan TargetsStatusUpdate
	server      Server
	ms          MetricSet
}

func NewExporter(updatesChan <-chan TargetsStatusUpdate, server Server,
	metricName, metricDescription string) *Exporter {
	return &Exporter{
		updatesChan: updatesChan,
		server:      server,
		ms:          NewMetricSet(metricName, metricDescription),
	}
}

func (e *Exporter) Start(ctx context.Context) error {
	go e.digestUpdates()
	e.registerMetrics()

	return e.startServer(ctx)
}

func (e *Exporter) digestUpdates() {
	for update := range e.updatesChan {
		var metrics []Metric
		for _, part := range update.Statuses {
			klog.V(2).InfoS("Received update", "objects", len(part.Statuses))
			for _, status := range part.Statuses {
				metric := statusToMetric(part.Target.Category, status)
				klog.V(3).InfoS("Converted status to metric", "metric", metric)
				metrics = append(metrics, metric)
			}
		}
		e.ms.Update(metrics)
	}
}

func (e *Exporter) registerMetrics() {
	reg := prom.NewRegistry()
	reg.MustRegister(e.ms)

	e.server.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
}

func (e *Exporter) startServer(ctx context.Context) error {
	return e.server.Start(ctx)
}

func statusToMetric(category string, objStatus status.ObjectStatus) Metric {
	status := objStatus.Status()
	// We add "progressing" as extra result + expose the original value as result_details.
	statusStr := strings.ToLower(status.Result.String())
	if status.Progressing {
		statusStr = "progressing"
	}

	return Metric{
		Labels: prom.Labels{
			"kind":      objStatus.Object.Kind,
			"name":      objStatus.Object.Name,
			"namespace": objStatus.Object.Namespace,
			"status":    statusStr,
			"result":    strings.ToLower(status.Result.String()),
			"category":  category,
		},
		Value: resultToValue(status),
	}
}

// resultToValue converts status.Result to a float64 value.
// The value can be used to represent the status in Prometheus metrics
func resultToValue(s status.Status) float64 {
	switch s.Result {
	case status.Ok:
		return 0
	case status.Warning:
		return 1
	case status.Error:
		return 2
	case status.Unknown:
		return -1
	default:
		klog.V(1).InfoS("Unknown status result when preparing metric value. Using 2 as default", "result", s.Result)
		return 2
	}
}
