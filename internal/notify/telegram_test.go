package notify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trusted-technologies/nest/internal/config"
)

// captureServer records the last request path and form values.
func captureServer(t *testing.T, path *string, form *url.Values) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		*form = r.Form
		w.WriteHeader(http.StatusOK)
	}))
}

func withAPIBase(t *testing.T, base string) {
	t.Helper()
	old := apiBase
	apiBase = base
	t.Cleanup(func() { apiBase = old })
}

func TestNotifierSuccess(t *testing.T) {
	var path string
	var form url.Values
	srv := captureServer(t, &path, &form)
	defer srv.Close()
	withAPIBase(t, srv.URL)

	n := New(config.Telegram{Enabled: true, BotToken: "TOKEN", ChatID: "42", ThreadID: "7"})
	n.host = "testhost"
	n.Success("files", 90*time.Second)

	if path != "/botTOKEN/sendMessage" {
		t.Errorf("path: got %q", path)
	}
	if form.Get("chat_id") != "42" {
		t.Errorf("chat_id: got %q", form.Get("chat_id"))
	}
	if form.Get("message_thread_id") != "7" {
		t.Errorf("message_thread_id: got %q", form.Get("message_thread_id"))
	}
	text := form.Get("text")
	if !strings.Contains(text, "testhost") || !strings.Contains(text, "files") || !strings.Contains(text, "1m30s") {
		t.Errorf("text missing host/job/duration: %q", text)
	}
}

func TestNotifierFailureTruncatesError(t *testing.T) {
	var path string
	var form url.Values
	srv := captureServer(t, &path, &form)
	defer srv.Close()
	withAPIBase(t, srv.URL)

	n := New(config.Telegram{Enabled: true, BotToken: "TOKEN", ChatID: "42"})
	n.host = "testhost"
	n.Failure("db", errors.New(strings.Repeat("x", 1000)))

	text := form.Get("text")
	if !strings.Contains(text, "db") {
		t.Errorf("text missing job name: %q", text[:50])
	}
	if len(text) > 600 {
		t.Errorf("error should be truncated to ~500 chars, text len %d", len(text))
	}
	if _, ok := form["message_thread_id"]; ok {
		t.Errorf("message_thread_id should be omitted when empty")
	}
}

func TestNotifierDisabledSendsNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	for _, cfg := range []config.Telegram{
		{Enabled: false, BotToken: "T", ChatID: "1"},
		{Enabled: true, BotToken: "", ChatID: "1"},
		{Enabled: true, BotToken: "T", ChatID: ""},
	} {
		New(cfg).Start("job")
	}
	if called {
		t.Error("no request should be sent when disabled or incompletely configured")
	}
}
