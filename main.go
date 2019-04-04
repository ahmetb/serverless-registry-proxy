package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
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
	re = regexp.MustCompile(`^/v2/`)
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
	browserRedirects := os.Getenv("DISABLE_BROWSER_REDIRECTS") == ""

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

	var authHeader string
	if keyPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); keyPath != "" {
		b, err := ioutil.ReadFile(keyPath)
		if err != nil {
			log.Fatalf("could not read key file from %s: %+v", keyPath, err)
		}
		log.Printf("using specified service account json key to authenticate proxied requests")
		authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("_json_key:%s", string(b))))
	}

	addr := ":" + port
	if browserRedirects {
		http.Handle("/", browserRedirectHandler(gcr))
	}
	http.Handle("/v2/", registryAPIMux(gcr, authHeader))
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
func registryAPIMux(c gcrConfig, authHeader string) http.HandlerFunc {
	reverseProxy := &httputil.ReverseProxy{
		Director: rewriteRegistryV2URL(c),
		Transport: &gcrRoundtripper{
			authHeader: authHeader,
		},
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

// rewriteRegistryV2URL rewrites request.URL like /v2/* that come into the server
// into https://[GCR_HOST]/v2/[PROJECT_ID]/*. It leaves /v2/ as is.
func rewriteRegistryV2URL(c gcrConfig) func(*http.Request) {
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

type gcrRoundtripper struct {
	authHeader string
}

func (g *gcrRoundtripper) RoundTrip(req *http.Request) (*http.Response, error) {
	log.Printf("request received. url=%s", req.URL)

	if g.authHeader != "" {
		req.Header.Set("Authorization", g.authHeader)
	}

	if ua := req.Header.Get("user-agent"); ua != "" {
		req.Header.Set("user-agent", "gcr-proxy/0.1 "+ua)
	}

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
