package cmp

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

type Cmp struct {
	ApiUser     string
	ApiKey      string
	ResourceId  string
	CmpInstance string
	Timeout     internal.Duration

	client *http.Client
}

var sampleConfig = `
  # Cmp Api User and Key
  api_user = "my-api-user" # required.
  api_key = "my-api-key" # required.
  resource_id = "1234"

  # Cmp Instance URL
  cmp_instance = "https://yourcmpinstance" # required

  # Connection timeout.
  # timeout = "5s"
`

var translateMap = map[string]Translation{
    "cpu-usage.idle": {
        Name: "cpu-usage",
        Unit: "percent",
        Conversion: subtract_from_100_percent,
    },
    "cpu-usage.user": {
        Name: "cpu-usage.user",
        Unit: "percent",
    },
	"cpu-usage.system": {
		Name: "cpu-usage.system",
		Unit: "percent",
	},
	"mem-available.percent": {
		Name:       "memory-usage",
		Unit:       "percent",
		Conversion: subtract_from_100_percent,
	},
	"system-load1": {
		Name: "load-avg-1",
	},
	"system-load5": {
		Name: "load-avg-5",
	},
	"system-load15": {
		Name: "load-avg-15",
	},
	"disk-used.percent": {
		Name: "disk-usage",
	},
	//     "system-uptime": {
	//         Name: "uptime",
	//     },
	"docker_cpu-usage.percent": {
		Name: "docker-cpu-usage.system",
		Unit: "percent",
	},
	"docker_mem-usage.percent": {
		Name: "docker-memory-usage",
		Unit: "percent",
	},
    "elasticsearch_cluster_health-status": {
        Name: "es-status",
        Unit: "",
        Conversion: es_cluster_health,
    },
    "elasticsearch_cluster_health-number.of.nodes": {
        Name: "es-nodes",
        Unit: "",
    },
    "elasticsearch_cluster_health-unassigned.shards": {
        Name: "es-unassigned-shards",
        Unit: "",
    },
    "elasticsearch_cluster_health-active.shards": {
        Name: "es-active-shards",
        Unit: "",
    },
    "elasticsearch_cluster_health-active.primary.shards": {
        Name: "es-primary-shards",
        Unit: "",
    },
    "elasticsearch_cluster_health-initializing.shards": {
        Name: "es-initializing-shards",
        Unit: "",
    },
    "elasticsearch_cluster_health-relocating.shards": {
        Name: "es-relocating-shards",
        Unit: "",
    },
    "elasticsearch_jvm-mem.heap.used.in.bytes": {
        Name: "es-heap.used",
        Unit: "B",
    },
    "elasticsearch_jvm-mem.heap.committed.in.bytes": {
        Name: "es-heap.committed",
        Unit: "B",
    },
    "elasticsearch_jvm-mem.non.heap.used.in.bytes": {
        Name: "es-nonheap.used",
        Unit: "B",
    },
    "elasticsearch_jvm-mem.non.heap.committed.in.bytes": {
        Name: "es-nonheap.committed",
        Unit: "B",
    },
    "elasticsearch_indices-search.query.total": {
        Name: "es-search-requests.query",
        Unit: "",
    },
    "elasticsearch_indices-search.fetch.total": {
        Name: "es-search-requests.fetch",
        Unit: "",
    },
    "elasticsearch_indices-search.query.time.in.millis": {
        Name: "es-search-time.query",
        Unit: "Ms",
    },
    "elasticsearch_indices-search.fetch.time.in.millis": {
        Name: "es-search-time.fetch",
        Unit: "Ms",
    },
    "elasticsearch_indices-get.total": {
        Name: "es-get-requests.get",
        Unit: "",
    },
    "elasticsearch_indices-get.exists.total": {
        Name: "es-get-requests.exists",
        Unit: "",
    },
    "elasticsearch_indices-get.missing.total": {
        Name: "es-get-requests.missing",
        Unit: "",
    },
    "elasticsearch_indices-get.time.in.millis": {
        Name: "es-get-time.get",
        Unit: "Ms",
    },
    "elasticsearch_indices-get.exists.time.in.millis": {
        Name: "es-get-time.exists",
        Unit: "Ms",
    },
    "elasticsearch_indices-get.missing.time.in.millis": {
        Name: "es-get-time.missing",
        Unit: "Ms",
    },
    "elasticsearch_indices-indexing.index.total": {
        Name: "es-index-requests.index",
        Unit: "",
    },
    "elasticsearch_indices-indexing.delete.total": {
        Name: "es-index-requests.delete",
        Unit: "",
    },
    "elasticsearch_indices-indexing.index.time.in.millis": {
        Name: "es-index-time.index",
        Unit: "",
    },
    "elasticsearch_indices-indexing.delete.time.in.millis": {
        Name: "es-index-time.delete",
        Unit: "",
    },
}


