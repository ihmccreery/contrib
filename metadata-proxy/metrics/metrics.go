package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	RequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "metadata_proxy_request_count",
			Help: "Counter of metadata proxy requests broken out by each type of request and HTTP response code.",
		},
		[]string{"proxy_type", "code"},
	)
)

func init() {
	prometheus.MustRegister(RequestCounter)
}
