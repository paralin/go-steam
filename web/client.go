package web

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aperturerobotics/fastjson"
	"github.com/pkg/errors"
)

// BaseURL is Valve's public Web API endpoint.
const BaseURL = "https://api.steampowered.com"

// Config supplies immutable connection settings for a Web API client.
type Config struct {
	// APIKey authenticates requests; an empty key permits public endpoints.
	APIKey string
	// BaseURL selects the API origin; empty uses Valve's public endpoint.
	BaseURL string
	// HTTPClient supplies transport and timeout settings; nil uses a 30-second client.
	HTTPClient *http.Client
	// MaxResponseBytes bounds decoded response allocation; zero permits 64 MiB.
	MaxResponseBytes int64
}

// Client owns Web API connection settings and is safe for concurrent requests.
// It does not retry mutations or retain results; callers own caching and retries.
type Client struct {
	// apiKey authenticates this client's calls without global mutable state.
	apiKey string
	// base is the immutable API endpoint.
	base string
	// http executes requests with redirects disabled to protect credentials.
	http *http.Client
	// maxResponseBytes bounds every response, including failures.
	maxResponseBytes int64
}

// NewClient validates settings and copies the supplied HTTP client before use.
func NewClient(config Config) (*Client, error) {
	// Resolve one API origin, excluding query credentials and ambiguous paths.
	endpoint := config.BaseURL
	if endpoint == "" {
		endpoint = BaseURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid Steam Web API base URL")
	}
	if config.MaxResponseBytes < 0 {
		return nil, errors.New("negative Web API response limit")
	}
	limit := config.MaxResponseBytes
	if limit == 0 {
		limit = 64 << 20
	}

	// Keep caller transports while preventing redirects from forwarding API keys.
	transport := http.Client{Timeout: 30 * time.Second}
	if config.HTTPClient != nil {
		transport = *config.HTTPClient
	}
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		apiKey:           config.APIKey,
		base:             strings.TrimRight(endpoint, "/"),
		http:             &transport,
		maxResponseBytes: limit,
	}, nil
}

// Call performs one Web API operation and returns its parsed JSON document.
// Parameters are copied. POST uses form data; credentials never appear in errors.
// Returned values remain valid independently of later calls.
func (c *Client) Call(ctx context.Context, verb, iface, method string, version uint, params url.Values) (*fastjson.Value, error) {
	// Limit path components to API identifiers so parameters cannot change origin.
	for _, component := range []string{iface, method} {
		if component == "" || strings.IndexFunc(component, func(r rune) bool {
			return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
		}) >= 0 {
			return nil, errors.New("invalid Steam Web API method")
		}
	}
	if verb != http.MethodGet && verb != http.MethodPost {
		return nil, errors.New("unsupported Steam Web API HTTP method")
	}
	operation := iface + "/" + method
	values := make(url.Values, len(params)+2)
	for key, items := range params {
		values[key] = append([]string(nil), items...)
	}
	values.Set("format", "json")
	if c.apiKey != "" {
		values.Set("key", c.apiKey)
	}
	endpoint := c.base + "/" + operation + "/v" + strconv.FormatUint(uint64(version), 10) + "/"
	var body io.Reader
	if verb == http.MethodGet {
		endpoint += "?" + values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}

	// The request context owns network cancellation and body reads.
	request, err := http.NewRequestWithContext(ctx, verb, endpoint, body)
	if err != nil {
		return nil, &RequestError{Operation: operation, Err: errors.New("invalid request")}
	}
	if verb == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, &RequestError{Operation: operation, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &RequestError{Operation: operation, StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}

	// Reject oversized or malformed documents before exposing provider data.
	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, &RequestError{Operation: operation, Err: err}
	}
	if int64(len(data)) > c.maxResponseBytes {
		return nil, &RequestError{Operation: operation, Err: errors.New("response exceeds size limit")}
	}
	parser := &fastjson.Parser{}
	value, err := parser.ParseBytes(data)
	if err != nil {
		return nil, &RequestError{Operation: operation, Err: errors.New("invalid JSON response")}
	}
	if _, err := value.Object(); err != nil {
		return nil, &RequestError{Operation: operation, Err: errors.New("expected response object")}
	}
	return value, nil
}

// JSONMessage accepts generated protobuf-go-lite JSON codecs.
type JSONMessage interface {
	// Reset clears fields omitted by a later response.
	Reset()
	// UnmarshalJSON decodes one complete typed record.
	UnmarshalJSON([]byte) error
}

// Decode reads a required response object using its generated JSON codec.
func Decode(value *fastjson.Value, target JSONMessage, path ...string) error {
	// Require the selected record before replacing the caller's destination.
	selected := value.Get(path...)
	if selected == nil || selected.Type() == fastjson.TypeNull {
		return errors.New("Steam Web API response is missing the requested record")
	}

	// Replace omitted fields as well as fields present in the current response.
	target.Reset()
	if err := target.UnmarshalJSON(selected.MarshalTo(nil)); err != nil {
		return errors.New("invalid Steam Web API record")
	}
	return nil
}
