package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type cloudCostStatusResponse struct {
	Code int `json:"code"`
	Data []struct {
		Key              string `json:"key"`
		Source           string `json:"source"`
		Provider         string `json:"provider"`
		Active           bool   `json:"active"`
		Valid            bool   `json:"valid"`
		LastRun          string `json:"lastRun"`
		NextRun          string `json:"nextRun"`
		ConnectionStatus string `json:"connectionStatus"`
	} `json:"data"`
}

type cloudCostTotalsResponse struct {
	Code int `json:"code"`
	Data struct {
		Combined struct {
			Name              string  `json:"name"`
			KubernetesPercent float64 `json:"kubernetesPercent"`
			Cost              float64 `json:"cost"`
		} `json:"combined"`
	} `json:"data"`
}

type cloudCostTableResponse struct {
	Code int `json:"code"`
	Data []struct {
		Name              string  `json:"name"`
		KubernetesPercent float64 `json:"kubernetesPercent"`
		Cost              float64 `json:"cost"`
	} `json:"data"`
}

type cloudCostGraphResponse struct {
	Code int `json:"code"`
	Data []struct {
		Start string `json:"start"`
		End   string `json:"end"`
		Items []struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		} `json:"items"`
	} `json:"data"`
}

type config struct {
	OpenCostURL     string
	Window          string
	CostMetric      string
	CostMetrics     []string
	Aggregates      []string
	RefreshInterval time.Duration
	HTTPTimeout     time.Duration
	ListenAddr      string
}

func mustConfig() config {
	get := func(k string) string { return os.Getenv(k) }

	cfg := config{
		OpenCostURL: get("OPENCOST_URL"),
		Window:      get("WINDOW"),
		CostMetric:  get("COST_METRIC"),
		ListenAddr:  get("LISTEN_ADDR"),
	}

	if cfg.OpenCostURL == "" {
		log.Fatal("OPENCOST_URL is required")
	}
	if cfg.Window == "" {
		log.Fatal("WINDOW is required")
	}
	if cfg.CostMetric == "" {
		log.Fatal("COST_METRIC is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	// Optional lists:
	// - COST_METRICS: comma-separated list of costMetric values to scrape (e.g. "amortizedNetCost,netCost,listCost")
	// - AGGREGATES: comma-separated list of aggregate properties to scrape (e.g. "service,category,accountID,provider,regionID,availabilityZone")
	// If not set, default to the existing single COST_METRIC and "service,category".
	if s := get("COST_METRICS"); s != "" {
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			log.Fatal("COST_METRICS is set but empty")
		}
		cfg.CostMetrics = out
	} else {
		cfg.CostMetrics = []string{cfg.CostMetric}
	}

	if s := get("AGGREGATES"); s != "" {
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			log.Fatal("AGGREGATES is set but empty")
		}
		cfg.Aggregates = out
	} else {
		cfg.Aggregates = []string{"service", "category"}
	}

	if s := get("REFRESH_INTERVAL"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			log.Fatalf("invalid REFRESH_INTERVAL: %v", err)
		}
		cfg.RefreshInterval = d
	} else {
		cfg.RefreshInterval = 5 * time.Minute
	}

	if s := get("HTTP_TIMEOUT"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			log.Fatalf("invalid HTTP_TIMEOUT: %v", err)
		}
		cfg.HTTPTimeout = d
	} else {
		cfg.HTTPTimeout = 30 * time.Second
	}

	return cfg
}

type exporter struct {
	cfg config
	cli *http.Client

	scrapeSuccess      prometheus.Gauge
	scrapeDuration     prometheus.Gauge
	cloudIntegrationUp *prometheus.GaugeVec
	cloudIntegrationTS *prometheus.GaugeVec
	cloudTotalCost     *prometheus.GaugeVec
	cloudAggCost       *prometheus.GaugeVec
	cloudAggK8sPct     *prometheus.GaugeVec
	cloudServiceCost   *prometheus.GaugeVec
	cloudServiceK8sPct *prometheus.GaugeVec
	cloudCategoryCost  *prometheus.GaugeVec

	// Daily metrics need explicit sample timestamps (derived from the day) so time-based alerting (offset) works.
	daily *dailyCollector
}

