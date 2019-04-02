package main

import (
	"bytes"
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

	gcrHost := defaultGCRHost
	if v := os.Getenv("GCR_HOST"); v != "" {
		gcrHost = v
	}
	gcrProjectID := os.Getenv("GCR_PROJECT_ID")
	if gcrProjectID == "" {
		log.Fatal("GCR_PROJECT_ID environment variable not specified")
	}

	proxy := &httputil.ReverseProxy{
		Transport: roundTripper(rt),
		Director: rewrite(gcrConfig{
			host:      gcrHost,
			projectID: gcrProjectID,
		}),
	}
	addr := ":" + port
	log.Printf("starting to listen on %s", addr)
	http.Handle("/v2/", proxy)
	if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen error: %+v", err)
	}
	log.Printf("server shutdown successfully")
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func rt(req *http.Request) (*http.Response, error) {
	log.Printf("request received. url=%s", req.URL)

	// TODO(ahmetb) remove after b/129780113 is fixed.
	req.Header.Del("accept")
	req.Header.Set("accept", "*/*")

	// fabricate 200 OK response for /v2/ endpoint
	if req.URL.Path == "/v2/" {
		resp := &http.Response{
			Request: req,
			Header: map[string][]string{
				"Docker-Distribution-API-Version": []string{"registry/2.0"},
			},
			Body:       ioutil.NopCloser(bytes.NewReader(nil)),
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Proto:      req.Proto,
			ProtoMinor: req.ProtoMinor,
			ProtoMajor: req.ProtoMajor,
		}
		return resp, nil
	}

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil {
		log.Printf("request completed (status=%d) url=%s", resp.StatusCode, req.URL)
	} else {
		log.Printf("request failed with error: %+v", err)
	}
	return resp, err
}

func rewrite(c gcrConfig) func(*http.Request) {
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
