package network

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/deskaone/deskaone-sdk/proxy"
)

const defaultHTTPUserAgent = "DesKaOne/0.0.1"

type HTTPClient struct {
	Timeout   time.Duration
	Picker    proxy.ProxyPicker
	LocalBind *LocalBindTCP

	UserAgent string

	MaxRedirects int
	MaxRetries   int
	RetryDelay   time.Duration

	AutoDecompress bool
	MaxBodySize    int64

	Debug func(HTTPDebugInfo)
}

type HTTPDebugInfo struct {
	Method     string
	URL        string
	ProxyUsed  bool
	StatusCode int
	Error      error
	Attempt    int
	Elapsed    time.Duration
}

type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       []byte
}

type HTTPStreamResponse struct {
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       io.ReadCloser
	Client     *TCPClient
}

func NewHTTPClient(
	timeout time.Duration,
	picker proxy.ProxyPicker,
	localBind ...*LocalBindTCP,
) *HTTPClient {
	return &HTTPClient{
		Timeout:        timeout,
		Picker:         picker,
		LocalBind:      firstHTTPClientLocalBind(localBind...),
		UserAgent:      defaultHTTPUserAgent,
		MaxRedirects:   5,
		MaxRetries:     0,
		RetryDelay:     300 * time.Millisecond,
		AutoDecompress: true,
		MaxBodySize:    0,
	}
}

func (c *HTTPClient) Get(ctx context.Context, rawURL string, headers map[string]string) (*HTTPResponse, error) {
	return c.Do(ctx, &HTTPRequest{
		Method:  "GET",
		URL:     rawURL,
		Headers: headers,
	})
}

func (c *HTTPClient) Delete(ctx context.Context, rawURL string, headers map[string]string) (*HTTPResponse, error) {
	return c.Do(ctx, &HTTPRequest{
		Method:  "DELETE",
		URL:     rawURL,
		Headers: headers,
	})
}

func (c *HTTPClient) Post(
	ctx context.Context,
	rawURL string,
	headers map[string]string,
	body []byte,
) (*HTTPResponse, error) {
	return c.Do(ctx, &HTTPRequest{
		Method:  "POST",
		URL:     rawURL,
		Headers: headers,
		Body:    body,
	})
}

func (c *HTTPClient) Put(
	ctx context.Context,
	rawURL string,
	headers map[string]string,
	body []byte,
) (*HTTPResponse, error) {
	return c.Do(ctx, &HTTPRequest{
		Method:  "PUT",
		URL:     rawURL,
		Headers: headers,
		Body:    body,
	})
}

func (c *HTTPClient) Do(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("nil HTTPClient")
	}

	if req == nil {
		return nil, fmt.Errorf("nil HTTP request")
	}

	currentReq := cloneHTTPRequest(req)

	var lastErr error

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		start := time.Now()

		res, err := c.doWithRedirects(ctx, currentReq)

		elapsed := time.Since(start)
		if c.Debug != nil {
			statusCode := 0
			if res != nil {
				statusCode = res.StatusCode
			}

			c.Debug(HTTPDebugInfo{
				Method:     currentReq.Method,
				URL:        currentReq.URL,
				ProxyUsed:  c.Picker != nil,
				StatusCode: statusCode,
				Error:      err,
				Attempt:    attempt + 1,
				Elapsed:    elapsed,
			})
		}

		if err == nil {
			return res, nil
		}

		lastErr = err

		if !isRetryableHTTPError(err) {
			break
		}

		if attempt < c.MaxRetries && c.RetryDelay > 0 {
			select {
			case <-ctxDone(ctx):
				return nil, contextError(ctx)
			case <-time.After(c.RetryDelay):
			}
		}
	}

	return nil, lastErr
}