func newExporter(cfg config) *exporter {
	daily := newDailyCollector()
	e := &exporter{
		cfg: cfg,
		cli: &http.Client{Timeout: cfg.HTTPTimeout},
		scrapeSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_exporter_scrape_success",
			Help: "1 if the last scrape from OpenCost succeeded; 0 otherwise.",
		}),
		scrapeDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_exporter_scrape_duration_seconds",
			Help: "Duration of the last scrape from OpenCost in seconds.",
		}),
		cloudIntegrationUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_integration_up",
			Help: "1 if the configured Cloud Cost integration is active+valid; 0 otherwise.",
		}, []string{"key", "provider", "source", "connection_status"}),
		cloudIntegrationTS: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_integration_run_timestamp",
			Help: "Timestamps (unix seconds) for cloud cost integration runs.",
		}, []string{"key", "provider", "which"}),
		cloudTotalCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_total_cost",
			Help: "Total cloud cost over the configured window.",
		}, []string{"window", "cost_metric"}),
		cloudAggCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_aggregate_cost",
			Help: "Cloud cost by aggregate property over the configured window.",
		}, []string{"aggregate", "name", "window", "cost_metric"}),
		cloudAggK8sPct: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_aggregate_kubernetes_percent",
			Help: "KubernetesPercent by aggregate property over the configured window.",
		}, []string{"aggregate", "name", "window", "cost_metric"}),
		cloudServiceCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_service_cost",
			Help: "Cloud cost by service over the configured window.",
		}, []string{"service", "window", "cost_metric"}),
		cloudServiceK8sPct: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_service_kubernetes_percent",
			Help: "KubernetesPercent by service over the configured window.",
		}, []string{"service", "window", "cost_metric"}),
		cloudCategoryCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "opencost_cloudcost_category_cost",
			Help: "Cloud cost by category (resource type) over the configured window.",
		}, []string{"category", "window", "cost_metric"}),
		daily: daily,
	}

	prometheus.MustRegister(e.scrapeSuccess)
	prometheus.MustRegister(e.scrapeDuration)
	prometheus.MustRegister(e.cloudIntegrationUp)
	prometheus.MustRegister(e.cloudIntegrationTS)
	prometheus.MustRegister(e.cloudTotalCost)
	prometheus.MustRegister(e.cloudAggCost)
	prometheus.MustRegister(e.cloudAggK8sPct)
	prometheus.MustRegister(e.cloudServiceCost)
	prometheus.MustRegister(e.cloudServiceK8sPct)
	prometheus.MustRegister(e.cloudCategoryCost)
	prometheus.MustRegister(e.daily)

	return e
}

func (e *exporter) statusURL() string {
	return fmt.Sprintf("%s/cloudCost/status", e.cfg.OpenCostURL)
}

func (e *exporter) totalsURL(costMetric string) string {
	return fmt.Sprintf("%s/cloudCost/view/totals?window=%s&aggregate=service&accumulate=day&costMetric=%s", e.cfg.OpenCostURL, e.cfg.Window, costMetric)
}

func (e *exporter) tableURL(aggregate, costMetric string) string {
	// "item" (aka no aggregate param) returns fully-qualified names like:
	// invoiceEntityID/accountID/provider/providerID/category/service
	// which lets you break down by resource/providerID.
	if aggregate == "item" {
		return fmt.Sprintf("%s/cloudCost/view/table?window=%s&accumulate=day&costMetric=%s&sortBy=cost&sortByOrder=desc&limit=500", e.cfg.OpenCostURL, e.cfg.Window, costMetric)
	}
	return fmt.Sprintf("%s/cloudCost/view/table?window=%s&aggregate=%s&accumulate=day&costMetric=%s&sortBy=cost&sortByOrder=desc&limit=500", e.cfg.OpenCostURL, e.cfg.Window, aggregate, costMetric)
}

func (e *exporter) graphURL(aggregate, costMetric string) string {
	if aggregate == "item" {
		return fmt.Sprintf("%s/cloudCost/view/graph?window=%s&accumulate=day&costMetric=%s", e.cfg.OpenCostURL, e.cfg.Window, costMetric)
	}
	return fmt.Sprintf("%s/cloudCost/view/graph?window=%s&aggregate=%s&accumulate=day&costMetric=%s", e.cfg.OpenCostURL, e.cfg.Window, aggregate, costMetric)
}

