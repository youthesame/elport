package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
)

const (
	DefaultDialTimeout = 10 * time.Second
	DefaultIdleTimeout = 120 * time.Second
	maxRedirectHops    = 10
)

type NotFoundError struct {
	Msg string
}

func (e NotFoundError) Error() string {
	return e.Msg
}

type Option func(*options)

type options struct {
	dialTimeout time.Duration
	idleTimeout time.Duration
	rootCAs     *x509.CertPool
}

func WithTimeouts(dial, idle time.Duration) Option {
	return func(o *options) {
		if dial > 0 {
			o.dialTimeout = dial
		}
		if idle > 0 {
			o.idleTimeout = idle
		}
	}
}

func WithRootCAs(pool *x509.CertPool) Option {
	return func(o *options) {
		o.rootCAs = pool
	}
}

type Client struct {
	root        string
	base        string
	origin      string
	key         config.APIKey
	idleTimeout time.Duration
	httpClient  *http.Client

	mu       sync.Mutex
	identity map[string]any
	infoSet  bool
	infoVal  map[string]any
	infoErr  error
	catalogs map[string][]map[string]any
}

func canonicalHTTPSOrigin(u *url.URL) (string, bool) {
	if u == nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return "https://" + net.JoinHostPort(host, port), true
}

func New(baseURL string, apiKey config.APIKey, verifySSL bool, opts ...Option) (*Client, error) {
	if err := config.RequireHTTPS(baseURL); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	origin, ok := canonicalHTTPSOrigin(parsed)
	if !ok {
		return nil, fmt.Errorf("base_url must start with https://: %s", baseURL)
	}

	cfg := options{
		dialTimeout: DefaultDialTimeout,
		idleTimeout: DefaultIdleTimeout,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	dialer := &net.Dialer{
		Timeout:   cfg.dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	tlsCfg := &tls.Config{
		InsecureSkipVerify: !verifySSL,
		RootCAs:            cfg.rootCAs,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsCfg,
		TLSHandshakeTimeout:   cfg.dialTimeout,
		ResponseHeaderTimeout: cfg.idleTimeout,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}

	hc := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirectHops {
				return errors.New("stopped after 10 redirects")
			}
			nextOrigin, ok := canonicalHTTPSOrigin(req.URL)
			if !ok || nextOrigin != origin {
				return fmt.Errorf("refusing redirect across origin or scheme: %s", req.URL.Redacted())
			}
			req.Header.Set("Authorization", apiKey.Reveal())
			return nil
		},
	}

	root := strings.TrimRight(baseURL, "/")
	return &Client{
		root:        root,
		base:        root + "/api/v2",
		origin:      origin,
		key:         apiKey,
		idleTimeout: cfg.idleTimeout,
		httpClient:  hc,
		catalogs:    map[string][]map[string]any{},
	}, nil
}

func (c *Client) Root() string {
	return c.root
}

func (c *Client) KeyIDPrefix() string {
	raw := c.key.Reveal()
	dash := strings.IndexByte(raw, '-')
	if dash <= 0 {
		return ""
	}
	prefix := raw[:dash]
	for i := range len(prefix) {
		if prefix[i] < '0' || prefix[i] > '9' {
			return ""
		}
	}
	return prefix
}

