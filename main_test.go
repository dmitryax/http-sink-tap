package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCaptureRequestIncludesDetails(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.test/some/path?x=1", strings.NewReader("hello"))
	req.RemoteAddr = "10.0.0.10:1234"
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Add("X-Test", "one")
	req.Header.Add("X-Test", "two")

	captured := captureRequest(req, 1024)

	if captured.Method != http.MethodPost {
		t.Fatalf("method = %q, want %q", captured.Method, http.MethodPost)
	}
	if captured.Scheme != "https" {
		t.Fatalf("scheme = %q, want https", captured.Scheme)
	}
	if captured.Host != "example.test" {
		t.Fatalf("host = %q, want example.test", captured.Host)
	}
	if captured.Path != "/some/path" {
		t.Fatalf("path = %q, want /some/path", captured.Path)
	}
	if captured.RawQuery != "x=1" {
		t.Fatalf("raw query = %q, want x=1", captured.RawQuery)
	}
	if captured.Body.Encoding != "utf-8" || captured.Body.Text != "hello" {
		t.Fatalf("body = %#v, want utf-8 hello", captured.Body)
	}
	if got := captured.Headers["X-Test"]; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("X-Test headers = %#v, want two values", got)
	}
}

func TestCaptureRequestTruncatesBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "http://example.test/upload", strings.NewReader("abcdef"))

	captured := captureRequest(req, 4)

	if !captured.Body.Truncated {
		t.Fatal("expected body to be truncated")
	}
	if captured.Body.Size != 4 {
		t.Fatalf("body size = %d, want 4", captured.Body.Size)
	}
	if captured.Body.Text != "abcd" {
		t.Fatalf("body text = %q, want abcd", captured.Body.Text)
	}
}

func TestBrokerPublishesToClientsAndHistory(t *testing.T) {
	broker := NewBroker(2, 2)
	ch, history, unregister := broker.Register()
	defer unregister()

	if len(history) != 0 {
		t.Fatalf("initial history length = %d, want 0", len(history))
	}

	first := broker.Publish(CapturedRequest{Method: http.MethodGet, Path: "/one"})
	second := broker.Publish(CapturedRequest{Method: http.MethodPost, Path: "/two"})

	got := <-ch
	if got.ID != first.ID || got.Path != "/one" {
		t.Fatalf("first published request = %#v, want id %d path /one", got, first.ID)
	}
	got = <-ch
	if got.ID != second.ID || got.Path != "/two" {
		t.Fatalf("second published request = %#v, want id %d path /two", got, second.ID)
	}

	_, history, unregister2 := broker.Register()
	defer unregister2()
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].Path != "/one" || history[1].Path != "/two" {
		t.Fatalf("history = %#v, want /one then /two", history)
	}
}

func TestSinkHandlerPublishesAndResponds(t *testing.T) {
	broker := NewBroker(10, 10)
	ch, _, unregister := broker.Register()
	defer unregister()

	handler := SinkHandler{
		Broker:         broker,
		BodyLimitBytes: 1024,
		ResponseStatus: http.StatusAccepted,
		ResponseBody:   "accepted\n",
	}
	req := httptest.NewRequest(http.MethodPatch, "http://sink.test/anything", strings.NewReader("payload"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if rec.Body.String() != "accepted\n" {
		t.Fatalf("response body = %q, want accepted", rec.Body.String())
	}
	if rec.Header().Get("X-Http-Sink-Tap-Request-Id") == "" {
		t.Fatal("missing X-Http-Sink-Tap-Request-Id response header")
	}

	select {
	case captured := <-ch:
		if captured.Method != http.MethodPatch || captured.Path != "/anything" || captured.Body.Text != "payload" {
			t.Fatalf("captured request = %#v, want PATCH /anything payload", captured)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published request")
	}
}

func TestWebSocketReceivesHistoryAndLiveRequests(t *testing.T) {
	broker := NewBroker(10, 10)
	historyReq := broker.Publish(CapturedRequest{Method: http.MethodGet, Path: "/history"})
	server := httptest.NewServer(listenerHandler(broker))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))

	var got CapturedRequest
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("read history request: %v", err)
	}
	if got.ID != historyReq.ID || got.Path != "/history" {
		t.Fatalf("history request = %#v, want id %d path /history", got, historyReq.ID)
	}

	liveReq := broker.Publish(CapturedRequest{Method: http.MethodPost, Path: "/live"})
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("read live request: %v", err)
	}
	if got.ID != liveReq.ID || got.Path != "/live" {
		t.Fatalf("live request = %#v, want id %d path /live", got, liveReq.ID)
	}
}
