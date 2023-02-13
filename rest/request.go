package rest

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http2"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
)

const (
	// Environment variables: Note that the duration should be long enough that the backoff
	// persists for some reasonable time (i.e. 120 seconds).  The typical base might be "1".
	envBackoffBase     = "KUBE_CLIENT_BACKOFF_BASE"
	envBackoffDuration = "KUBE_CLIENT_BACKOFF_DURATION"
	defaultMaxRerryNum = 10
)

var _ RequestInterface = &Request{}

type Request struct {
	c *RESTClient

	maxRetries  int
	backoff     rest.BackoffManager
	rateLimiter flowcontrol.RateLimiter

	authenticated bool
	verb          string
	body          io.Reader
	header        map[string]string
	url           *url.URL
	Err           error
	timeout       time.Duration
}

type Response struct {
	Code int
	Body []byte
	Err  error
}

func (r *Request) Verb(verb string) *Request {
	r.verb = verb
	return r
}

func (r *Request) Get() *Request {
	return r.Verb("GET")
}

func (r *Request) Post() *Request {
	return r.Verb("POST")
}

func (r *Request) Delete() *Request {
	return r.Verb("DELETE")
}

func (r *Request) Put() *Request {
	return r.Verb("PUT")
}

func stringToURL(baseURL string) (base *url.URL) {
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
	return
}

func NewRequestWithClient(c *RESTClient) *Request {
	var timeout time.Duration
	if c.Client != nil {
		timeout = c.Client.Timeout
	}
	var backoff rest.BackoffManager
	if c.createBackoffMgr != nil {
		backoff = c.createBackoffMgr()
	}
	if backoff == nil {
		backoff = noBackoff
	}
	return &Request{
		c:          c,
		backoff:    backoff,
		header:     make(map[string]string),
		body:       nil,
		maxRetries: defaultMaxRerryNum,
		timeout:    timeout,
	}
}

func FastRequest(baseURL string) *Request {
	var c = NewDefaultRESTClient()
	var backoff = c.createBackoffMgr()
	if backoff == nil {
		backoff = noBackoff
	}
	return &Request{
		url:        stringToURL(baseURL),
		c:          c,
		backoff:    backoff,
		header:     make(map[string]string),
		body:       nil,
		maxRetries: defaultMaxRerryNum,
		timeout:    c.Client.Timeout,
	}
}

func NewDefaultRequest() *Request {
	var c = NewDefaultRESTClient()
	var backoff = c.createBackoffMgr()

	if backoff == nil {
		backoff = noBackoff
	}

	return &Request{
		c:          c,
		backoff:    backoff,
		header:     make(map[string]string),
		body:       nil,
		maxRetries: defaultMaxRerryNum,
		timeout:    c.Client.Timeout,
	}
}

func (r *Request) URL(baseURL string) *Request {
	r.url = stringToURL(baseURL)
	return r
}

func (r *Request) Host(baseHost string) *Request {
	r.url = stringToURL(baseHost)
	return r
}

func (r *Request) Path(basePath string) *Request {

	if !strings.HasSuffix(basePath, "/") {
		r.url.Path += "/"
	}
	r.url.RawQuery = ""
	r.url.Fragment = ""
	r.url.Path = basePath
	return r
}

func (r *Request) tryThrottle(ctx context.Context) error {
	if r.rateLimiter == nil {
		return nil
	}

	now := time.Now()
	err := r.rateLimiter.Wait(ctx)

	latency := time.Since(now)
	if latency > longThrottleLatency {
		klog.V(3).Infof("Throttling request took %v, request: %s:%s", latency, r.verb, r.url)
	}

	if latency > extraLongThrottleLatency {
		// If the rate limiter latency is very high, the log message should be printed at a higher log level,
		// but we use a throttled logger to prevent spamming.
		klog.V(3).Infof("Throttling request took %v, request: %s:%s", latency, r.verb, r.url)
	}

	return err
}

func (r *Request) setHeader(req *http.Request) {
	for k, v := range r.header {
		req.Header.Add(k, v)
	}
}