func (c *HTTPClient) DoStream(ctx context.Context, req *HTTPRequest) (*HTTPStreamResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("nil HTTPClient")
	}

	if req == nil {
		return nil, fmt.Errorf("nil HTTP request")
	}

	currentReq := cloneHTTPRequest(req)

	var lastErr error

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		start := time.Now()

		res, err := c.doStreamWithRedirects(ctx, currentReq)

		elapsed := time.Since(start)
		if c.Debug != nil {
			statusCode := 0
			if res != nil {
				statusCode = res.StatusCode
			}

			c.Debug(HTTPDebugInfo{
				Method:     currentReq.Method,
				URL:        currentReq.URL,
				ProxyUsed:  c.Picker != nil,
				StatusCode: statusCode,
				Error:      err,
				Attempt:    attempt + 1,
				Elapsed:    elapsed,
			})
		}

		if err == nil {
			return res, nil
		}

		lastErr = err

		if !isRetryableHTTPError(err) {
			break
		}

		if attempt < c.MaxRetries && c.RetryDelay > 0 {
			select {
			case <-ctxDone(ctx):
				return nil, contextError(ctx)
			case <-time.After(c.RetryDelay):
			}
		}
	}

	return nil, lastErr
}

func (c *HTTPClient) doWithRedirects(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error) {
	maxRedirects := c.MaxRedirects
	if maxRedirects < 0 {
		maxRedirects = 0
	}

	currentReq := cloneHTTPRequest(req)

	for redirectCount := 0; redirectCount <= maxRedirects; redirectCount++ {
		res, err := c.doOnce(ctx, currentReq)
		if err != nil {
			return nil, err
		}

		if !isRedirectStatus(res.StatusCode) {
			return res, nil
		}

		location := getHeader(res.Headers, "Location")
		if location == "" {
			return res, nil
		}

		nextURL, err := resolveRedirectURL(currentReq.URL, location)
		if err != nil {
			return nil, err
		}

		currentReq = nextRedirectRequest(currentReq, nextURL, res.StatusCode)
	}

	return nil, fmt.Errorf("too many redirects")
}

func (c *HTTPClient) doStreamWithRedirects(ctx context.Context, req *HTTPRequest) (*HTTPStreamResponse, error) {
	maxRedirects := c.MaxRedirects
	if maxRedirects < 0 {
		maxRedirects = 0
	}

	currentReq := cloneHTTPRequest(req)

	for redirectCount := 0; redirectCount <= maxRedirects; redirectCount++ {
		res, err := c.doStreamOnce(ctx, currentReq)
		if err != nil {
			return nil, err
		}

		if !isRedirectStatus(res.StatusCode) {
			return res, nil
		}

		location := getHeader(res.Headers, "Location")
		if location == "" {
			return res, nil
		}

		_ = res.Body.Close()

		nextURL, err := resolveRedirectURL(currentReq.URL, location)
		if err != nil {
			return nil, err
		}

		currentReq = nextRedirectRequest(currentReq, nextURL, res.StatusCode)
	}

	return nil, fmt.Errorf("too many redirects")
}

func (c *HTTPClient) doOnce(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error) {
	streamRes, err := c.doStreamOnce(ctx, req)
	if err != nil {
		return nil, err
	}
	defer streamRes.Body.Close()

	var reader io.Reader = streamRes.Body
	if c.MaxBodySize > 0 {
		reader = io.LimitReader(reader, c.MaxBodySize)
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return &HTTPResponse{
		StatusCode: streamRes.StatusCode,
		Status:     streamRes.Status,
		Headers:    streamRes.Headers,
		Body:       body,
	}, nil
}

func (c *HTTPClient) doStreamOnce(ctx context.Context, req *HTTPRequest) (*HTTPStreamResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return nil, err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("empty URL host")
	}

	port := defaultPortByScheme(u.Scheme)
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil {
			return nil, fmt.Errorf("invalid URL port: %s", u.Port())
		}
	}

	isTLS := u.Scheme == "https"

	client, err := NewTCPClientAutoTLS(
		ctx,
		host,
		port,
		c.Timeout,
		c.Picker,
		isTLS,
		c.LocalBind,
	)
	if err != nil {
		return nil, err
	}

	rawRequest := buildRawHTTPRequest(method, u, req.Headers, req.Body, c.UserAgent, c.AutoDecompress)

	if err := client.WriteFull(rawRequest); err != nil {
		_ = client.Close()
		return nil, err
	}

	if c.Timeout > 0 {
		_ = client.conn.SetReadDeadline(time.Now().Add(c.Timeout))
	}

	reader := bufio.NewReader(client.conn)

	statusLine, err := readHTTPLine(reader)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	statusCode, err := parseHTTPStatusCode(statusLine)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	headers, err := readHTTPHeaders(reader)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	bodyReader, err := buildHTTPBodyReader(reader, headers)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	if c.AutoDecompress {
		bodyReader, err = wrapDecompressReader(bodyReader, headers)
		if err != nil {
			_ = client.Close()
			return nil, err
		}
	}

	return &HTTPStreamResponse{
		StatusCode: statusCode,
		Status:     statusLine,
		Headers:    headers,
		Body: &httpBodyCloser{
			reader: bodyReader,
			closeFn: func() error {
				if c.Timeout > 0 {
					_ = client.conn.SetReadDeadline(time.Time{})
				}
				return client.Close()
			},
		},
		Client: client,
	}, nil
}

