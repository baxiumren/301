package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (h *Handler) buildBulkKeyboard(selected map[string]bool) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for i, d := range h.cfg.Domains {
		label := "☐ " + d.Name
		if selected[d.Name] {
			label = "✅ " + d.Name
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(label, "bulk_toggle:"+d.Name))
		if len(row) == 2 || i == len(h.cfg.Domains)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("✅ Selesai Pilih", "bulk_done"),
		tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
	})
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) handleBulkCommand(msg *tgbotapi.Message) {
	h.sessions.Delete(msg.From.ID)
	h.bulk.Delete(msg.From.ID)

	selected := make(map[string]bool, len(h.cfg.Domains))
	keyboard := h.buildBulkKeyboard(selected)

	bulkMsg := tgbotapi.NewMessage(msg.Chat.ID, "🔀 *Bulk Redirect* — Centang domain yang mau diganti:")
	bulkMsg.ParseMode = "Markdown"
	bulkMsg.ReplyMarkup = keyboard
	sent, err := h.api.Send(bulkMsg)
	if err != nil {
		log.Printf("send error: %v", err)
		return
	}
	h.bulk.New(msg.From.ID, msg.Chat.ID, sent.MessageID, h.cfg.Domains)
}

func (h *Handler) handleCallbackBulkToggle(cb *tgbotapi.CallbackQuery, domainName string) {
	h.bulk.Toggle(cb.From.ID, domainName)
	if sess, ok := h.bulk.Get(cb.From.ID); ok {
		keyboard := h.buildBulkKeyboard(sess.Selected)
		h.api.Send(tgbotapi.NewEditMessageReplyMarkup(cb.Message.Chat.ID, cb.Message.MessageID, keyboard))
	}
}

func (h *Handler) handleCallbackBulkDone(cb *tgbotapi.CallbackQuery) {
	sess, ok := h.bulk.Get(cb.From.ID)
	if !ok {
		h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
		return
	}
	selected := sess.SelectedNames()
	if len(selected) == 0 {
		h.api.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "⚠️ Pilih minimal 1 domain dulu!"))
		return
	}
	h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
	h.bulk.SetAwaitingURL(cb.From.ID)

	text := "🔀 *Bulk Redirect*\n\nDomain terpilih:\n"
	for _, name := range selected {
		text += "• " + name + "\n"
	}
	text += "\nKirim URL tujuan baru (atau klik Cancel):"
	cancelKb := h.cancelKeyboard()
	editMsg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &cancelKb
	h.api.Send(editMsg)
}

func (h *Handler) handleBulkURLInput(msg *tgbotapi.Message, sess *BulkSession) {
	newURL := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(newURL, "https://") {
		h.send(msg.Chat.ID, "⚠️ URL harus diawali dengan https://")
		return
	}

	h.bulk.SetPendingURL(msg.From.ID, newURL)
	selected := sess.SelectedNames()

	text := "⚠️ *Konfirmasi Bulk Redirect*\n\nDomain yang akan diubah:\n"
	for _, name := range selected {
		text += "• " + name + "\n"
	}
	text += fmt.Sprintf("\n➡️ URL Baru:\n%s\n\nYakin mau ganti semua?", newURL)

	confirmMsg := tgbotapi.NewMessage(msg.Chat.ID, text)
	confirmMsg.ParseMode = "Markdown"
	confirmMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✅ Ya, Ganti Semua", "bulk_confirm_yes"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Batal", "bulk_confirm_no"),
		},
	)
	if _, err := h.api.Send(confirmMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) handleCallbackBulkConfirmYes(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID
	sess, ok := h.bulk.Get(userID)
	if !ok {
		h.send(cb.Message.Chat.ID, "⏰ Sesi sudah habis.")
		return
	}

	username := cb.From.FirstName
	if cb.From.UserName != "" {
		username = "@" + cb.From.UserName
	}

	var success, failed []string
	for _, d := range h.cfg.Domains {
		if !sess.Selected[d.Name] {
			continue
		}
		oldURL, _ := h.cf.GetCurrentURL(d)
		if err := h.cf.UpdateURL(d, sess.PendingURL); err != nil {
			log.Printf("bulk update error for %s: %v", d.Name, err)
			failed = append(failed, d.Name)
			continue
		}
		success = append(success, d.Name)
		appendHistory(HistoryEntry{
			Time:     time.Now(),
			UserID:   userID,
			Username: username,
			Domain:   d.Name,
			OldURL:   oldURL,
			NewURL:   sess.PendingURL,
		})
	}

	h.bulk.Delete(userID)

	var sb strings.Builder
	if len(success) > 0 {
		sb.WriteString("✅ *Berhasil diubah:*\n")
		for _, name := range success {
			sb.WriteString("• " + name + "\n")
		}
		sb.WriteString(fmt.Sprintf("\n➡️ URL Baru: %s", sess.PendingURL))
	}
	if len(failed) > 0 {
		sb.WriteString("\n\n❌ *Gagal diubah:*\n")
		for _, name := range failed {
			sb.WriteString("• " + name + "\n")
		}
	}
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, sb.String())
}

func (h *Handler) handleCallbackBulkConfirmNo(cb *tgbotapi.CallbackQuery) {
	h.bulk.Delete(cb.From.ID)
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, "🚫 Dibatalkan.")
}
