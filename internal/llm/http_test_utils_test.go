package llm

import (
	"io"
	"net/http"
	"strings"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestHTTPClient(fn roundTripperFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func newTestHTTPResponse(req *http.Request, status int, contentType, body string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp
}