func entityPath(entity string, eid int, suffix string) (string, error) {
	id, err := frontmatter.ValidateID(eid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/%s/%d%s", entity, id, suffix), nil
}

func normalizeEntity(entity map[string]any) map[string]any {
	if entity == nil {
		return map[string]any{}
	}
	if v, exists := entity["body"]; exists && v == nil {
		entity["body"] = ""
	}
	return entity
}

func readAllWithIdleTimeout(r io.Reader, idle time.Duration, cancel context.CancelFunc) ([]byte, error) {
	timer := time.AfterFunc(idle, cancel)
	defer timer.Stop()

	var out bytes.Buffer
	buf := make([]byte, 32*1024)
	for {
		timer.Reset(idle)
		n, err := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out.Bytes(), nil
			}
			return nil, err
		}
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, contentType string, body io.Reader) (int, http.Header, []byte, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	fullURL := c.base + path
	if len(query) > 0 {
		sep := "?"
		if strings.Contains(fullURL, "?") {
			sep = "&"
		}
		fullURL += sep + query.Encode()
	}

	req, err := http.NewRequestWithContext(reqCtx, method, fullURL, body)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", c.key.Reveal())
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("could not reach %s (connection failed): %w", c.root, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := readAllWithIdleTimeout(resp.Body, c.idleTimeout, cancel)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("could not reach %s (connection failed): %w", c.root, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(data)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		denied := ""
		if resp.StatusCode == http.StatusForbidden {
			denied = fmt.Sprintf("this API key may not %s %s: ", method, strings.TrimLeft(path, "/"))
		}
		msg := fmt.Sprintf("API %d: %s%s", resp.StatusCode, denied, snippet)
		if resp.StatusCode == http.StatusNotFound {
			return resp.StatusCode, resp.Header, nil, NotFoundError{Msg: msg}
		}
		return resp.StatusCode, resp.Header, nil, errors.New(msg)
	}
	return resp.StatusCode, resp.Header, data, nil
}

func decodeJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(dst)
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, payload any) (http.Header, []byte, error) {
	var body io.Reader
	ct := ""
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(raw)
		ct = "application/json"
	}
	_, hdr, data, err := c.do(ctx, method, path, query, ct, body)
	return hdr, data, err
}

func (c *Client) Me(ctx context.Context) (map[string]any, error) {
	c.mu.Lock()
	if c.identity != nil {
		res := c.identity
		c.mu.Unlock()
		return res, nil
	}
	c.mu.Unlock()

	_, data, err := c.doJSON(ctx, http.MethodGet, "/users/me", nil, nil)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.identity = res
	c.mu.Unlock()
	return res, nil
}

func (c *Client) Info(ctx context.Context) (map[string]any, error) {
	c.mu.Lock()
	if c.infoErr != nil {
		err := c.infoErr
		c.mu.Unlock()
		return nil, err
	}
	if c.infoSet {
		res := c.infoVal
		c.mu.Unlock()
		return res, nil
	}
	c.mu.Unlock()

	_, data, err := c.doJSON(ctx, http.MethodGet, "/info", nil, nil)
	if err != nil {
		c.mu.Lock()
		c.infoErr = err
		c.mu.Unlock()
		return nil, err
	}
	var res map[string]any
	if err := decodeJSON(data, &res); err != nil {
		c.mu.Lock()
		c.infoErr = err
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Lock()
	c.infoVal = res
	c.infoSet = true
	c.mu.Unlock()
	return res, nil
}

func (c *Client) APIKeys(ctx context.Context) ([]map[string]any, error) {
	_, data, err := c.doJSON(ctx, http.MethodGet, "/apikeys", nil, nil)
	if err != nil {
		return nil, err
	}
	var res []map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) Get(ctx context.Context, entity string, eid int) (map[string]any, error) {
	p, err := entityPath(entity, eid, "")
	if err != nil {
		return nil, err
	}
	_, data, err := c.doJSON(ctx, http.MethodGet, p, nil, nil)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	return normalizeEntity(res), nil
}

func idFromLocation(hdr http.Header) string {
	loc := strings.TrimRight(hdr.Get("Location"), "/")
	if loc == "" {
		return ""
	}
	if idx := strings.LastIndexByte(loc, '/'); idx >= 0 {
		return loc[idx+1:]
	}
	return loc
}

func (c *Client) Create(ctx context.Context, entity, title string) (map[string]any, error) {
	payload := map[string]any{
		"title":        title,
		"body":         "",
		"content_type": 2,
	}
	hdr, data, err := c.doJSON(ctx, http.MethodPost, "/"+entity, nil, payload)
	if err != nil {
		return nil, err
	}
	res := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		_ = decodeJSON(data, &res)
	}
	if _, hasID := res["id"]; !hasID {
		if locID := idFromLocation(hdr); locID != "" {
			res["id"] = locID
		}
	}
	return res, nil
}

func (c *Client) Patch(ctx context.Context, entity string, eid int, payload map[string]any) (map[string]any, error) {
	p, err := entityPath(entity, eid, "")
	if err != nil {
		return nil, err
	}
	_, data, err := c.doJSON(ctx, http.MethodPatch, p, nil, payload)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return map[string]any{}, nil
	}
	return normalizeEntity(res), nil
}

