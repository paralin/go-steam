package steam

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSteamDirectoryInitializeReturnsHTTPStatusBeforeJSONDecode(t *testing.T) {
	oldTransport := http.DefaultTransport
	body := &trackingReadCloser{r: strings.NewReader("not json"), maxChunk: 1}
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})
	defer func() { http.DefaultTransport = oldTransport }()

	err := new(steamDirectory).Initialize()
	if err == nil {
		t.Fatal("expected status error")
	}
	if err.Error() != "got non-200 response from Steam: status=502 body=not json" {
		t.Fatalf("error = %q", err)
	}
	if !body.sawEOF {
		t.Fatal("expected response body to be fully drained")
	}
	if !body.closed {
		t.Fatal("expected response body to be closed")
	}
}

func TestSteamDirectoryInitializeDrainsSuccessfulResponseBody(t *testing.T) {
	oldTransport := http.DefaultTransport
	body := &trackingReadCloser{
		r:        strings.NewReader(`{"response":{"serverlist":["127.0.0.1:27017"],"result":1}}` + strings.Repeat(" ", 4096)),
		maxChunk: 1,
	}
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})
	defer func() { http.DefaultTransport = oldTransport }()

	if err := new(steamDirectory).Initialize(); err != nil {
		t.Fatal(err)
	}
	if !body.sawEOF {
		t.Fatal("expected response body to be fully drained")
	}
	if !body.closed {
		t.Fatal("expected response body to be closed")
	}
}

type trackingReadCloser struct {
	r        *strings.Reader
	maxChunk int
	sawEOF   bool
	closed   bool
}

func (b *trackingReadCloser) Read(p []byte) (int, error) {
	if b.maxChunk > 0 && len(p) > b.maxChunk {
		p = p[:b.maxChunk]
	}
	n, err := b.r.Read(p)
	if err == io.EOF {
		b.sawEOF = true
	}
	return n, err
}

func (b *trackingReadCloser) Close() error {
	b.closed = true
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
