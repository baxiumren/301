package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/config"
)

// domainLabel returns "name [LABEL]" kalau label ada, atau cukup "name".
func domainLabel(name, label string) string {
	if label != "" {
		return name + " [" + label + "]"
	}
	return name
}

// sendDomainReplyKeyboard: tampilkan semua domain di teks + reply keyboard bawah.
func (h *Handler) sendDomainReplyKeyboard(chatID int64) {
	var rows [][]tgbotapi.KeyboardButton
	var sb strings.Builder
	sb.WriteString("🌐 *Pilih domain yang mau diganti:*\n\n")
	for i, d := range h.cfg.Domains {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(domainLabel(d.Name, d.Label)),
		))
		sb.WriteString(fmt.Sprintf("%d. *%s*\n", i+1, domainLabel(d.Name, d.Label)))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// findDomainByLabel: cari domain berdasarkan teks button (format: "name" atau "name [LABEL]").
func (h *Handler) findDomainByLabel(text string) *config.Domain {
	for i := range h.cfg.Domains {
		if domainLabel(h.cfg.Domains[i].Name, h.cfg.Domains[i].Label) == text {
			return &h.cfg.Domains[i]
		}
	}
	return nil
}

func (h *Handler) handleRedirectCommand(msg *tgbotapi.Message) {
	if !h.isCFConfigured() {
		h.sendMd(msg.Chat.ID, "⚠️ Cloudflare belum dikonfigurasi.\n\nGunakan `/setcf <email> <api_key>` terlebih dahulu.")
		return
	}
	if len(h.cfg.Domains) == 0 {
		h.sendMd(msg.Chat.ID, "⚠️ Belum ada domain.\n\nGunakan ⚙️ *Kelola Domain* untuk menambahkan domain.")
		return
	}
	// Bersihkan state lama
	h.bulk.Delete(msg.From.ID)
	h.sessions.Delete(msg.From.ID)

	// Kalau cuma 1 domain → langsung skip picker, lompat ke input URL
	if len(h.cfg.Domains) == 1 {
		domain := &h.cfg.Domains[0]
		currentURL, err := h.cfFor(*domain).GetCurrentURL(*domain)
		if err != nil {
			log.Printf("get URL error for %s: %v", domain.Name, err)
			currentURL = "(gagal mengambil URL saat ini)"
		}
		label := "Redirect Rules"
		if domain.Type == "page_rules" {
			label = "Page Rules"
		}
		h.sessions.Set(msg.From.ID, domain, currentURL)
		text := fmt.Sprintf(
			"📌 *%s* (%s)\n\n"+
				"URL sekarang:\n`%s`\n\n"+
				"━━━━━━━━━━━━━━━━━━━━\n"+
				"Kirim *URL tujuan baru*:\n\n"+
				"📍 Harus diawali `https://`\n\n"+
				"Contoh:\n`https://google.com`\n`https://landing.example.com/promo`",
			domainLabel(domain.Name, domain.Label), label, currentURL,
		)
		h.sendWizardMsg(msg.Chat.ID, text)
		return
	}

	// Lebih dari 1 domain → tampilkan keyboard pilih domain
	setRedirectAwaitDomain(msg.From.ID)
	h.sendDomainReplyKeyboard(msg.Chat.ID)
}

// handleRedirectDomainSelect: dipanggil dari handleURLInput ketika user sedang pilih domain.
func (h *Handler) handleRedirectDomainSelect(msg *tgbotapi.Message) {
	userID := msg.From.ID
	input := strings.TrimSpace(msg.Text)

	found := h.findDomainByLabel(input)
	if found == nil {
		// Tidak cocok → tampilkan ulang pilihan
		h.sendDomainReplyKeyboard(msg.Chat.ID)
		return
	}

	deleteRedirectAwaitDomain(userID)

	currentURL, err := h.cfFor(*found).GetCurrentURL(*found)
	if err != nil {
		log.Printf("get URL error for %s: %v", found.Name, err)
		currentURL = "(gagal mengambil URL saat ini)"
	}

	label := "Redirect Rules"
	if found.Type == "page_rules" {
		label = "Page Rules"
	}

	h.sessions.Set(userID, found, currentURL)

	text := fmt.Sprintf(
		"📌 *%s* (%s)\n\n"+
			"URL sekarang:\n`%s`\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"Kirim *URL tujuan baru*:\n\n"+
			"📍 Harus diawali `https://`\n\n"+
			"Contoh:\n`https://google.com`\n`https://landing.example.com/promo`",
		domainLabel(found.Name, found.Label), label, currentURL,
	)
	h.sendWizardMsg(msg.Chat.ID, text)
}