func (c *Client) Uploads(ctx context.Context, entity string, eid int) ([]map[string]any, error) {
	p, err := entityPath(entity, eid, "/uploads")
	if err != nil {
		return nil, err
	}
	_, data, err := c.doJSON(ctx, http.MethodGet, p, nil, nil)
	if err != nil {
		return nil, err
	}
	var res []map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) Comments(ctx context.Context, entity string, eid int) ([]map[string]any, error) {
	p, err := entityPath(entity, eid, "/comments")
	if err != nil {
		return nil, err
	}
	_, data, err := c.doJSON(ctx, http.MethodGet, p, nil, nil)
	if err != nil {
		return nil, err
	}
	var res []map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) Upload(ctx context.Context, entity string, eid int, approved plan.ApprovedUploadPath) (map[string]any, error) {
	if !approved.Valid() {
		return nil, errors.New("refusing unapproved upload path")
	}
	p, err := entityPath(entity, eid, "/uploads")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(approved.Path())
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filepath.Base(approved.Path()))
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.CloseWithError(mw.Close())
	}()

	_, hdr, data, err := c.do(ctx, http.MethodPost, p, nil, mw.FormDataContentType(), pr)
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	var res map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return map[string]any{"id": idFromLocation(hdr)}, nil
	}
	return res, nil
}

func (c *Client) Download(ctx context.Context, entity string, eid, uid int) ([]byte, error) {
	validUID, err := frontmatter.ValidateID(uid)
	if err != nil {
		return nil, err
	}
	p, err := entityPath(entity, eid, fmt.Sprintf("/uploads/%d?format=binary", validUID))
	if err != nil {
		return nil, err
	}
	_, _, data, err := c.do(ctx, http.MethodGet, p, nil, "", nil)
	return data, err
}

func (c *Client) AddTag(ctx context.Context, entity string, eid int, tag string) error {
	p, err := entityPath(entity, eid, "/tags")
	if err != nil {
		return err
	}
	_, _, err = c.doJSON(ctx, http.MethodPost, p, nil, map[string]any{"tag": tag})
	return err
}

func (c *Client) AddLink(ctx context.Context, entity string, eid int, targetEntity string, targetID int) error {
	validTarget, err := frontmatter.ValidateID(targetID)
	if err != nil {
		return err
	}
	p, err := entityPath(entity, eid, fmt.Sprintf("/%s_links/%d", targetEntity, validTarget))
	if err != nil {
		return err
	}
	_, _, err = c.doJSON(ctx, http.MethodPost, p, nil, map[string]any{})
	return err
}

func (c *Client) AddComment(ctx context.Context, entity string, eid int, text string) error {
	p, err := entityPath(entity, eid, "/comments")
	if err != nil {
		return err
	}
	_, _, err = c.doJSON(ctx, http.MethodPost, p, nil, map[string]any{"comment": text})
	return err
}

func (c *Client) catalog(ctx context.Context, teamID any, resource string) ([]map[string]any, error) {
	key := fmt.Sprintf("%v|%s", teamID, resource)
	c.mu.Lock()
	if cached, ok := c.catalogs[key]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	_, data, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/teams/%v/%s", teamID, resource), nil, nil)
	if err != nil {
		return nil, err
	}
	var res []map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.catalogs[key] = res
	c.mu.Unlock()
	return res, nil
}

func (c *Client) Categories(ctx context.Context, teamID any, entity string) ([]map[string]any, error) {
	resource := "resources_categories"
	if entity == "experiments" {
		resource = "experiments_categories"
	}
	return c.catalog(ctx, teamID, resource)
}

func (c *Client) Statuses(ctx context.Context, teamID any, entity string) ([]map[string]any, error) {
	resource := "items_status"
	if entity == "experiments" {
		resource = "experiments_status"
	}
	return c.catalog(ctx, teamID, resource)
}

func (c *Client) Search(ctx context.Context, entity string, params url.Values) ([]map[string]any, error) {
	_, data, err := c.doJSON(ctx, http.MethodGet, "/"+entity, params, nil)
	if err != nil {
		return nil, err
	}
	var res []map[string]any
	if err := decodeJSON(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}