func (e *exporter) scrape(ctx context.Context) error {
	start := time.Now()
	defer func() {
		e.scrapeDuration.Set(time.Since(start).Seconds())
	}()

	// Reset only the series for this window/metric by wiping all and rebuilding.
	// This exporter is intended to run with a single configured window, but may scrape multiple aggregates/cost metrics.
	e.cloudIntegrationUp.Reset()
	e.cloudIntegrationTS.Reset()
	e.cloudAggCost.Reset()
	e.cloudAggK8sPct.Reset()
	e.cloudServiceCost.Reset()
	e.cloudServiceK8sPct.Reset()
	e.cloudCategoryCost.Reset()
	e.daily.Reset()

	status, err := e.fetchStatus(ctx)
	if err != nil {
		e.scrapeSuccess.Set(0)
		return err
	}
	e.applyStatus(status)

	for _, costMetric := range e.cfg.CostMetrics {
		totals, err := e.fetchTotals(ctx, costMetric)
		if err != nil {
			e.scrapeSuccess.Set(0)
			return err
		}
		e.cloudTotalCost.WithLabelValues(e.cfg.Window, costMetric).Set(totals)

		// Always scrape daily totals from service graph (used by dashboards, and gives a consistent total).
		dailyService, err := e.fetchGraph(ctx, "service", costMetric)
		if err != nil {
			e.scrapeSuccess.Set(0)
			return err
		}
		for _, d := range dailyService {
			day := d.Day
			if err := e.daily.SetTotalCost(day, e.cfg.Window, costMetric, d.Total); err != nil {
				e.scrapeSuccess.Set(0)
				return err
			}
			for svc, v := range d.ByService {
				if err := e.daily.SetAggCost("service", svc, day, e.cfg.Window, costMetric, v); err != nil {
					e.scrapeSuccess.Set(0)
					return err
				}
				if err := e.daily.SetServiceCost(svc, day, e.cfg.Window, costMetric, v); err != nil {
					e.scrapeSuccess.Set(0)
					return err
				}
			}
		}

		for _, agg := range e.cfg.Aggregates {
			rows, err := e.fetchTable(ctx, agg, costMetric)
			if err != nil {
				e.scrapeSuccess.Set(0)
				return err
			}
			for _, r := range rows {
				e.cloudAggCost.WithLabelValues(agg, r.Name, e.cfg.Window, costMetric).Set(r.Cost)
				e.cloudAggK8sPct.WithLabelValues(agg, r.Name, e.cfg.Window, costMetric).Set(r.KubernetesPercent)

				if agg == "service" {
					e.cloudServiceCost.WithLabelValues(r.Name, e.cfg.Window, costMetric).Set(r.Cost)
					e.cloudServiceK8sPct.WithLabelValues(r.Name, e.cfg.Window, costMetric).Set(r.KubernetesPercent)
				}
				if agg == "category" {
					e.cloudCategoryCost.WithLabelValues(r.Name, e.cfg.Window, costMetric).Set(r.Cost)
				}
			}

			// Daily series for each aggregate (service already scraped above).
			if agg == "service" {
				continue
			}
			daily, err := e.fetchGraph(ctx, agg, costMetric)
			if err != nil {
				e.scrapeSuccess.Set(0)
				return err
			}
			for _, d := range daily {
				day := d.Day
				for name, v := range d.ByService {
					if err := e.daily.SetAggCost(agg, name, day, e.cfg.Window, costMetric, v); err != nil {
						e.scrapeSuccess.Set(0)
						return err
					}
					if agg == "category" {
						if err := e.daily.SetCategoryCost(name, day, e.cfg.Window, costMetric, v); err != nil {
							e.scrapeSuccess.Set(0)
							return err
						}
					}
				}
			}
		}
	}

	e.scrapeSuccess.Set(1)
	return nil
}

func (e *exporter) fetchStatus(ctx context.Context) (cloudCostStatusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.statusURL(), nil)
	if err != nil {
		return cloudCostStatusResponse{}, err
	}
	resp, err := e.cli.Do(req)
	if err != nil {
		return cloudCostStatusResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return cloudCostStatusResponse{}, fmt.Errorf("status http status %d", resp.StatusCode)
	}
	var out cloudCostStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return cloudCostStatusResponse{}, err
	}
	if out.Code != 200 {
		return cloudCostStatusResponse{}, fmt.Errorf("status response code %d", out.Code)
	}
	return out, nil
}