type Translation struct {
	Name       string
	Unit       string
	Conversion func(interface{}) interface{}
}

func subtract_from_100_percent(available interface{}) interface{} {
	return (100.0 - available.(float64))
}

func es_cluster_health(status interface{}) interface{} {
    switch status.(string) {
        case "green":
            return 0.0
        case "yellow":
            return 1.0
        case "red":
            return 2.0
        default:
            return 3.0
    }
}

func to_float64(value interface{}) float64 {
    switch v := value.(type) {
        case int64:
            return float64(v)
        default:
            return 0.0
    }
}

type CmpData struct {
	MonitoringSystem string      `json:"monitoring_system"`
	ResourceId       string      `json:"resource_id"`
	Metrics          []CmpMetric `json:"metrics"`
}

type CmpMetric struct {
	Metric string    `json:"metric"`
	Unit   string    `json:"unit"`
	Value  float64   `json:"value"`
	Time   time.Time `json:"time"`
}

func (data *CmpData) AddMetric(item CmpMetric) []CmpMetric {
	data.Metrics = append(data.Metrics, item)
	return data.Metrics
}

func (a *Cmp) Connect() error {
	if a.ApiUser == "" || a.ApiKey == "" || a.CmpInstance == "" || a.ResourceId == "" {
		return fmt.Errorf("api_user, api_key, resource_id and cmp_instance are required fields for cmp output")
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	a.client = &http.Client{
		Transport: tr,
		Timeout:   a.Timeout.Duration,
	}
	return nil
}

func (a *Cmp) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	cmp_data := &CmpData{
		MonitoringSystem: "telegraf",
		ResourceId:       a.ResourceId,
	}

	for _, m := range metrics {

		suffix := ""
		cpu := m.Tags()["cpu"]
		path := m.Tags()["path"]
		container_name := m.Tags()["cont_name"]

		if len(cpu) > 0 && cpu != "cpu-total" {
			suffix = cpu[3:]
		} else if len(path) > 0 {
			suffix = path
		} else if len(container_name) > 0 {
			suffix = container_name
		}

		timestamp := m.Time()
		for k, v := range m.Fields() {
			metric_name := m.Name() + "-" + strings.Replace(k, "_", ".", -1)
			translation, found := translateMap[metric_name]
			if found {
				cmp_name := translation.Name
				if len(suffix) > 0 {
					cmp_name += "." + suffix
				}

				conversion := translation.Conversion
				if conversion != nil {
					v = conversion(v)
				}

				cmp_data.AddMetric(CmpMetric{
					Metric: cmp_name,
					Unit:   translation.Unit,
					Value:  to_float64(v),
					Time:   timestamp,
				})
			}
		}
	}

	cmp_bytes, err := json.Marshal(cmp_data)
	if err != nil {
		return fmt.Errorf("unable to marshal TimeSeries, %s\n", err.Error())
	}
	req, err := http.NewRequest("POST", a.authenticatedUrl(), bytes.NewBuffer(cmp_bytes))
	if err != nil {
		return fmt.Errorf("unable to create http.Request, %s\n", err.Error())
	}
	req.Header.Add("Content-Type", "application/json")
	req.SetBasicAuth(a.ApiUser, a.ApiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("error POSTing metrics, %s\n", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 209 {
		return fmt.Errorf("received bad status code, %d\n", resp.StatusCode)
	}

	return nil
}

func (a *Cmp) SampleConfig() string {
	return sampleConfig
}

func (a *Cmp) Description() string {
	return "Configuration for Cmp Server to send metrics to."
}

func (a *Cmp) authenticatedUrl() string {

	return fmt.Sprintf("%s/metrics", a.CmpInstance)
}

func (a *Cmp) Close() error {
	return nil
}

func init() {
	outputs.Add("cmp", func() telegraf.Output {
		return &Cmp{}
	})
}
