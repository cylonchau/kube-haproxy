package rest

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

var dataplaneapi = "http://10.0.0.3:5555"

var (
	// longThrottleLatency defines threshold for logging requests. All requests being
	// throttled (via the provided rateLimiter) for more than longThrottleLatency will
	// be logged.
	longThrottleLatency = 50 * time.Millisecond

	// extraLongThrottleLatency defines the threshold for logging requests at log level 2.
	extraLongThrottleLatency = 1 * time.Second
)

var noBackoff = &rest.NoBackoff{}

type RESTClient struct {
	Client           *http.Client
	createBackoffMgr func() rest.BackoffManager
	rateLimiter      flowcontrol.RateLimiter
}

func NewDefaultRESTClient() *RESTClient {
	return &RESTClient{
		createBackoffMgr: readExpBackoffConfig,
		rateLimiter:      flowcontrol.NewTokenBucketRateLimiter(100.00, 10),
		Client:           &http.Client{},
	}
}

func FastNewRESTClientWithProxy(baseURL, proxyUrl string) *RESTClient {
	var (
		base   *url.URL
		client *http.Client
	)
	parsedUrl, err := url.Parse(baseURL)
	if err != nil {
		base = new(url.URL)
	} else {
		base = parsedUrl
	}

	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	base.RawQuery = ""
	base.Fragment = ""

	parsedProxyUrl, err := url.Parse(proxyUrl)
	if err != nil {
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		}
	} else {
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(parsedProxyUrl),
			},
		}
	}

	return &RESTClient{
		createBackoffMgr: readExpBackoffConfig,
		rateLimiter:      flowcontrol.NewTokenBucketRateLimiter(100.00, 10),
		Client:           client,
	}
}

// checkWait returns true along with a number of seconds if the server instructed us to wait
// before retrying.
func checkWait(resp *http.Response) (int, bool) {
	switch r := resp.StatusCode; {
	// any 500 error code and 429 can trigger a wait
	case r == http.StatusTooManyRequests, r >= 500:
	default:
		return 0, false
	}
	i, ok := retryAfterSeconds(resp)
	return i, ok
}

// retryAfterSeconds returns the value of the Retry-After header and true, or 0 and false if
// the header was missing or not a valid number.
func retryAfterSeconds(resp *http.Response) (int, bool) {
	if h := resp.Header.Get("Retry-After"); len(h) > 0 {
		if i, err := strconv.Atoi(h); err == nil {
			return i, true
		}
	}
	return 0, false
}