// sendRedirectConfirmMsg: tampilkan pesan konfirmasi perubahan URL dengan reply keyboard.
func (h *Handler) sendRedirectConfirmMsg(chatID int64, sess *Session) {
	text := fmt.Sprintf(
		"⚠️ *Konfirmasi Perubahan*\n\n"+
			"🔹 Domain: *%s*\n"+
			"⬅️ URL Lama:\n`%s`\n"+
			"➡️ URL Baru:\n`%s`\n\n"+
			"Yakin mau diganti?",
		sess.Domain.Name, sess.OldURL, sess.PendingURL,
	)
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Ya, Ganti"),
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

func (h *Handler) handleURLInput(msg *tgbotapi.Message) {
	userID := msg.From.ID

	// Setup wizard session (CF email/apikey)
	if setupSess, ok := setupSessions.get(userID); ok {
		h.handleSetupInput(msg, setupSess)
		return
	}

	// Add domain session
	if addSess, ok := addDomainSessions.get(userID); ok {
		h.handleAddDomainInput(msg, addSess)
		return
	}

	// List filter session
	if listFilters.has(userID) {
		h.handleListFilterInput(msg)
		return
	}

	// Redirect domain selection
	if hasRedirectAwaitDomain(userID) {
		h.handleRedirectDomainSelect(msg)
		return
	}

	// Remove domain selection
	if hasRemoveDomainSelectAwait(userID) {
		h.handleRemoveDomainSelect(msg)
		return
	}

	// Rollback selection
	if hasRollbackAwait(userID) {
		h.handleRollbackSelect(msg)
		return
	}

	// Bulk session — handle semua phase
	if bulkSess, ok := h.bulk.Get(userID); ok {
		switch bulkSess.Phase {
		case "selecting":
			h.handleBulkSelectInput(msg, bulkSess)
		case "awaiting_url":
			h.handleBulkURLInput(msg, bulkSess)
		case "confirming":
			// Input lain saat konfirmasi → tampilkan ulang pesan konfirmasi
			h.sendBulkConfirmMsg(msg.Chat.ID, bulkSess)
		}
		return
	}

	// Redirect session
	sess, ok := h.sessions.Get(userID)
	if !ok {
		return
	}

	// Jika sudah di tahap konfirmasi, tampilkan ulang pesan konfirmasi
	if sess.PendingURL != "" {
		h.sendRedirectConfirmMsg(msg.Chat.ID, sess)
		return
	}

	newURL := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(newURL, "https://") {
		h.sendWizardMsg(msg.Chat.ID, "⚠️ URL harus diawali dengan `https://`\n\nContoh:\n`https://google.com`")
		return
	}

	sess.PendingURL = newURL
	h.sendRedirectConfirmMsg(msg.Chat.ID, sess)
}

// handleConfirmRedirectYes: dipanggil saat user tap "✅ Ya, Ganti" dari reply keyboard.
func (h *Handler) handleConfirmRedirectYes(msg *tgbotapi.Message) {
	userID := msg.From.ID
	sess, ok := h.sessions.Get(userID)
	if !ok {
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "⏰ Sesi sudah habis. Mulai ulang.")
		return
	}
	if sess.PendingURL == "" {
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "⚠️ Tidak ada URL yang dikonfirmasi.")
		return
	}

	domainName := sess.Domain.Name
	pendingURL := sess.PendingURL
	oldURL := sess.OldURL

	if err := h.cfFor(*sess.Domain).UpdateURL(*sess.Domain, pendingURL); err != nil {
		log.Printf("update URL error for %s: %v", domainName, err)
		h.sendWizardMsg(msg.Chat.ID, "❌ Gagal mengubah URL. Coba lagi.")
		return
	}

	username := msg.From.FirstName
	if msg.From.UserName != "" {
		username = "@" + msg.From.UserName
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
	h.sendWithReplyKeyboard(msg.Chat.ID, userID,
		fmt.Sprintf("✅ *Berhasil diubah!*\n👤 %s\n🔹 Domain: *%s*\n➡️ URL Baru:\n`%s`", username, domainName, pendingURL),
	)
}

// handleCallbackConfirmYes / No: fallback untuk pesan lama yang masih punya inline button.
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
	if err := h.cfFor(*sess.Domain).UpdateURL(*sess.Domain, pendingURL); err != nil {
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
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, userID,
		fmt.Sprintf("✅ *Berhasil diubah!*\n👤 %s\n🔹 Domain: *%s*\n➡️ URL Baru:\n`%s`", username, domainName, pendingURL),
	)
}

func (h *Handler) handleCallbackConfirmNo(cb *tgbotapi.CallbackQuery) {
	h.sessions.Delete(cb.From.ID)
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, cb.From.ID, "🚫 Dibatalkan.")
}
