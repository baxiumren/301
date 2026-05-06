package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/config"
)

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

func (h *Handler) handleRedirectCommand(msg *tgbotapi.Message) {
	h.bulk.Delete(msg.From.ID)
	h.sendWithInlineKeyboard(msg.Chat.ID, "🌐 Pilih domain yang mau diganti:", h.domainKeyboard())
}

func (h *Handler) handleCallbackDomain(cb *tgbotapi.CallbackQuery, domainName string) {
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

	h.sessions.Set(cb.From.ID, found, currentURL)
	text := fmt.Sprintf("📌 *%s* (%s)\nURL sekarang: %s\n\nKirim URL tujuan baru (atau klik Cancel):", found.Name, label, currentURL)
	replyMsg := tgbotapi.NewMessage(cb.Message.Chat.ID, text)
	replyMsg.ParseMode = "Markdown"
	replyMsg.ReplyMarkup = h.cancelKeyboard()
	if _, err := h.api.Send(replyMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) handleURLInput(msg *tgbotapi.Message) {
	userID := msg.From.ID

	// Bulk session duluan
	if bulkSess, ok := h.bulk.Get(userID); ok && bulkSess.Phase == "awaiting_url" {
		h.handleBulkURLInput(msg, bulkSess)
		return
	}

	sess, ok := h.sessions.Get(userID)
	if !ok {
		return
	}

	newURL := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(newURL, "https://") {
		h.send(msg.Chat.ID, "⚠️ URL harus diawali dengan https://")
		return
	}

	sess.PendingURL = newURL
	text := fmt.Sprintf(
		"⚠️ *Konfirmasi Perubahan*\n\n🔹 Domain: *%s*\n⬅️ URL Lama:\n%s\n➡️ URL Baru:\n%s\n\nYakin mau diganti?",
		sess.Domain.Name, sess.OldURL, newURL,
	)
	confirmMsg := tgbotapi.NewMessage(msg.Chat.ID, text)
	confirmMsg.ParseMode = "Markdown"
	confirmMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✅ Ya, Ganti", "confirm_yes"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Batal", "confirm_no"),
		},
	)
	if _, err := h.api.Send(confirmMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) handleCallbackConfirmYes(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID
	sess, ok := h.sessions.Get(userID)
	if !ok {
		h.send(cb.Message.Chat.ID, "⏰ Sesi sudah habis. Mulai ulang.")
		return
	}

	domainName := sess.Domain.Name
	pendingURL := sess.PendingURL
	oldURL := sess.OldURL

	if err := h.cf.UpdateURL(*sess.Domain, pendingURL); err != nil {
		log.Printf("update URL error for %s: %v", domainName, err)
		h.send(cb.Message.Chat.ID, "❌ Gagal mengubah URL. Coba lagi.")
		return
	}

	username := cb.From.FirstName
	if cb.From.UserName != "" {
		username = "@" + cb.From.UserName
	}
	appendHistory(HistoryEntry{
		Time:     time.Now(),
		UserID:   userID,
		Username: username,
		Domain:   domainName,
		OldURL:   oldURL,
		NewURL:   pendingURL,
	})

	h.sessions.Delete(userID)
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, fmt.Sprintf("✅ *Berhasil diubah!*\nDomain: %s\nURL Baru: %s", domainName, pendingURL))
}

func (h *Handler) handleCallbackConfirmNo(cb *tgbotapi.CallbackQuery) {
	h.sessions.Delete(cb.From.ID)
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, "🚫 Dibatalkan.")
}
