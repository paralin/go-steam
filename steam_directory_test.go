package steam

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSteamDirectoryInitializeReturnsHTTPStatusBeforeJSONDecode(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("not json")),
		}, nil
	})
	defer func() { http.DefaultTransport = oldTransport }()

	err := new(steamDirectory).Initialize()
	if err == nil {
		t.Fatal("expected status error")
	}
	if err.Error() != "steam directory request failed with status 502 Bad Gateway" {
		t.Fatalf("error = %q", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
