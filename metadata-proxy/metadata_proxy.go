package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/contrib/metadata-proxy/metrics"
)

var (
	concealedEndpoints = []string{
		"/0.1/meta-data/attributes/kube-env",
		"/computeMetadata/v1beta1/instance/attributes/kube-env",
		"/computeMetadata/v1/instance/attributes/kube-env",
	}
	concealedPatterns = []*regexp.Regexp{
		regexp.MustCompile("/0.1/meta-data/service-accounts/.+/identity"),
		regexp.MustCompile("/computeMetadata/v1beta1/instance/service-accounts/.+/identity"),
		regexp.MustCompile("/computeMetadata/v1/instance/service-accounts/.+/identity"),
	}
	knownPrefixes = []string{
		"/0.1/meta-data/",
		"/computeMetadata/v1beta1/",
		"/computeMetadata/v1/",
	}
	discoveryEndpoints = []string{
		"",
		"/",
		"/0.1",
		"/0.1/",
		"/0.1/meta-data",
		"/computeMetadata",
		"/computeMetadata/",
		"/computeMetadata/v1beta1",
		"/computeMetadata/v1",
	}
)

var (
	proxyTypeBlocked = "proxy_type_blocked"
	proxyTypeProxied = "proxy_type_proxied"
)

func main() {
	// TODO(ihmccreery) Make these ports configurable.
	go func() {
		err := http.ListenAndServe("127.0.0.1:989", promhttp.Handler())
		log.Fatalf("Failed to start metrics: %v", err)
	}()
	log.Fatal(http.ListenAndServe("127.0.0.1:988", newMetadataHandler()))
}

// xForwardedForStripper is identical to http.DefaultTransport except that it
// strips X-Forwarded-For headers.  It fulfills the http.RoundTripper
// interface.
type xForwardedForStripper struct{}

// RoundTrip wraps the http.DefaultTransport.RoundTrip method, and strips
// X-Forwarded-For headers, since httputil.ReverseProxy.ServeHTTP adds it but
// the GCE metadata server rejects requests with that header.
func (x xForwardedForStripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Del("X-Forwarded-For")
	return http.DefaultTransport.RoundTrip(req)
}

// responseWriter wraps the given http.ResponseWriter to record metrics.
type responseWriter struct {
	proxyType string
	code      int
	http.ResponseWriter
}

func newResponseWriter(rw http.ResponseWriter) *responseWriter {
	return &responseWriter{
		"",
		0,
		rw,
	}
}

// WriteHeader records the header and writes the appropriate metric.
func (m responseWriter) WriteHeader(code int) {
	metrics.RequestCounter.WithLabelValues(m.proxyType, strconv.Itoa(code)).Inc()
	m.ResponseWriter.WriteHeader(code)
}

type metadataHandler struct {
	proxy *httputil.ReverseProxy
}

func newMetadataHandler() *metadataHandler {
	u, err := url.Parse("http://169.254.169.254")
	if err != nil {
		log.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)

	x := xForwardedForStripper{}
	proxy.Transport = x

	return &metadataHandler{
		proxy: proxy,
	}
}

func (h *metadataHandler) ServeHTTP(hrw http.ResponseWriter, req *http.Request) {
	rw := newResponseWriter(hrw)
	if req.URL.Query().Get("recursive") != "" {
		rw.proxyType = proxyTypeBlocked
		http.Error(rw, "?recursive calls are not allowed by the metadata proxy.", http.StatusForbidden)
		return
	}
	for _, e := range concealedEndpoints {
		if req.URL.Path == e {
			rw.proxyType = proxyTypeBlocked
			http.Error(rw, "This metadata endpoint is concealed.", http.StatusForbidden)
			return
		}
	}
	for _, p := range concealedPatterns {
		if p.MatchString(req.URL.Path) {
			rw.proxyType = proxyTypeBlocked
			http.Error(rw, "This metadata endpoint is concealed.", http.StatusForbidden)
			return
		}
	}
	// Since we're stripping the X-Forwarded-For header that's added by
	// httputil.ReverseProxy.ServeHTTP, check for the header here and
	// refuse to serve if it's present.
	if _, ok := req.Header["X-Forwarded-For"]; ok {
		rw.proxyType = proxyTypeBlocked
		http.Error(rw, "Calls with X-Forwarded-For header are not allowed by the metadata proxy.", http.StatusForbidden)
		return
	}
	for _, p := range knownPrefixes {
		if strings.HasPrefix(req.URL.Path, p) {
			rw.proxyType = proxyTypeProxied
			h.proxy.ServeHTTP(rw, req)
			return
		}
	}
	for _, e := range discoveryEndpoints {
		if req.URL.Path == e {
			rw.proxyType = proxyTypeProxied
			h.proxy.ServeHTTP(rw, req)
			return
		}
	}
	rw.proxyType = proxyTypeBlocked
	http.Error(rw, "This metadata API is not allowed by the metadata proxy.", http.StatusForbidden)
}