func (e *exporter) applyStatus(status cloudCostStatusResponse) {
	for _, s := range status.Data {
		up := 0.0
		if s.Active && s.Valid {
			up = 1.0
		}
		e.cloudIntegrationUp.WithLabelValues(s.Key, s.Provider, s.Source, s.ConnectionStatus).Set(up)

		if t, err := time.Parse(time.RFC3339Nano, s.LastRun); err == nil {
			e.cloudIntegrationTS.WithLabelValues(s.Key, s.Provider, "last_run").Set(float64(t.Unix()))
		}
		if t, err := time.Parse(time.RFC3339Nano, s.NextRun); err == nil {
			e.cloudIntegrationTS.WithLabelValues(s.Key, s.Provider, "next_run").Set(float64(t.Unix()))
		}
	}
}

func (e *exporter) fetchTotals(ctx context.Context, costMetric string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.totalsURL(costMetric), nil)
	if err != nil {
		return 0, err
	}
	resp, err := e.cli.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, fmt.Errorf("totals http status %d", resp.StatusCode)
	}
	var out cloudCostTotalsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if out.Code != 200 {
		return 0, fmt.Errorf("totals response code %d", out.Code)
	}
	return out.Data.Combined.Cost, nil
}

type tableRow struct {
	Name              string
	KubernetesPercent float64
	Cost              float64
}

func (e *exporter) fetchTable(ctx context.Context, aggregate, costMetric string) ([]tableRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.tableURL(aggregate, costMetric), nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("table http status %d", resp.StatusCode)
	}
	var out cloudCostTableResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Code != 200 {
		return nil, fmt.Errorf("table response code %d", out.Code)
	}
	rows := make([]tableRow, 0, len(out.Data))
	for _, r := range out.Data {
		rows = append(rows, tableRow{Name: r.Name, KubernetesPercent: r.KubernetesPercent, Cost: r.Cost})
	}
	return rows, nil
}

type dailyPoint struct {
	Day       string
	Total     float64
	ByService map[string]float64
}

func (e *exporter) fetchGraph(ctx context.Context, aggregate, costMetric string) ([]dailyPoint, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.graphURL(aggregate, costMetric), nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("graph http status %d", resp.StatusCode)
	}
	var out cloudCostGraphResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Code != 200 {
		return nil, fmt.Errorf("graph response code %d", out.Code)
	}

	points := make([]dailyPoint, 0, len(out.Data))
	for _, d := range out.Data {
		// OpenCost returns start like "2025-12-04T00:00:00Z"
		day := d.Start
		if len(day) >= 10 {
			day = day[:10]
		}
		byService := make(map[string]float64, len(d.Items))
		total := 0.0
		for _, it := range d.Items {
			byService[it.Name] = it.Value
			total += it.Value
		}
		points = append(points, dailyPoint{
			Day:       day,
			Total:     total,
			ByService: byService,
		})
	}
	return points, nil
}

type dailySample struct {
	desc   *prometheus.Desc
	labels []string
	value  float64
	ts     time.Time
}

type dailyCollector struct {
	mu sync.Mutex

	dailyAggCostDesc      *prometheus.Desc
	dailyServiceCostDesc  *prometheus.Desc
	dailyTotalCostDesc    *prometheus.Desc
	dailyCategoryCostDesc *prometheus.Desc

	samples []dailySample
}

func newDailyCollector() *dailyCollector {
	return &dailyCollector{
		dailyAggCostDesc: prometheus.NewDesc(
			"opencost_cloudcost_daily_aggregate_cost",
			"Cloud cost by aggregate property per day (from /cloudCost/view/graph).",
			[]string{"aggregate", "name", "day", "window", "cost_metric"},
			nil,
		),
		dailyServiceCostDesc: prometheus.NewDesc(
			"opencost_cloudcost_daily_service_cost",
			"Cloud cost by service per day (from /cloudCost/view/graph).",
			[]string{"service", "day", "window", "cost_metric"},
			nil,
		),
		dailyTotalCostDesc: prometheus.NewDesc(
			"opencost_cloudcost_daily_total_cost",
			"Total cloud cost per day (sum of items in /cloudCost/view/graph).",
			[]string{"day", "window", "cost_metric"},
			nil,
		),
		dailyCategoryCostDesc: prometheus.NewDesc(
			"opencost_cloudcost_daily_category_cost",
			"Cloud cost by category (resource type) per day (from /cloudCost/view/graph).",
			[]string{"category", "day", "window", "cost_metric"},
			nil,
		),
	}
}

func (d *dailyCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- d.dailyAggCostDesc
	ch <- d.dailyServiceCostDesc
	ch <- d.dailyTotalCostDesc
	ch <- d.dailyCategoryCostDesc
}