func buildRawHTTPRequest(
	method string,
	u *url.URL,
	headers map[string]string,
	body []byte,
	userAgent string,
	autoDecompress bool,
) []byte {
	var b bytes.Buffer

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	hostHeader := u.Host
	if hostHeader == "" {
		hostHeader = u.Hostname()
	}

	b.WriteString(method)
	b.WriteByte(' ')
	b.WriteString(path)
	b.WriteString(" HTTP/1.1\r\n")

	writeHeaderIfMissing(&b, headers, "Host", hostHeader)

	if userAgent == "" {
		userAgent = defaultHTTPUserAgent
	}
	writeHeaderIfMissing(&b, headers, "User-Agent", userAgent)

	writeHeaderIfMissing(&b, headers, "Accept", "*/*")
	writeHeaderIfMissing(&b, headers, "Connection", "close")

	if autoDecompress {
		writeHeaderIfMissing(&b, headers, "Accept-Encoding", "gzip, deflate")
	}

	if len(body) > 0 {
		writeHeaderIfMissing(&b, headers, "Content-Length", strconv.Itoa(len(body)))
	}

	for k, v := range headers {
		key := strings.TrimSpace(k)
		value := strings.TrimSpace(v)

		if key == "" {
			continue
		}

		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\r\n")
	}

	b.WriteString("\r\n")

	if len(body) > 0 {
		b.Write(body)
	}

	return b.Bytes()
}

func writeHeaderIfMissing(
	b *bytes.Buffer,
	headers map[string]string,
	key string,
	value string,
) {
	if hasHeader(headers, key) {
		return
	}

	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

func hasHeader(headers map[string]string, key string) bool {
	for k := range headers {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return true
		}
	}

	return false
}

func readHTTPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimRight(line, "\r\n"), nil
}

func parseHTTPStatusCode(statusLine string) (int, error) {
	parts := strings.Fields(statusLine)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid HTTP status line: %q", statusLine)
	}

	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid HTTP status code: %q", parts[1])
	}

	return code, nil
}

func readHTTPHeaders(reader *bufio.Reader) (map[string]string, error) {
	headers := make(map[string]string)

	for {
		line, err := readHTTPLine(reader)
		if err != nil {
			return nil, err
		}

		if line == "" {
			break
		}

		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		if old, ok := headers[key]; ok {
			headers[key] = old + ", " + value
		} else {
			headers[key] = value
		}
	}

	return headers, nil
}

func buildHTTPBodyReader(reader *bufio.Reader, headers map[string]string) (io.Reader, error) {
	transferEncoding := getHeader(headers, "Transfer-Encoding")

	if strings.Contains(strings.ToLower(transferEncoding), "chunked") {
		return &chunkedBodyReader{
			reader: reader,
		}, nil
	}

	contentLength := getHeader(headers, "Content-Length")
	if contentLength != "" {
		n, err := strconv.Atoi(strings.TrimSpace(contentLength))
		if err != nil {
			return nil, fmt.Errorf("invalid Content-Length: %s", contentLength)
		}

		if n <= 0 {
			return bytes.NewReader(nil), nil
		}

		return io.LimitReader(reader, int64(n)), nil
	}

	return reader, nil
}

func wrapDecompressReader(reader io.Reader, headers map[string]string) (io.Reader, error) {
	encoding := strings.ToLower(strings.TrimSpace(getHeader(headers, "Content-Encoding")))

	switch encoding {
	case "":
		return reader, nil

	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		return gz, nil

	case "deflate":
		return flate.NewReader(reader), nil

	default:
		return reader, nil
	}
}

