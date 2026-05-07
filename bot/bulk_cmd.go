package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/config"
)

// sendBulkSelectMsg: tampilkan daftar domain di teks + reply keyboard checklist bawah.
func (h *Handler) sendBulkSelectMsg(chatID int64, selected map[string]bool) {
	var rows [][]tgbotapi.KeyboardButton
	count := 0
	for _, v := range selected {
		if v {
			count++
		}
	}

	var sb strings.Builder
	sb.WriteString("🔀 *Bulk Redirect* — Pilih domain yang mau diganti:\n\n")
	for i, d := range h.cfg.Domains {
		label := domainLabel(d.Name, d.Label)
		check := "☐"
		if selected[d.Name] {
			check = "☑️"
		}
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(check+" "+label),
		))
		sb.WriteString(fmt.Sprintf("%s %d. *%s*\n", check, i+1, label))
	}
	rows = append(rows,
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("✅ Selesai Pilih")),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true

	sb.WriteString("\n_Tap tombol di bawah untuk centang/uncentang_")
	if count > 0 {
		sb.WriteString(fmt.Sprintf("\n\n☑️ *%d domain dipilih*", count))
	}

	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendBulkConfirmMsg: konfirmasi bulk redirect dengan reply keyboard.
func (h *Handler) sendBulkConfirmMsg(chatID int64, sess *BulkSession) {
	selected := sess.SelectedNames()
	text := "⚠️ *Konfirmasi Bulk Redirect*\n\nDomain yang akan diubah:\n"
	for _, name := range selected {
		text += "• " + name + "\n"
	}
	text += fmt.Sprintf("\n➡️ URL Baru:\n`%s`\n\nYakin mau ganti semua?", sess.PendingURL)
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Ya, Ganti Semua"),
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

// findDomainByBulkBtn: parse domain dari teks button bulk (format: "☐ name [LABEL]" atau "☑️ name [LABEL]").
func (h *Handler) findDomainByBulkBtn(text string) *config.Domain {
	text = strings.TrimPrefix(text, "☐ ")
	text = strings.TrimPrefix(text, "☑️ ")
	return h.findDomainByLabel(text)
}

func (h *Handler) handleBulkCommand(msg *tgbotapi.Message) {
	if !h.isCFConfigured() {
		h.sendMd(msg.Chat.ID, "⚠️ Cloudflare belum dikonfigurasi.\n\nGunakan `/setcf <email> <api_key>` terlebih dahulu.")
		return
	}
	if len(h.cfg.Domains) == 0 {
		h.sendMd(msg.Chat.ID, "⚠️ Belum ada domain.\n\nGunakan `/adddomain` untuk menambahkan domain.")
		return
	}
	h.sessions.Delete(msg.From.ID)
	h.bulk.Delete(msg.From.ID)

	selected := make(map[string]bool, len(h.cfg.Domains))
	h.bulk.New(msg.From.ID, msg.Chat.ID, 0, h.cfg.Domains)
	h.sendBulkSelectMsg(msg.Chat.ID, selected)
}

// handleBulkSelectInput: handle teks saat phase "selecting" — toggle domain atau selesai pilih.
func (h *Handler) handleBulkSelectInput(msg *tgbotapi.Message, sess *BulkSession) {
	input := strings.TrimSpace(msg.Text)

	if input == "✅ Selesai Pilih" {
		selected := sess.SelectedNames()
		if len(selected) == 0 {
			h.send(msg.Chat.ID, "⚠️ Pilih minimal 1 domain dulu.")
			h.sendBulkSelectMsg(msg.Chat.ID, sess.Selected)
			return
		}
		h.bulk.SetAwaitingURL(msg.From.ID)
		text := "🔀 *Bulk Redirect*\n\nDomain terpilih:\n"
		for _, name := range selected {
			text += "• " + name + "\n"
		}
		text += "\n━━━━━━━━━━━━━━━━━━━━\n" +
			"Kirim *URL tujuan baru* untuk semua domain di atas:\n\n" +
			"📍 Harus diawali `https://`\n\n" +
			"Contoh:\n`https://google.com`\n`https://landing.example.com/promo`"
		h.sendWizardMsg(msg.Chat.ID, text)
		return
	}

	// Coba match dengan tombol domain
	found := h.findDomainByBulkBtn(input)
	if found != nil {
		h.bulk.Toggle(msg.From.ID, found.Name)
		if updSess, ok := h.bulk.Get(msg.From.ID); ok {
			h.sendBulkSelectMsg(msg.Chat.ID, updSess.Selected)
		}
		return
	}

	// Input tidak dikenal → tampilkan ulang
	h.sendBulkSelectMsg(msg.Chat.ID, sess.Selected)
}

func (h *Handler) handleBulkURLInput(msg *tgbotapi.Message, sess *BulkSession) {
	newURL := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(newURL, "https://") {
		h.sendWizardMsg(msg.Chat.ID, "⚠️ URL harus diawali dengan `https://`\n\nContoh:\n`https://google.com`")
		return
	}

	h.bulk.SetPendingURL(msg.From.ID, newURL)
	if updSess, ok := h.bulk.Get(msg.From.ID); ok {
		h.sendBulkConfirmMsg(msg.Chat.ID, updSess)
	}
}

// handleBulkConfirmYes: eksekusi bulk redirect setelah user tap "✅ Ya, Ganti Semua".
func (h *Handler) handleBulkConfirmYes(msg *tgbotapi.Message) {
	userID := msg.From.ID
	sess, ok := h.bulk.Get(userID)
	if !ok {
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "⏰ Sesi sudah habis.")
		return
	}
	if sess.Phase != "confirming" || sess.PendingURL == "" {
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "⚠️ Tidak ada konfirmasi yang pending.")
		return
	}

	username := msg.From.FirstName
	if msg.From.UserName != "" {
		username = "@" + msg.From.UserName
	}

	// Loading indicator
	selectedCount := len(sess.SelectedNames())
	loadMsg, loadErr := h.api.Send(tgbotapi.NewMessage(msg.Chat.ID,
		fmt.Sprintf("⏳ Mengubah %d domain di Cloudflare...", selectedCount),
	))

	var success, failed []string
	for _, d := range h.cfg.Domains {
		if !sess.Selected[d.Name] {
			continue
		}
		oldURL, _ := h.cfFor(d).GetCurrentURL(d)
		if err := h.cfFor(d).UpdateURL(d, sess.PendingURL); err != nil {
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

	if loadErr == nil {
		h.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, loadMsg.MessageID))
	}

	var sb strings.Builder
	if len(success) > 0 {
		sb.WriteString(fmt.Sprintf("✅ *Berhasil diubah!*\n👤 %s\n\n", username))
		for _, name := range success {
			sb.WriteString("🔹 " + name + "\n")
		}
		sb.WriteString(fmt.Sprintf("\n➡️ URL Baru:\n`%s`", sess.PendingURL))
	}
	if len(failed) > 0 {
		sb.WriteString("\n\n❌ *Gagal diubah:*\n")
		for _, name := range failed {
			sb.WriteString("• " + name + "\n")
		}
	}
	h.sendWithReplyKeyboard(msg.Chat.ID, userID, sb.String())
}

// Callback handlers lama — fallback/ack saja agar tidak error jika ada pesan lama.
func (h *Handler) handleCallbackBulkToggle(cb *tgbotapi.CallbackQuery, _ string) {
	h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
}
func (h *Handler) handleCallbackBulkDone(cb *tgbotapi.CallbackQuery) {
	h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
}
func (h *Handler) handleCallbackBulkConfirmYes(cb *tgbotapi.CallbackQuery) {
	h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
}
func (h *Handler) handleCallbackBulkConfirmNo(cb *tgbotapi.CallbackQuery) {
	h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
}
