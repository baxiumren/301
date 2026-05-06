package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type HistoryEntry struct {
	Time     time.Time `json:"time"`
	UserID   int64     `json:"user_id"`
	Username string    `json:"username"`
	Domain   string    `json:"domain"`
	OldURL   string    `json:"old_url"`
	NewURL   string    `json:"new_url"`
}

func appendHistory(entry HistoryEntry) {
	f, err := os.OpenFile("history.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("history write error: %v", err)
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		log.Printf("history encode error: %v", err)
	}
}

func loadHistory() []HistoryEntry {
	data, err := os.ReadFile("history.log")
	if err != nil {
		return nil
	}
	var entries []HistoryEntry
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

func (h *Handler) handleHistoryCommand(msg *tgbotapi.Message) {
	entries := loadHistory()
	text := formatHistory(entries)
	histMsg := tgbotapi.NewMessage(msg.Chat.ID, text)
	histMsg.ParseMode = "Markdown"
	histMsg.ReplyMarkup = h.replyKeyboard()
	if _, err := h.api.Send(histMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func formatHistory(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return "📜 *Belum ada riwayat perubahan.*"
	}

	// ambil 10 terakhir, tampilkan dari yang terbaru
	start := 0
	if len(entries) > 10 {
		start = len(entries) - 10
	}
	recent := entries[start:]

	var buf bytes.Buffer
	buf.WriteString("📜 *Riwayat Perubahan Redirect* (10 terakhir)\n\n")
	for i := len(recent) - 1; i >= 0; i-- {
		e := recent[i]
		wib := e.Time.In(time.FixedZone("WIB", 7*3600))
		buf.WriteString(fmt.Sprintf(
			"🔹 *%s*\n👤 %s\n🕐 %s\n⬅️ %s\n➡️ %s\n\n",
			e.Domain,
			e.Username,
			wib.Format("02 Jan 2006, 15:04 WIB"),
			e.OldURL,
			e.NewURL,
		))
	}
	return buf.String()
}
