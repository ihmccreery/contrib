package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
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

func main() {
	log.Fatal(http.ListenAndServe("127.0.0.1:988", newMetadataHandler()))
}

// xForwardedForStripper is identical to http.DefaultTransport except that it
// strips X-Forwarded-For headers.  It fulfills the http.RoundTripper
// interface.
type xForwardedForStripper struct{}

// RoundTrip wraps the http.DefaultTransport.RoundTrip method, and strips
// X-Forwarded-For headers.
func (x xForwardedForStripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Del("X-Forwarded-For")
	return http.DefaultTransport.RoundTrip(req)
}

type metadataHandler struct {
	proxy *httputil.ReverseProxy
}

func newMetadataHandler() http.Handler {
	u, err := url.Parse("http://169.254.169.254")
	if err != nil {
		log.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)

	// TODO(ihmccreery) Enforce X-Forwarded-For here, since stripping it.

	x := xForwardedForStripper{}
	proxy.Transport = x

	return &metadataHandler{
		proxy: proxy,
	}
}

func (h *metadataHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Println(req.URL.Path)
	if req.URL.Query().Get("recursive") != "" {
		http.Error(rw, "?recursive calls are not allowed by the metadata proxy.", http.StatusForbidden)
		return
	}
	for _, e := range concealedEndpoints {
		if req.URL.Path == e {
			http.Error(rw, "This metadata endpoint is concealed.", http.StatusForbidden)
			return
		}
	}
	for _, p := range concealedPatterns {
		if p.MatchString(req.URL.Path) {
			http.Error(rw, "This metadata endpoint is concealed.", http.StatusForbidden)
			return
		}
	}
	for _, p := range knownPrefixes {
		if strings.HasPrefix(req.URL.Path, p) {
			h.proxy.ServeHTTP(rw, req)
			return
		}
	}
	for _, e := range discoveryEndpoints {
		if req.URL.Path == e {
			h.proxy.ServeHTTP(rw, req)
			return
		}
	}
	http.Error(rw, "This metadata API is not allowed by the metadata proxy.", http.StatusForbidden)
}
