package bot

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/cloudflare"
	"cf-redirect-bot/config"
)

type Handler struct {
	api      *tgbotapi.BotAPI
	cfg      *config.Config
	cf       cloudflare.Client
	sessions *SessionStore
}

func NewHandler(api *tgbotapi.BotAPI, cfg *config.Config, cf cloudflare.Client) *Handler {
	return &Handler{
		api:      api,
		cfg:      cfg,
		cf:       cf,
		sessions: NewSessionStore(),
	}
}

func (h *Handler) isAllowed(userID int64) bool {
	for _, id := range h.cfg.Whitelist {
		if id == userID {
			return true
		}
	}
	return false
}

func (h *Handler) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) sendWithKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) Handle(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		h.handleCallback(update.CallbackQuery)
		return
	}
	if update.Message == nil {
		return
	}

	userID := update.Message.From.ID
	if !h.isAllowed(userID) {
		h.send(update.Message.Chat.ID, "⛔ Kamu tidak memiliki akses untuk menggunakan command ini.")
		return
	}

	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "redirect":
			h.handleRedirectCommand(update.Message)
		case "status":
			h.handleStatusCommand(update.Message)
		}
		return
	}

	h.handleURLInput(update.Message)
}

func (h *Handler) domainKeyboard() tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for i, d := range h.cfg.Domains {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(d.Name, "domain:"+d.Name))
		if len(row) == 2 || i == len(h.cfg.Domains)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
	})
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) cancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
		},
	)
}

func (h *Handler) handleRedirectCommand(msg *tgbotapi.Message) {
	h.sendWithKeyboard(msg.Chat.ID, "🌐 Pilih domain yang mau diganti:", h.domainKeyboard())
}

func (h *Handler) handleStatusCommand(msg *tgbotapi.Message) {
	var sb strings.Builder
	sb.WriteString("📊 *Status Redirect Semua Domain:*\n\n")
	for _, d := range h.cfg.Domains {
		label := "Redirect Rules"
		if d.Type == "page_rules" {
			label = "Page Rules"
		}
		url, err := h.cf.GetCurrentURL(d)
		if err != nil {
			log.Printf("status error for %s: %v", d.Name, err)
			sb.WriteString(fmt.Sprintf("🔹 *%s* (%s)\n❌ Gagal mengambil data\n\n", d.Name, label))
			continue
		}
		sb.WriteString(fmt.Sprintf("🔹 *%s* (%s)\n→ %s\n\n", d.Name, label, url))
	}
	statusMsg := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	statusMsg.ParseMode = "Markdown"
	if _, err := h.api.Send(statusMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) handleCallback(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID
	ack := tgbotapi.NewCallback(cb.ID, "")
	h.api.Send(ack)

	if !h.isAllowed(userID) {
		h.send(cb.Message.Chat.ID, "⛔ Kamu tidak memiliki akses.")
		return
	}

	data := cb.Data

	if data == "cancel" {
		h.sessions.Delete(userID)
		h.send(cb.Message.Chat.ID, "🚫 Dibatalkan.")
		return
	}

	if strings.HasPrefix(data, "domain:") {
		domainName := strings.TrimPrefix(data, "domain:")
		var found *config.Domain
		for i := range h.cfg.Domains {
			if h.cfg.Domains[i].Name == domainName {
				found = &h.cfg.Domains[i]
				break
			}
		}
		if found == nil {
			h.send(cb.Message.Chat.ID, "❌ Domain tidak ditemukan.")
			return
		}

		currentURL, err := h.cf.GetCurrentURL(*found)
		if err != nil {
			log.Printf("get URL error for %s: %v", found.Name, err)
			currentURL = "(gagal mengambil URL saat ini)"
		}

		label := "Redirect Rules"
		if found.Type == "page_rules" {
			label = "Page Rules"
		}

		h.sessions.Set(userID, found)
		text := fmt.Sprintf("📌 *%s* (%s)\nURL sekarang: %s\n\nKirim URL tujuan baru (atau klik Cancel):", found.Name, label, currentURL)
		replyMsg := tgbotapi.NewMessage(cb.Message.Chat.ID, text)
		replyMsg.ParseMode = "Markdown"
		replyMsg.ReplyMarkup = h.cancelKeyboard()
		if _, err := h.api.Send(replyMsg); err != nil {
			log.Printf("send error: %v", err)
		}
	}
}

func (h *Handler) handleURLInput(msg *tgbotapi.Message) {
	userID := msg.From.ID
	sess, ok := h.sessions.Get(userID)
	if !ok {
		return
	}

	newURL := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(newURL, "https://") {
		h.send(msg.Chat.ID, "⚠️ URL harus diawali dengan https://")
		return
	}

	if err := h.cf.UpdateURL(*sess.Domain, newURL); err != nil {
		log.Printf("update URL error for %s: %v", sess.Domain.Name, err)
		h.send(msg.Chat.ID, "❌ Gagal mengubah URL. Coba lagi.")
		return
	}

	h.sessions.Delete(userID)
	h.send(msg.Chat.ID, fmt.Sprintf("✅ Berhasil diubah!\nDomain : %s\nURL Baru: %s", sess.Domain.Name, newURL))
}