func (d *dailyCollector) Collect(ch chan<- prometheus.Metric) {
	d.mu.Lock()
	snaps := make([]dailySample, len(d.samples))
	copy(snaps, d.samples)
	d.mu.Unlock()

	for _, s := range snaps {
		m, err := prometheus.NewConstMetric(s.desc, prometheus.GaugeValue, s.value, s.labels...)
		if err != nil {
			log.Printf("daily metric build failed: %v", err)
			continue
		}
		ch <- prometheus.NewMetricWithTimestamp(s.ts, m)
	}
}

func (d *dailyCollector) Reset() {
	d.mu.Lock()
	d.samples = d.samples[:0]
	d.mu.Unlock()
}

func parseDayUTC(day string) (time.Time, error) {
	// day is expected to be YYYY-MM-DD (derived from OpenCost graph start).
	return time.ParseInLocation("2006-01-02", day, time.UTC)
}

func (d *dailyCollector) add(desc *prometheus.Desc, ts time.Time, value float64, labels ...string) {
	d.samples = append(d.samples, dailySample{
		desc:   desc,
		labels: append([]string(nil), labels...),
		value:  value,
		ts:     ts,
	})
}

func (d *dailyCollector) SetAggCost(aggregate, name, day, window, costMetric string, value float64) error {
	ts, err := parseDayUTC(day)
	if err != nil {
		return fmt.Errorf("invalid day %q for daily_aggregate_cost: %w", day, err)
	}
	d.mu.Lock()
	d.add(d.dailyAggCostDesc, ts, value, aggregate, name, day, window, costMetric)
	d.mu.Unlock()
	return nil
}

func (d *dailyCollector) SetServiceCost(service, day, window, costMetric string, value float64) error {
	ts, err := parseDayUTC(day)
	if err != nil {
		return fmt.Errorf("invalid day %q for daily_service_cost: %w", day, err)
	}
	d.mu.Lock()
	d.add(d.dailyServiceCostDesc, ts, value, service, day, window, costMetric)
	d.mu.Unlock()
	return nil
}

func (d *dailyCollector) SetTotalCost(day, window, costMetric string, value float64) error {
	ts, err := parseDayUTC(day)
	if err != nil {
		return fmt.Errorf("invalid day %q for daily_total_cost: %w", day, err)
	}
	d.mu.Lock()
	d.add(d.dailyTotalCostDesc, ts, value, day, window, costMetric)
	d.mu.Unlock()
	return nil
}

func (d *dailyCollector) SetCategoryCost(category, day, window, costMetric string, value float64) error {
	ts, err := parseDayUTC(day)
	if err != nil {
		return fmt.Errorf("invalid day %q for daily_category_cost: %w", day, err)
	}
	d.mu.Lock()
	d.add(d.dailyCategoryCostDesc, ts, value, category, day, window, costMetric)
	d.mu.Unlock()
	return nil
}

func main() {
	cfg := mustConfig()
	e := newExporter(cfg)

	// Initial scrape before serving metrics.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	if err := e.scrape(ctx); err != nil {
		// Keep running; metrics will show scrape_success=0.
		log.Printf("initial scrape failed: %v", err)
	}
	cancel()

	// Background refresh loop.
	go func() {
		t := time.NewTicker(cfg.RefreshInterval)
		defer t.Stop()
		for {
			<-t.C
			ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
			err := e.scrape(ctx)
			cancel()
			if err != nil {
				log.Printf("scrape failed: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("opencost cloud cost exporter\n"))
		_, _ = w.Write([]byte("/metrics\n"))
		_, _ = w.Write([]byte("/healthz\n"))
		_, _ = w.Write([]byte("config:\n"))
		_, _ = w.Write([]byte("  OPENCOST_URL=" + cfg.OpenCostURL + "\n"))
		_, _ = w.Write([]byte("  WINDOW=" + cfg.Window + "\n"))
		_, _ = w.Write([]byte("  COST_METRIC=" + cfg.CostMetric + "\n"))
		_, _ = w.Write([]byte("  REFRESH_INTERVAL=" + cfg.RefreshInterval.String() + "\n"))
		_, _ = w.Write([]byte("  HTTP_TIMEOUT=" + cfg.HTTPTimeout.String() + "\n"))
		_, _ = w.Write([]byte("  LISTEN_ADDR=" + cfg.ListenAddr + "\n"))
		_ = r
	})

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	log.Printf("listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
