package posthog

import (
	"bytes"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/redis/rueidis"
)

const (
	posthogUrl  = "https://us.i.posthog.com"
	proxyPrefix = "/posthog"
	ttl         = time.Minute
)

type Proxy struct {
	proxy *httputil.ReverseProxy
	redis rueidis.Client
}

func NewProxy(redis rueidis.Client) *Proxy {
	targetURL, _ := url.Parse(posthogUrl)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Strip the /posthog prefix
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = strings.TrimPrefix(req.URL.Path, proxyPrefix)
		req.Host = targetURL.Host

		// Remove hop-by-hop headers that cause issues with proxying
		req.Header.Del("Connection")
		req.Header.Del("Upgrade")
		req.Header.Del("Keep-Alive")
		req.Header.Del("Proxy-Connection")
		req.Header.Del("Transfer-Encoding")
	}

	return &Proxy{
		proxy: proxy,
		redis: redis,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, proxyPrefix)

	if path == "/api/feature_flag/local_evaluation" {
		p.handleLocalEvaluation(w, r)
		return
	}

	p.proxy.ServeHTTP(w, r)
}

func (p *Proxy) handleLocalEvaluation(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "posthog:feature_flag_local_eval"

	ctx := r.Context()
	cached, err := p.redis.Do(ctx, p.redis.B().Get().Key(cacheKey).Build()).AsBytes()
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		return
	}

	rec := &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
	p.proxy.ServeHTTP(rec, r)

	if rec.statusCode == http.StatusOK && rec.body.Len() > 0 {
		_ = p.redis.Do(ctx, p.redis.B().Set().Key(cacheKey).Value(rec.body.String()).Ex(ttl).Build()).Error()
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