type chunkedBodyReader struct {
	reader *bufio.Reader

	currentRemaining int64
	done             bool
}

func (r *chunkedBodyReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}

	for r.currentRemaining == 0 {
		line, err := readHTTPLine(r.reader)
		if err != nil {
			return 0, err
		}

		sizeText := line
		if idx := strings.Index(sizeText, ";"); idx >= 0 {
			sizeText = sizeText[:idx]
		}

		sizeText = strings.TrimSpace(sizeText)

		size, err := strconv.ParseInt(sizeText, 16, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid chunk size: %q", sizeText)
		}

		if size == 0 {
			for {
				trailerLine, err := readHTTPLine(r.reader)
				if err != nil {
					return 0, err
				}

				if trailerLine == "" {
					break
				}
			}

			r.done = true
			return 0, io.EOF
		}

		if size < 0 {
			return 0, fmt.Errorf("invalid negative chunk size")
		}

		r.currentRemaining = size
	}

	if int64(len(p)) > r.currentRemaining {
		p = p[:r.currentRemaining]
	}

	n, err := r.reader.Read(p)
	r.currentRemaining -= int64(n)

	if err != nil {
		return n, err
	}

	if r.currentRemaining == 0 {
		crlf := make([]byte, 2)
		if _, err := io.ReadFull(r.reader, crlf); err != nil {
			return n, err
		}

		if crlf[0] != '\r' || crlf[1] != '\n' {
			return n, fmt.Errorf("invalid chunk delimiter")
		}
	}

	return n, nil
}

type httpBodyCloser struct {
	reader  io.Reader
	closeFn func() error
}

func (c *httpBodyCloser) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *httpBodyCloser) Close() error {
	if closer, ok := c.reader.(io.Closer); ok {
		_ = closer.Close()
	}

	if c.closeFn != nil {
		return c.closeFn()
	}

	return nil
}

func getHeader(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return v
		}
	}

	return ""
}

func defaultPortByScheme(scheme string) int {
	switch strings.ToLower(scheme) {
	case "https":
		return 443
	default:
		return 80
	}
}

func firstHTTPClientLocalBind(bind ...*LocalBindTCP) *LocalBindTCP {
	if len(bind) == 0 {
		return nil
	}

	return bind[0]
}

func cloneHTTPRequest(req *HTTPRequest) *HTTPRequest {
	if req == nil {
		return nil
	}

	headers := make(map[string]string, len(req.Headers))
	for k, v := range req.Headers {
		headers[k] = v
	}

	body := append([]byte(nil), req.Body...)

	return &HTTPRequest{
		Method:  req.Method,
		URL:     req.URL,
		Headers: headers,
		Body:    body,
	}
}

func isRedirectStatus(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	default:
		return false
	}
}

func resolveRedirectURL(currentURL string, location string) (string, error) {
	base, err := url.Parse(currentURL)
	if err != nil {
		return "", err
	}

	next, err := url.Parse(strings.TrimSpace(location))
	if err != nil {
		return "", err
	}

	return base.ResolveReference(next).String(), nil
}

func nextRedirectRequest(req *HTTPRequest, nextURL string, statusCode int) *HTTPRequest {
	next := cloneHTTPRequest(req)
	next.URL = nextURL

	method := strings.ToUpper(strings.TrimSpace(next.Method))

	if statusCode == 303 || ((statusCode == 301 || statusCode == 302) && method == "POST") {
		next.Method = "GET"
		next.Body = nil

		removeHeader(next.Headers, "Content-Length")
		removeHeader(next.Headers, "Content-Type")
	}

	removeHeader(next.Headers, "Host")

	return next
}

func removeHeader(headers map[string]string, key string) {
	for k := range headers {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			delete(headers, k)
		}
	}
}

func isRetryableHTTPError(err error) bool {
	if err == nil {
		return false
	}

	if err == io.EOF {
		return true
	}

	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "proxy refused") ||
		strings.Contains(msg, "socks connect failed")
}

func ctxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return neverDone()
	}

	return ctx.Done()
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return context.Canceled
}

func neverDone() <-chan struct{} {
	ch := make(chan struct{})
	return ch
}
