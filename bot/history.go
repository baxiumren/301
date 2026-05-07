package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/config"
)

type HistoryEntry struct {
	Time     time.Time `json:"time"`
	UserID   int64     `json:"user_id"`
	Username string    `json:"username"`
	Domain   string    `json:"domain"`
	OldURL   string    `json:"old_url"`
	NewURL   string    `json:"new_url"`
}

// --- rollback store ---

type rollbackStore struct {
	mu      sync.Mutex
	entries map[int64][]HistoryEntry
}

var rollbacks = &rollbackStore{entries: make(map[int64][]HistoryEntry)}

func (r *rollbackStore) set(userID int64, entries []HistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[userID] = entries
}

func (r *rollbackStore) get(userID int64, idx int) (HistoryEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, ok := r.entries[userID]
	if !ok || idx < 0 || idx >= len(entries) {
		return HistoryEntry{}, false
	}
	return entries[idx], true
}

func (r *rollbackStore) getAll(userID int64) ([]HistoryEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, ok := r.entries[userID]
	return entries, ok
}

// --- history file ---

const historyRetention = 48 * time.Hour // simpan 2 hari terakhir

// initHistoryFile membuat history.log jika belum ada, trim entry lama,
// dan jalankan goroutine cleanup otomatis tiap 24 jam.
func initHistoryFile() {
	f, err := os.OpenFile("history.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("history init error: %v", err)
		return
	}
	f.Close()

	trimHistory()

	go func() {
		for range time.Tick(24 * time.Hour) {
			trimHistory()
		}
	}()
}

func trimHistory() {
	entries := loadHistory()
	if len(entries) == 0 {
		return
	}
	cutoff := time.Now().Add(-historyRetention)
	var kept []HistoryEntry
	for _, e := range entries {
		if e.Time.After(cutoff) {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(entries) {
		return
	}
	var buf bytes.Buffer
	for _, e := range kept {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile("history.log", buf.Bytes(), 0644); err != nil {
		log.Printf("history trim error: %v", err)
		return
	}
	log.Printf("history: trimmed %d entry lama (>2 hari)", len(entries)-len(kept))
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

// --- handlers ---

func (h *Handler) handleHistoryCommand(msg *tgbotapi.Message) {
	all := loadHistory()

	// ambil 10 terakhir, urut dari yang terbaru
	start := 0
	if len(all) > 10 {
		start = len(all) - 10
	}
	recent := all[start:]

	// reverse: index 0 = paling baru
	displayed := make([]HistoryEntry, len(recent))
	for i, e := range recent {
		displayed[len(recent)-1-i] = e
	}

	rollbacks.set(msg.From.ID, displayed)

	text := formatHistory(displayed)

	if len(displayed) == 0 {
		h.sendWithReplyKeyboard(msg.Chat.ID, msg.From.ID, text)
		return
	}

	// reply keyboard: satu tombol rollback per entry
	setRollbackAwait(msg.From.ID)
	var rows [][]tgbotapi.KeyboardButton
	for i, e := range displayed {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("↩️ #%d %s", i+1, e.Domain)),
		))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true

	histMsg := tgbotapi.NewMessage(msg.Chat.ID, text)
	histMsg.ParseMode = "Markdown"
	histMsg.ReplyMarkup = kb
	if _, err := h.api.Send(histMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func formatHistory(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return "📜 *Belum ada riwayat perubahan.*"
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("📜 *Riwayat Perubahan Redirect* (%d terakhir)\n\n", len(entries)))
	for i, e := range entries {
		wib := e.Time.In(time.FixedZone("WIB", 7*3600))
		buf.WriteString(fmt.Sprintf(
			"*#%d* 🔹 *%s*\n👤 %s | 🕐 %s\n⬅️ `%s`\n➡️ `%s`\n\n",
			i+1,
			e.Domain,
			e.Username,
			wib.Format("02 Jan 2006, 15:04 WIB"),
			e.OldURL,
			e.NewURL,
		))
	}
	buf.WriteString("_Tap tombol di bawah untuk rollback ke URL lama._")
	return buf.String()
}

// parseRollbackBtnIdx: parse "↩️ #N domain.com" → index N-1 (0-based).
func parseRollbackBtnIdx(text string) (int, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "↩️ #") {
		return 0, false
	}
	text = strings.TrimPrefix(text, "↩️ #")
	parts := strings.SplitN(text, " ", 2)
	if len(parts) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}

// sendRollbackReplyKeyboard: tampilkan ulang keyboard rollback dari store.
func (h *Handler) sendRollbackReplyKeyboard(chatID int64, userID int64) {
	entries, ok := rollbacks.getAll(userID)
	if !ok || len(entries) == 0 {
		deleteRollbackAwait(userID)
		h.sendWithReplyKeyboard(chatID, userID, "⏰ Sesi history sudah kedaluwarsa. Ketik /history lagi.")
		return
	}
	var rows [][]tgbotapi.KeyboardButton
	for i, e := range entries {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("↩️ #%d %s", i+1, e.Domain)),
		))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, "⚠️ Pilih entry rollback dengan tap tombol di bawah:")
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendRollbackConfirmMsg: tampilkan konfirmasi rollback dengan reply keyboard.
func (h *Handler) sendRollbackConfirmMsg(chatID int64, domain *config.Domain, entry HistoryEntry) {
	text := fmt.Sprintf(
		"↩️ *Konfirmasi Rollback*\n\n🔹 Domain: *%s*\n\n⬅️ URL Saat Ini:\n`%s`\n\n🔄 Kembali ke:\n`%s`\n\nYakin mau rollback?",
		domainLabel(domain.Name, domain.Label),
		entry.NewURL,
		entry.OldURL,
	)
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Ya, Rollback"),
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	out := tgbotapi.NewMessage(chatID, text)
	out.ParseMode = "Markdown"
	out.ReplyMarkup = kb
	if _, err := h.api.Send(out); err != nil {
		log.Printf("send error: %v", err)
	}
}

