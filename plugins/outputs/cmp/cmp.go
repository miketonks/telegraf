package cmp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

type Cmp struct {
	ServerKey    string
    ResourceId   string
	CmpInstance  string
	Timeout      internal.Duration
    Headers      []string

	client *http.Client
}

var sampleConfig = `
  # Cmp Server Key
  server_key = "my-server-key" # required.
  resource_id = "1234"

  # Cmp Instance URL
  cmp_instance = "https://yourcmpinstance" # required

  # Connection timeout.
  # timeout = "5s"

  headers = ["X-Header1:12345", "X-Header2:23456"]
`

var translateMap = map[string]Translation{
    "cpu-usage.user": {
        Name: "cpu-usage.user",
        Unit: "percent",
    },
    "cpu-usage.system": {
        Name: "cpu-usage.system",
        Unit: "percent",
    },
    "mem-available.percent": {
        Name: "memory-used",
        Unit: "percent",
        Conversion: memory_used_from_available,
    },
    "system-load1": {
        Name: "load-avg.1",
    },
    "system-load5": {
        Name: "load-avg.5",
    },
    "system-load15": {
        Name: "load-avg.15",
    },
    "disk-used.percent": {
        Name: "disk-usage",
    },
//     "system-uptime": {
//         Name: "uptime",
//     },
}

type Translation struct {
    Name       string
    Unit       string
    Conversion func(float64) float64
}

var valueConversionMap = map[string]func(float64)float64 {
    "memory-used": memory_used_from_available,
}

func memory_used_from_available(available float64) float64{
    return (100.0 - available)
}

type CmpData struct {
    ResourceId string `json:"resource_id"`
    Metrics    []CmpMetric  `json:"metrics"`
}

type CmpMetric struct {
	Metric     string   `json:"metric"`
	Unit       string   `json:"unit"`
	Value      float64  `json:"value"`
}

func (data *CmpData) AddMetric(item CmpMetric) []CmpMetric {
    data.Metrics = append(data.Metrics, item)
    return data.Metrics
}

type Point [2]float64

func (a *Cmp) Connect() error {
	if a.ServerKey == "" || a.CmpInstance == "" || a.ResourceId == "" {
		return fmt.Errorf("server_key, resource_id and cmp_instance are required fields for cmp output")
	}
	a.client = &http.Client{
		Timeout: a.Timeout.Duration,
	}
	return nil
}

func (a *Cmp) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	cmp_data := &CmpData{
	   ResourceId: a.ResourceId,
	}

	for _, m := range metrics {
        suffix := ""
        cpu := m.Tags()["cpu"]
        path := m.Tags()["path"]

        if len(cpu) > 0 && cpu != "cpu-total" {
            suffix = cpu[3:]
        }
        if len(path) > 0 {
            suffix = path
        }

 		for k, v := range m.Fields() {
            metric_name := m.Name() + "-" + strings.Replace(k, "_", ".", -1)
            translation, found := translateMap[metric_name]
            if found {
                cmp_name := translation.Name
                if len(suffix) > 0 {
                    cmp_name += "." + suffix
                }

                value := v.(float64)
                conversion := translation.Conversion
                if conversion != nil {
                    value = conversion(value)
                }

                cmp_data.AddMetric(CmpMetric{
                    Metric: cmp_name,
                    Unit: translation.Unit,
                    Value: value,
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

    for _, header := range a.Headers {
        s := strings.Split(header, ":")
        req.Header.Add(s[0], s[1])
    }

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
