package mqtt_consumer

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"

	"git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
)

const MaxClientIdLen = 8
const MaxRetryCount = 3

type MQTTConsumer struct {
	Servers []string
	Topics  []string

	Username string
	Password string

	MetricBuffer int

	sync.Mutex
	client *mqtt.Client
	// channel for all incoming parsed mqtt metrics
	metricC chan telegraf.Metric
	done    chan struct{}
	in      chan []byte
}

var sampleConfig = `
  server = "localhost:1883"

  topics = [
    "telegraf/host01/cpu",
    "telegraf/host02/mem",
  ]

  # Maximum number of metrics to buffer between collection intervals
  metric_buffer = 100000

  # username and password to connect MQTT server.
  # username = "telegraf"
  # password = "metricsmetricsmetricsmetrics"
`

func (m *MQTTConsumer) SampleConfig() string {
	return sampleConfig
}

func (m *MQTTConsumer) Description() string {
	return "Read line-protocol metrics from MQTT topic(s)"
}

func (m *MQTTConsumer) Start() error {
	m.Lock()
	defer m.Unlock()

	opts, err := m.createOpts()
	if err != nil {
		return err
	}

	m.client = mqtt.NewClient(opts)
	if token := m.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	m.in = make(chan []byte, m.MetricBuffer)
	m.done = make(chan struct{})
	if m.MetricBuffer == 0 {
		m.MetricBuffer = 100000
	}
	m.metricC = make(chan telegraf.Metric, m.MetricBuffer)

	topics := make(map[string]byte)
	for _, topic := range m.Topics {
		topics[topic] = byte(0)
	}
	subscribeToken := m.client.SubscribeMultiple(topics, m.recvMessage)
	subscribeToken.Wait()
	if subscribeToken.Error() != nil {
		return subscribeToken.Error()
	}

	go m.parser()

	return nil
}

func (m *MQTTConsumer) parser() {
	for {
		select {
		case <-m.done:
			return
		case msg := <-m.in:
			metrics, err := telegraf.ParseMetrics(msg)
			if err != nil {
				log.Printf("Could not parse MQTT message: %s, error: %s",
					string(msg), err.Error())
			}

			for _, metric := range metrics {
				select {
				case m.metricC <- metric:
					continue
				default:
					log.Printf("MQTT Consumer buffer is full, dropping a metric." +
						" You may want to increase the metric_buffer setting")
				}
			}
		}
	}
}

func (m *MQTTConsumer) recvMessage(_ *mqtt.Client, msg mqtt.Message) {
	m.in <- msg.Payload()
}

func (m *MQTTConsumer) Stop() {
	m.Lock()
	defer m.Unlock()
	close(m.done)
	m.client.Disconnect(200)
}

func (m *MQTTConsumer) Gather(acc telegraf.Accumulator) error {
	m.Lock()
	defer m.Unlock()
	nmetrics := len(m.metricC)
	for i := 0; i < nmetrics; i++ {
		metric := <-m.metricC
		acc.AddFields(metric.Name(), metric.Fields(), metric.Tags(), metric.Time())
	}
	return nil
}

func (m *MQTTConsumer) createOpts() (*mqtt.ClientOptions, error) {
	opts := mqtt.NewClientOptions()

	opts.SetClientID("Telegraf-MQTT-Consumer")

	TLSConfig := &tls.Config{InsecureSkipVerify: false}
	ca := "" // TODO
	scheme := "tcp"
	if ca != "" {
		scheme = "ssl"
		certPool, err := getCertPool(ca)
		if err != nil {
			return nil, err
		}
		TLSConfig.RootCAs = certPool
	}
	TLSConfig.InsecureSkipVerify = true // TODO
	opts.SetTLSConfig(TLSConfig)

	user := m.Username
	if user == "" {
		opts.SetUsername(user)
	}
	password := m.Password
	if password != "" {
		opts.SetPassword(password)
	}

	if len(m.Servers) == 0 {
		return opts, fmt.Errorf("could not get host infomations")
	}
	for _, host := range m.Servers {
		server := fmt.Sprintf("%s://%s", scheme, host)

		opts.AddBroker(server)
	}
	opts.SetAutoReconnect(true)
	// Setting KeepAlive to 0 disables it.
	// TODO set KeepAlive to a real value (60s?) when this change is merged:
	// https://git.eclipse.org/r/#/c/65850/
	opts.SetKeepAlive(time.Duration(0))
	return opts, nil
}

func getCertPool(pemPath string) (*x509.CertPool, error) {
	certs := x509.NewCertPool()

	pemData, err := ioutil.ReadFile(pemPath)
	if err != nil {
		return nil, err
	}
	certs.AppendCertsFromPEM(pemData)
	return certs, nil
}

func init() {
	inputs.Add("mqtt_consumer", func() telegraf.Input {
		return &MQTTConsumer{}
	})
}