// handleRollbackSelect: dipanggil dari handleURLInput saat rollbackAwait aktif.
func (h *Handler) handleRollbackSelect(msg *tgbotapi.Message) {
	userID := msg.From.ID
	input := strings.TrimSpace(msg.Text)

	idx, ok := parseRollbackBtnIdx(input)
	if !ok {
		h.sendRollbackReplyKeyboard(msg.Chat.ID, userID)
		return
	}

	entry, ok := rollbacks.get(userID, idx)
	if !ok {
		deleteRollbackAwait(userID)
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "⏰ Sesi history sudah kedaluwarsa. Ketik /history lagi.")
		return
	}

	// cari domain dari config
	var domain *config.Domain
	for i := range h.cfg.Domains {
		if h.cfg.Domains[i].Name == entry.Domain {
			domain = &h.cfg.Domains[i]
			break
		}
	}
	if domain == nil {
		deleteRollbackAwait(userID)
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, fmt.Sprintf("❌ Domain %s tidak ditemukan di config.", entry.Domain))
		return
	}

	deleteRollbackAwait(userID)

	// setup session: OldURL = URL saat ini (entry.NewURL), PendingURL = target rollback (entry.OldURL)
	h.sessions.Set(userID, domain, entry.NewURL)
	if sess, ok := h.sessions.Get(userID); ok {
		sess.PendingURL = entry.OldURL
	}

	h.sendRollbackConfirmMsg(msg.Chat.ID, domain, entry)
}

// handleCallbackRollback: fallback untuk pesan lama yang masih punya inline button.
func (h *Handler) handleCallbackRollback(cb *tgbotapi.CallbackQuery, idxStr string) {
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		h.send(cb.Message.Chat.ID, "❌ Index rollback tidak valid.")
		return
	}

	entry, ok := rollbacks.get(cb.From.ID, idx)
	if !ok {
		h.send(cb.Message.Chat.ID, "⏰ Sesi history sudah kedaluwarsa. Ketik /history lagi.")
		return
	}

	// cari domain dari config
	var domain *config.Domain
	for i := range h.cfg.Domains {
		if h.cfg.Domains[i].Name == entry.Domain {
			domain = &h.cfg.Domains[i]
			break
		}
	}
	if domain == nil {
		h.send(cb.Message.Chat.ID, fmt.Sprintf("❌ Domain %s tidak ditemukan di config.", entry.Domain))
		return
	}

	// setup session
	h.sessions.Set(cb.From.ID, domain, entry.NewURL)
	if sess, ok := h.sessions.Get(cb.From.ID); ok {
		sess.PendingURL = entry.OldURL
	}

	h.sendRollbackConfirmMsg(cb.Message.Chat.ID, domain, entry)
}
