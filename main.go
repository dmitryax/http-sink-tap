package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	defaultSinkAddr       = ":8080"
	defaultListenerAddr   = ":8081"
	defaultBodyLimitBytes = int64(1024 * 1024)
	defaultHistoryLimit   = 200
	defaultClientQueue    = 256
	defaultResponseStatus = http.StatusOK
	defaultResponseBody   = "ok\n"
)

type Config struct {
	SinkAddr       string
	ListenerAddr   string
	BodyLimitBytes int64
	HistoryLimit   int
	ResponseStatus int
	ResponseBody   string
}

type CapturedRequest struct {
	ID               uint64              `json:"id"`
	ReceivedAt       time.Time           `json:"received_at"`
	RemoteAddr       string              `json:"remote_addr"`
	Method           string              `json:"method"`
	Scheme           string              `json:"scheme"`
	Host             string              `json:"host"`
	Path             string              `json:"path"`
	RawQuery         string              `json:"raw_query,omitempty"`
	RequestURI       string              `json:"request_uri"`
	Proto            string              `json:"proto"`
	Headers          map[string][]string `json:"headers"`
	ContentLength    int64               `json:"content_length"`
	TransferEncoding []string            `json:"transfer_encoding,omitempty"`
	Body             CapturedBody        `json:"body"`
}

type CapturedBody struct {
	Size      int    `json:"size"`
	Limit     int64  `json:"limit"`
	Truncated bool   `json:"truncated"`
	Encoding  string `json:"encoding"`
	Text      string `json:"text,omitempty"`
	Base64    string `json:"base64,omitempty"`
	ReadError string `json:"read_error,omitempty"`
}

type MethodStats struct {
	Calls      int64 `json:"calls"`
	TotalBytes int64 `json:"total_bytes"`
}

type Stats struct {
	ByMethod map[string]*MethodStats `json:"by_method"`
	ByPath   map[string]*MethodStats `json:"by_path"`
	Total    MethodStats             `json:"total"`
}

type Broker struct {
	mu           sync.Mutex
	nextID       uint64
	historyLimit int
	clientQueue  int
	history      []CapturedRequest
	clients      map[chan CapturedRequest]struct{}
	stats        struct {
		byMethod map[string]*MethodStats
		byPath   map[string]*MethodStats
		total    MethodStats
	}
}

func NewBroker(historyLimit, clientQueue int) *Broker {
	if historyLimit < 0 {
		historyLimit = 0
	}
	if clientQueue <= 0 {
		clientQueue = defaultClientQueue
	}
	b := &Broker{
		historyLimit: historyLimit,
		clientQueue:  clientQueue,
		clients:      make(map[chan CapturedRequest]struct{}),
	}
	b.stats.byMethod = make(map[string]*MethodStats)
	b.stats.byPath = make(map[string]*MethodStats)
	return b
}

func (b *Broker) Publish(req CapturedRequest) CapturedRequest {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	req.ID = b.nextID

	bytes := int64(req.Body.Size)
	if ms, ok := b.stats.byMethod[req.Method]; ok {
		ms.Calls++
		ms.TotalBytes += bytes
	} else {
		b.stats.byMethod[req.Method] = &MethodStats{Calls: 1, TotalBytes: bytes}
	}
	pathKey := req.Method + " " + req.Path
	if ms, ok := b.stats.byPath[pathKey]; ok {
		ms.Calls++
		ms.TotalBytes += bytes
	} else {
		b.stats.byPath[pathKey] = &MethodStats{Calls: 1, TotalBytes: bytes}
	}
	b.stats.total.Calls++
	b.stats.total.TotalBytes += bytes

	if b.historyLimit > 0 {
		if len(b.history) == b.historyLimit {
			copy(b.history, b.history[1:])
			b.history[len(b.history)-1] = req
		} else {
			b.history = append(b.history, req)
		}
	}

	for ch := range b.clients {
		select {
		case ch <- req:
		default:
			delete(b.clients, ch)
			close(ch)
		}
	}

	return req
}

func (b *Broker) GetStats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Stats{
		ByMethod: make(map[string]*MethodStats, len(b.stats.byMethod)),
		ByPath:   make(map[string]*MethodStats, len(b.stats.byPath)),
		Total:    b.stats.total,
	}
	for k, v := range b.stats.byMethod {
		cp := *v
		s.ByMethod[k] = &cp
	}
	for k, v := range b.stats.byPath {
		cp := *v
		s.ByPath[k] = &cp
	}
	return s
}

func (b *Broker) ResetStats() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.byMethod = make(map[string]*MethodStats)
	b.stats.byPath = make(map[string]*MethodStats)
	b.stats.total = MethodStats{}
}

func (b *Broker) Register() (<-chan CapturedRequest, []CapturedRequest, func()) {
	ch := make(chan CapturedRequest, b.clientQueue)

	b.mu.Lock()
	b.clients[ch] = struct{}{}
	history := append([]CapturedRequest(nil), b.history...)
	b.mu.Unlock()

	var once sync.Once
	unregister := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.clients[ch]; ok {
				delete(b.clients, ch)
				close(ch)
			}
			b.mu.Unlock()
		})
	}

	return ch, history, unregister
}

func (b *Broker) History() []CapturedRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]CapturedRequest(nil), b.history...)
}

type SinkHandler struct {
	Broker         *Broker
	BodyLimitBytes int64
	ResponseStatus int
	ResponseBody   string
}

