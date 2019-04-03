package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
)

const (
	defaultGCRHost = "gcr.io"
)

var (
	re               = regexp.MustCompile(`^/v2/`)
	browserRedirects bool
)

type gcrConfig struct {
	host      string
	projectID string
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT environment variable not specified")
	}
	browserRedirects = os.Getenv("DISABLE_BROWSER_REDIRECTS") == ""

	gcrHost := defaultGCRHost
	if v := os.Getenv("GCR_HOST"); v != "" {
		gcrHost = v
	}
	gcrProjectID := os.Getenv("GCR_PROJECT_ID")
	if gcrProjectID == "" {
		log.Fatal("GCR_PROJECT_ID environment variable not specified")
	}

	gcr := gcrConfig{
		host:      gcrHost,
		projectID: gcrProjectID,
	}

	addr := ":" + port
	if browserRedirects {
		http.Handle("/", browserRedirectHandler(gcr))
	}
	http.Handle("/v2/", registryAPIMux(gcr))
	log.Printf("starting to listen on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen error: %+v", err)
	}
	log.Printf("server shutdown successfully")
}

// browserRedirectHandler redirects a request like example.com/my-image to
// gcr.io/my-image, which shows a public UI for browsing the registry.
func browserRedirectHandler(c gcrConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url := fmt.Sprintf("https://%s/%s%s", c.host, c.projectID, r.RequestURI)
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	}
}

// registryAPIMux returns a handler for Docker Registry v2 API requests
// (/v2/). Request to path=/v2/ is handled-locally, other /v2/* requests are
// proxied back to GCR endpoint.
func registryAPIMux(c gcrConfig) http.HandlerFunc {
	reverseProxy := &httputil.ReverseProxy{
		Transport: roundtripperFunc(gcrRoundtripper),
		Director:  rewriteRegistryV2(c),
	}

	return func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v2/" {
			handleRegistryAPIVersion(w, req)
			return
		}
		reverseProxy.ServeHTTP(w, req)
	}
}

// handleRegistryAPIVersion signals docker-registry v2 API on /v2/ endpoint.
func handleRegistryAPIVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	fmt.Fprint(w, "ok")
}

// rewriteRegistryV2 rewrites request.URL like /v2/* that come into the server
// into https://[GCR_HOST]/v2/[PROJECT_ID]/*. It leaves /v2/ as is.
func rewriteRegistryV2(c gcrConfig) func(*http.Request) {
	return func(req *http.Request) {
		u := req.URL.String()
		req.Host = c.host
		req.URL.Scheme = "https"
		req.URL.Host = c.host
		if req.URL.Path != "/v2/" {
			req.URL.Path = re.ReplaceAllString(req.URL.Path, fmt.Sprintf("/v2/%s/", c.projectID))
		}
		log.Printf("rewrote url: %s into %s", u, req.URL)
	}
}

type roundtripperFunc func(*http.Request) (*http.Response, error)

func (f roundtripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func gcrRoundtripper(req *http.Request) (*http.Response, error) {
	log.Printf("request received. url=%s", req.URL)

	// TODO(ahmetb) remove after internal bug 129780113 is fixed.
	req.Header.Set("accept", "*/*")

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil {
		log.Printf("request completed (status=%d) url=%s", resp.StatusCode, req.URL)
	} else {
		log.Printf("request failed with error: %+v", err)
	}
	return resp, err
}