func (r *Request) request(ctx context.Context, fn func(*http.Response)) error {

	if r.Err != nil {
		klog.V(4).Infof("Error in request: %v", r.Err)
		return r.Err
	}

	client := r.c.Client
	if client == nil {
		client = http.DefaultClient
	}

	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	// Right now we make about ten retry attempts if we get a Retry-After response.
	retries := 0
	for {
		req, err := http.NewRequest(r.verb, r.url.String(), r.body)
		if err != nil {
			return r.Err
		}
		req = req.WithContext(ctx)

		r.setHeader(req)

		r.backoff.Sleep(r.backoff.CalculateBackoff(r.url))
		if retries > 0 {
			// We are retrying the request that we already send to apiserver
			// at least once before.
			// This request should also be throttled with the client-internal rate limiter.
			if err := r.tryThrottle(ctx); err != nil {
				return r.Err
			}
		}
		resp, err := client.Do(req)

		if err != nil {
			// "Connection reset by peer" or "apiserver is shutting down" are usually a transient errors.
			// Thus in case of "GET" operations, we simply retry it.
			// We are not automatically retrying "write" operations, as
			// they are not idempotent.
			if r.verb != "GET" {
				return err
			}
			// For connection errors and apiserver shutdown errors retry.
			if net.IsConnectionReset(err) || net.IsProbableEOF(err) {
				// For the purpose of retry, we set the artificial "retry-after" response.
				// TODO: Should we clean the original response if it exists?
				resp = &http.Response{
					StatusCode: http.StatusInternalServerError,
					Header:     http.Header{"Retry-After": []string{"1"}},
					Body:       ioutil.NopCloser(bytes.NewReader([]byte{})),
				}
			} else {
				return err
			}
		}
		done := func() bool {
			// Ensure the response body is fully read and closed
			// before we reconnect, so that we reuse the same TCP
			// connection.
			defer func() {
				const maxBodySlurpSize = 2 << 10
				if resp.ContentLength <= maxBodySlurpSize {
					io.Copy(ioutil.Discard, &io.LimitedReader{R: resp.Body, N: maxBodySlurpSize})
				}
				resp.Body.Close()
			}()

			retries++
			if seconds, wait := checkWait(resp); wait && retries <= r.maxRetries {
				if seeker, ok := r.body.(io.Seeker); ok && r.body != nil {
					_, err := seeker.Seek(0, 0)
					if err != nil {
						logerr := fmt.Sprintf("Could not retry request, can't Seek() back to beginning of body for %T", r.body)
						klog.V(4).Info(logerr)
						fn(resp)
						return true
					}
				}

				klog.V(4).Infof("Got a Retry-After %ds response for attempt %d to %v", seconds, retries, r.url)
				r.backoff.Sleep(time.Duration(seconds) * time.Second)
				return false
			}
			fn(resp)
			return true
		}()

		if done {
			return nil
		}
	}
}

func (r *Request) Do(ctx context.Context) Response {
	var resp Response
	err := r.request(ctx, func(response *http.Response) {
		resp = r.transformResponse(response)
	})
	if err != nil {
		return Response{Err: err}
	}
	return resp
}

func (r *Request) AddHeader(key, value string) *Request {
	r.header[key] = value
	return r
}

func (r *Request) Body(payload []byte) *Request {
	r.body = bytes.NewReader(payload)
	return r
}

func (r *Request) BasicAuth(username, password string) *Request {
	if !r.authenticated {
		r.header["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
		r.authenticated = true
	}
	return r
}

func (r *Request) BearerToken(token string) *Request {
	if !r.authenticated {
		r.header["Authorization"] = "Bearer " + token
		r.authenticated = true
	}
	return r
}

func (r *Request) transformResponse(resp *http.Response) Response {
	var body []byte
	if resp.Body != nil {
		data, err := ioutil.ReadAll(resp.Body)
		switch err.(type) {
		case nil:
			body = data
		case http2.StreamError:
			// This is trying to catch the scenario that the server may close the connection when sending the
			// response body. This can be caused by server timeout due to a slow network connection.
			// TODO: Add test for this. Steps may be:
			// 1. client-go (or kubectl) sends a GET request.
			// 2. Apiserver sends back the headers and then part of the body
			// 3. Apiserver closes connection.
			// 4. client-go should catch this and return an error.
			klog.V(2).Infof("Stream error %#v when reading response body, may be caused by closed connection.", err)
			streamErr := fmt.Errorf("stream error when reading response body, may be caused by closed connection. Please retry. Original error: %v", err)
			return Response{
				Err: streamErr,
			}
		default:
			klog.Errorf("Unexpected error when reading response body: %v", err)
			unexpectedErr := fmt.Errorf("unexpected error when reading response body. Please retry. Original error: %v", err)
			return Response{
				Err: unexpectedErr,
			}
		}
	}

	// cannot verify the content type is accurate

	contentType := resp.Header.Get("Content-Type")
	if len(contentType) > 0 {
		// if we fail to negotiate a decoder, treat this as an unstructured error
		switch {
		case resp.StatusCode == http.StatusSwitchingProtocols:
			// no-op, we've been upgraded
		case resp.StatusCode >= http.StatusBadRequest && resp.StatusCode <= http.StatusNotFound:
			return Response{Err: errors.New("Bad request")}
		case resp.StatusCode == http.StatusConflict:
			return Response{Err: errors.New("The specified resource already exists")}
		}
		return Response{
			Body: body,
			Code: resp.StatusCode,
		}
	}
	return Response{}
}

func readExpBackoffConfig() rest.BackoffManager {
	backoffBase := os.Getenv(envBackoffBase)
	backoffDuration := os.Getenv(envBackoffDuration)

	backoffBaseInt, errBase := strconv.ParseInt(backoffBase, 10, 64)
	backoffDurationInt, errDuration := strconv.ParseInt(backoffDuration, 10, 64)
	if errBase != nil || errDuration != nil {
		return &rest.NoBackoff{}
	}
	return &rest.URLBackoff{
		Backoff: flowcontrol.NewBackOff(
			time.Duration(backoffBaseInt)*time.Second,
			time.Duration(backoffDurationInt)*time.Second)}
}
