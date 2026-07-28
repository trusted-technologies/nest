// Package notify sends backup status notifications. Currently only
// Telegram is supported. Notifications are fire-and-forget: failures are
// logged as warnings and never affect the backup itself.
package notify

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/trusted-technologies/nest/internal/config"
)

// apiBase is the Telegram Bot API base URL (a variable so tests can point
// it at a local server).
var apiBase = "https://api.telegram.org"

// maxErrLen bounds how much of an error message is sent to Telegram.
const maxErrLen = 500

// Notifier sends Telegram notifications about backup job progress.
type Notifier struct {
	cfg  config.Telegram
	host string
}

// New creates a Notifier. All methods are no-ops when the configuration is
// disabled or incomplete.
func New(cfg config.Telegram) *Notifier {
	host, _ := os.Hostname()
	return &Notifier{cfg: cfg, host: host}
}

// Start announces that a backup job has started.
func (n *Notifier) Start(job string) {
	n.send(fmt.Sprintf("🚀 [%s] backup started: %s", n.host, job))
}

// Success announces that a backup job finished in d.
func (n *Notifier) Success(job string, d time.Duration) {
	n.send(fmt.Sprintf("✅ [%s] backup finished: %s (took %s)", n.host, job, d.Round(time.Second)))
}

// Failure announces that a backup job failed.
func (n *Notifier) Failure(job string, err error) {
	msg := err.Error()
	if len(msg) > maxErrLen {
		msg = msg[:maxErrLen] + "…"
	}
	n.send(fmt.Sprintf("❌ [%s] backup FAILED: %s\n%s", n.host, job, msg))
}

// send posts a message to the configured chat. Errors are only logged.
func (n *Notifier) send(text string) {
	if !n.cfg.Enabled || n.cfg.BotToken == "" || n.cfg.ChatID == "" {
		return
	}
	form := url.Values{
		"chat_id": {n.cfg.ChatID},
		"text":    {text},
	}
	if n.cfg.ThreadID != "" {
		form.Set("message_thread_id", n.cfg.ThreadID)
	}
	u := fmt.Sprintf("%s/bot%s/sendMessage", apiBase, n.cfg.BotToken)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.PostForm(u, form)
	if err != nil {
		// The token is part of the request URL; *url.Error messages embed
		// that URL, so log only the wrapped transport error.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		log.Printf("warning: telegram notification failed: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("warning: telegram notification returned %s", resp.Status)
	}
}