func (h SinkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	captured := captureRequest(r, h.BodyLimitBytes)
	captured = h.Broker.Publish(captured)

	log.Printf("captured request id=%d method=%s path=%s body_bytes=%d truncated=%t remote=%s",
		captured.ID, captured.Method, captured.RequestURI, captured.Body.Size, captured.Body.Truncated, captured.RemoteAddr)

	w.Header().Set("X-Http-Sink-Tap-Request-Id", strconv.FormatUint(captured.ID, 10))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(h.ResponseStatus)
	if r.Method != http.MethodHead && h.ResponseBody != "" {
		_, _ = w.Write([]byte(h.ResponseBody))
	}
}

func captureRequest(r *http.Request, bodyLimitBytes int64) CapturedRequest {
	body, readErr := readLimitedBody(r.Body, bodyLimitBytes)
	if r.Body != nil {
		_ = r.Body.Close()
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	}

	capturedBody := encodeBody(body.data, bodyLimitBytes, body.truncated)
	if readErr != nil {
		capturedBody.ReadError = readErr.Error()
	}

	return CapturedRequest{
		ReceivedAt:       time.Now().UTC(),
		RemoteAddr:       r.RemoteAddr,
		Method:           r.Method,
		Scheme:           scheme,
		Host:             r.Host,
		Path:             r.URL.Path,
		RawQuery:         r.URL.RawQuery,
		RequestURI:       r.RequestURI,
		Proto:            r.Proto,
		Headers:          cloneHeader(r.Header),
		ContentLength:    r.ContentLength,
		TransferEncoding: append([]string(nil), r.TransferEncoding...),
		Body:             capturedBody,
	}
}

type limitedBody struct {
	data      []byte
	truncated bool
}

func readLimitedBody(body io.Reader, limit int64) (limitedBody, error) {
	if body == nil {
		return limitedBody{}, nil
	}
	if limit < 0 {
		limit = 0
	}

	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:int(limit)]
	}

	return limitedBody{data: data, truncated: truncated}, err
}

func encodeBody(data []byte, limit int64, truncated bool) CapturedBody {
	body := CapturedBody{
		Size:      len(data),
		Limit:     limit,
		Truncated: truncated,
	}

	switch {
	case len(data) == 0:
		body.Encoding = "empty"
	case utf8.Valid(data):
		body.Encoding = "utf-8"
		body.Text = string(data)
	default:
		body.Encoding = "base64"
		body.Base64 = base64.StdEncoding.EncodeToString(data)
	}

	return body
}

func cloneHeader(src http.Header) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func firstHeaderValue(value string) string {
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func listenerHandler(broker *Broker) http.Handler {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(broker.History())
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(broker.GetStats())
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		broker.ResetStats()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("reset\n"))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWebSocket(w, r, broker, upgrader)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})

	return mux
}

func serveWebSocket(w http.ResponseWriter, r *http.Request, broker *Broker, upgrader websocket.Upgrader) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ch, history, unregister := broker.Register()
	defer unregister()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	send := func(req CapturedRequest) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(req); err != nil {
			log.Printf("websocket write failed: %v", err)
			return false
		}
		return true
	}

	for _, req := range history {
		if !send(req) {
			return
		}
	}

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case req, ok := <-ch:
			if !ok {
				return
			}
			if !send(req) {
				return
			}
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func loadConfig() (Config, error) {
	bodyLimit, err := getenvInt64("BODY_LIMIT_BYTES", defaultBodyLimitBytes)
	if err != nil {
		return Config{}, err
	}
	historyLimit, err := getenvInt("HISTORY_LIMIT", defaultHistoryLimit)
	if err != nil {
		return Config{}, err
	}
	responseStatus, err := getenvInt("RESPONSE_STATUS", defaultResponseStatus)
	if err != nil {
		return Config{}, err
	}
	if responseStatus < 100 || responseStatus > 599 {
		return Config{}, fmt.Errorf("RESPONSE_STATUS must be a valid HTTP status code, got %d", responseStatus)
	}

	return Config{
		SinkAddr:       getenvString("SINK_ADDR", defaultSinkAddr),
		ListenerAddr:   getenvString("LISTENER_ADDR", defaultListenerAddr),
		BodyLimitBytes: bodyLimit,
		HistoryLimit:   historyLimit,
		ResponseStatus: responseStatus,
		ResponseBody:   getenvString("RESPONSE_BODY", defaultResponseBody),
	}, nil
}

func getenvString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return parsed, nil
}

func getenvInt64(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return parsed, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	broker := NewBroker(cfg.HistoryLimit, defaultClientQueue)
	sinkServer := &http.Server{
		Addr:              cfg.SinkAddr,
		Handler:           SinkHandler{Broker: broker, BodyLimitBytes: cfg.BodyLimitBytes, ResponseStatus: cfg.ResponseStatus, ResponseBody: cfg.ResponseBody},
		ReadHeaderTimeout: 10 * time.Second,
	}
	listenerServer := &http.Server{
		Addr:              cfg.ListenerAddr,
		Handler:           listenerHandler(broker),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 2)
	go serveHTTP("sink", sinkServer, errCh)
	go serveHTTP("listener", listenerServer, errCh)

	select {
	case <-ctx.Done():
		log.Printf("shutdown requested")
	case err := <-errCh:
		if err != nil {
			log.Printf("server error: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = errors.Join(sinkServer.Shutdown(shutdownCtx), listenerServer.Shutdown(shutdownCtx))
	if err != nil {
		log.Fatalf("shutdown failed: %v", err)
	}
}

func serveHTTP(name string, srv *http.Server, errCh chan<- error) {
	log.Printf("%s listening on %s", name, srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- fmt.Errorf("%s server: %w", name, err)
	}
}
