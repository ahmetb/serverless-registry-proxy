/*
Copyright 2020 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
)

const (
	ctxKeyOriginalHost = myContextKey("original-host")
)

var (
	re                 = regexp.MustCompile(`^/v2/`)
	realm              = regexp.MustCompile(`realm="(.*?)"`)
)

type myContextKey string

type registryConfig struct {
	host       string
	repoPrefix string
}

func main() {
	host := os.Getenv("HOST")

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT environment variable not specified")
	}
	browserRedirects := os.Getenv("DISABLE_BROWSER_REDIRECTS") == ""

	registryHost := os.Getenv("REGISTRY_HOST")
	if registryHost == "" {
		log.Fatal("REGISTRY_HOST environment variable not specified (example: gcr.io)")
	}
	repoPrefix := os.Getenv("REPO_PREFIX")
	if repoPrefix == "" {
		log.Fatal("REPO_PREFIX environment variable not specified")
	}

	reg := registryConfig{
		host:       registryHost,
		repoPrefix: repoPrefix,
	}

	tokenEndpoint, err := discoverTokenService(reg.host)
	if err != nil {
		log.Fatalf("target registry's token endpoint could not be discovered: %+v", err)
	}
	log.Printf("discovered token endpoint for backend registry: %s", tokenEndpoint)

	var auth authenticator
	if basic := os.Getenv("AUTH_HEADER"); basic != "" {
		auth = authHeader(basic)
	} else if gcpKey := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); gcpKey != "" {
		b, err := ioutil.ReadFile(gcpKey)
		if err != nil {
			log.Fatalf("could not read key file from %s: %+v", gcpKey, err)
		}
		log.Printf("using specified service account json key to authenticate proxied requests")
		auth = authHeader("Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("_json_key:%s", string(b)))))
	}

	mux := http.NewServeMux()
	if browserRedirects {
		mux.Handle("/", browserRedirectHandler(reg))
	}
	if tokenEndpoint != "" {
		mux.Handle("/_token", tokenProxyHandler(tokenEndpoint, repoPrefix))
	}
	mux.Handle("/v2/", registryAPIProxy(reg, auth))

	addr := fmt.Sprintf("%s:%s", host, port)
	handler := captureHostHeader(mux)
	log.Printf("starting to listen on %s", addr)
	if cert, key := os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY"); cert != "" && key != "" {
		err = http.ListenAndServeTLS(addr, cert, key, handler)
	} else {
		err = http.ListenAndServe(addr, handler)
	}
	if err != http.ErrServerClosed {
		log.Fatalf("listen error: %+v", err)
	}

	log.Printf("server shutdown successfully")
}

func discoverTokenService(registryHost string) (string, error) {
	url := fmt.Sprintf("https://%s/v2/", registryHost)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to query the registry host %s: %+v", registryHost, err)
	}
	hdr := resp.Header.Get("www-authenticate")
	if hdr == "" {
		return "", fmt.Errorf("www-authenticate header not returned from %s, cannot locate token endpoint", url)
	}
	matches := realm.FindStringSubmatch(hdr)
	if len(matches) == 0 {
		return "", fmt.Errorf("cannot locate 'realm' in %s response header www-authenticate: %s", url, hdr)
	}
	return matches[1], nil
}

// captureHostHeader is a middleware to capture Host header in a context key.
func captureHostHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), ctxKeyOriginalHost, req.Host)
		req = req.WithContext(ctx)
		next.ServeHTTP(rw, req.WithContext(ctx))
	})
}

// tokenProxyHandler proxies the token requests to the specified token service.
// It adjusts the ?scope= parameter in the query from "repository:foo:..." to
// "repository:repoPrefix/foo:.." and reverse proxies the query to the specified
// tokenEndpoint.
func tokenProxyHandler(tokenEndpoint, repoPrefix string) http.HandlerFunc {
	return (&httputil.ReverseProxy{
		FlushInterval: -1,
		Director: func(r *http.Request) {
			orig := r.URL.String()

			q := r.URL.Query()
			scope := q.Get("scope")
			newScope := strings.Replace(scope, "repository:", fmt.Sprintf("repository:%s/", repoPrefix), 1)
			q.Set("scope", newScope)
			u, _ := url.Parse(tokenEndpoint)
			u.RawQuery = q.Encode()
			r.URL = u
			log.Printf("tokenProxyHandler: rewrote url:%s into:%s", orig, r.URL)
			r.Host = u.Host
		},
	}).ServeHTTP
}

// browserRedirectHandler redirects a request like example.com/my-image to
// REGISTRY_HOST/my-image, which shows a public UI for browsing the registry.
// This works only on registries that support a web UI when the image name is
// entered into the browser, like GCR (gcr.io/google-containers/busybox).
func browserRedirectHandler(cfg registryConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url := fmt.Sprintf("https://%s/%s%s", cfg.host, cfg.repoPrefix, r.RequestURI)
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	}
}

// registryAPIProxy returns a reverse proxy to the specified registry.
func registryAPIProxy(cfg registryConfig, auth authenticator) http.HandlerFunc {
	return (&httputil.ReverseProxy{
		FlushInterval: -1,
		Director:      rewriteRegistryV2URL(cfg),
		Transport: &registryRoundtripper{
			auth: auth,
		},
	}).ServeHTTP
}

// rewriteRegistryV2URL rewrites request.URL like /v2/* that come into the server
// into https://[GCR_HOST]/v2/[PROJECT_ID]/*. It leaves /v2/ as is.
func rewriteRegistryV2URL(c registryConfig) func(*http.Request) {
	return func(req *http.Request) {
		u := req.URL.String()
		req.Host = c.host
		req.URL.Scheme = "https"
		req.URL.Host = c.host
		if req.URL.Path != "/v2/" {
			req.URL.Path = re.ReplaceAllString(req.URL.Path, fmt.Sprintf("/v2/%s/", c.repoPrefix))
		}
		log.Printf("rewrote url: %s into %s", u, req.URL)
	}
}

type registryRoundtripper struct {
	auth authenticator
}

func (rrt *registryRoundtripper) RoundTrip(req *http.Request) (*http.Response, error) {
	log.Printf("request received. url=%s", req.URL)

	if rrt.auth != nil {
		req.Header.Set("Authorization", rrt.auth.AuthHeader())
	}

	origHost := req.Context().Value(ctxKeyOriginalHost).(string)
	if ua := req.Header.Get("user-agent"); ua != "" {
		req.Header.Set("user-agent", "gcr-proxy/0.1 customDomain/"+origHost+" "+ua)
	}

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil {
		log.Printf("request completed (status=%d) url=%s", resp.StatusCode, req.URL)
	} else {
		log.Printf("request failed with error: %+v", err)
		return nil, err
	}

	// Google Artifact Registry sends a "location: /artifacts-downloads/..." URL
	// to download blobs. We don't want these routed to the proxy itself.
	if locHdr := resp.Header.Get("location"); req.Method == http.MethodGet &&
		resp.StatusCode == http.StatusFound && strings.HasPrefix(locHdr, "/") {
		resp.Header.Set("location", req.URL.Scheme+"://"+req.URL.Host+locHdr)
	}

	updateTokenEndpoint(resp, origHost)
	return resp, nil
}

// updateTokenEndpoint modifies the response header like:
//    Www-Authenticate: Bearer realm="https://auth.docker.io/token",service="registry.docker.io"
// to point to the https://host/token endpoint to force using local token
// endpoint proxy.
func updateTokenEndpoint(resp *http.Response, host string) {
	v := resp.Header.Get("www-authenticate")
	if v == "" {
		return
	}
	cur := fmt.Sprintf("https://%s/_token", host)
	resp.Header.Set("www-authenticate", realm.ReplaceAllString(v, fmt.Sprintf(`realm="%s"`, cur)))
}

type authenticator interface {
	AuthHeader() string
}

type authHeader string

func (b authHeader) AuthHeader() string { return string(b) }
