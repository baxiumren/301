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
	api        *tgbotapi.BotAPI
	cfg        *config.Config
	sessions   *SessionStore
	bulk       *BulkStore
	configPath string
}

func NewHandler(api *tgbotapi.BotAPI, cfg *config.Config, configPath string) *Handler {
	initHistoryFile()
	return &Handler{
		api:        api,
		cfg:        cfg,
		sessions:   NewSessionStore(),
		bulk:       NewBulkStore(),
		configPath: configPath,
	}
}

// cfFor returns the CF client for the given domain's account.
func (h *Handler) cfFor(domain config.Domain) cloudflare.Client {
	acc := h.cfg.AccountForDomain(domain)
	if acc == nil {
		return cloudflare.New("", "")
	}
	return cloudflare.New(acc.Email, acc.APIKey)
}

// cfForAccountName returns the CF client for a named account.
// Falls back to default account if name is empty or not found.
func (h *Handler) cfForAccountName(name string) cloudflare.Client {
	var acc *config.CFAccount
	if name != "" {
		acc = h.cfg.FindAccount(name)
	}
	if acc == nil {
		acc = h.cfg.DefaultAccount()
	}
	if acc == nil {
		return cloudflare.New("", "")
	}
	return cloudflare.New(acc.Email, acc.APIKey)
}

// isCFConfigured returns true if at least one CF account is configured.
func (h *Handler) isCFConfigured() bool {
	return len(h.cfg.CFAccounts) > 0
}

// isAllowedChat returns true jika chat ID ini adalah grup yang terdaftar.
func (h *Handler) isAllowedChat(chatID int64) bool {
	return h.cfg.AllowedChatID != 0 && h.cfg.AllowedChatID == chatID
}

// --- keyboard helpers ---

func (h *Handler) replyKeyboardFor(_ int64) tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⚙️ Kelola Domain"),
			tgbotapi.NewKeyboardButton("☁️ Kelola Akun CF"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🌐 Ganti Redirect"),
			tgbotapi.NewKeyboardButton("🔀 Bulk Redirect"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 List URL"),
			tgbotapi.NewKeyboardButton("📜 History"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📖 Help"),
			tgbotapi.NewKeyboardButton("ℹ️ Info"),
		),
	}
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

// --- send helpers ---

func (h *Handler) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) sendMd(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) sendWithReplyKeyboard(chatID int64, userID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = h.replyKeyboardFor(userID)
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// --- main dispatcher ---

func (h *Handler) Handle(update tgbotapi.Update) {
	if update.MyChatMember != nil {
		// Bot ditambahkan ke grup
		newStatus := update.MyChatMember.NewChatMember.Status
		if newStatus == "member" || newStatus == "administrator" {
			chatID := update.MyChatMember.Chat.ID
			// Hanya sambut di grup yang sudah terdaftar
			if h.isAllowedChat(chatID) {
				h.sendWelcome(chatID, update.MyChatMember.From.ID)
			}
		}
		return
	}

	if update.CallbackQuery != nil {
		h.handleCallback(update.CallbackQuery)
		return
	}
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	chatType := update.Message.Chat.Type // "private", "group", "supergroup", "channel"

	// Setup mode: AllowedChatID belum diset → tunggu /start dari grup
	if h.isSetupMode() {
		if update.Message.IsCommand() && update.Message.Command() == "start" {
			if chatType == "private" {
				h.sendMd(chatID,
					"⚠️ *Bot ini hanya bisa digunakan dari grup.*\n\n"+
						"Tambahkan bot ke grup kamu, lalu ketik `/start` di sana.",
				)
				return
			}
			// Grup/supergroup → daftarkan sebagai allowed chat
			h.cfg.AllowedChatID = chatID
			if err := h.cfg.Save(h.configPath); err != nil {
				log.Printf("save config error: %v", err)
			}
			// Kalau CF sudah dikonfigurasi → langsung welcome, skip wizard
			if h.isCFConfigured() {
				h.sendWelcome(chatID, userID)
			} else {
				h.sendSetupWelcome(chatID, userID)
			}
		} else {
			if chatType == "private" {
				h.sendMd(chatID,
					"⚙️ *Bot belum dikonfigurasi.*\n\n"+
						"Tambahkan bot ke grup kamu, lalu ketik `/start` di sana.",
				)
			}
			// Dari grup lain → diam saja
		}
		return
	}

	// Hanya proses pesan dari grup yang terdaftar — grup lain & DM langsung diabaikan
	if !h.isAllowedChat(chatID) {
		return
	}

	// Cloudflare belum dikonfigurasi → paksa setup CF dulu sebelum apapun
	if !h.isCFConfigured() {
		// Ignore commands selain /start saat CF belum dikonfigurasi
		if update.Message.IsCommand() {
			cmd := update.Message.Command()
			if cmd != "start" && cmd != "help" && cmd != "setcf" {
				return
			}
		}
		if sess, ok := setupSessions.get(userID); ok {
			// Jangan proses command sebagai input wizard
			if !update.Message.IsCommand() {
				h.handleSetupInput(update.Message, sess)
				return
			}
		}
		// Siapapun di grup yang kirim pesan → mulai wizard CF
		setupSessions.set(userID, &SetupSession{Phase: setupCFEmail})
		h.sendMd(chatID,
			"⚙️ *Cloudflare belum dikonfigurasi!*\n\n"+
				"Masukkan *email Cloudflare* untuk melanjutkan.\n\n"+
				"📍 Cek di: cloudflare.com → klik foto profil kanan atas\n\n"+
				"Contoh:\n`user@gmail.com`",
		)
		return
	}

	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			h.handleStartCommand(update.Message)
		case "help":
			h.handleHelpCommand(update.Message)
		case "info":
			h.handleInfoCommand(update.Message)
		case "redirect":
			h.handleRedirectCommand(update.Message)
		case "bulk":
			h.handleBulkCommand(update.Message)
		case "list":
			h.handleListCommand(update.Message)
		case "history":
			h.handleHistoryCommand(update.Message)
		case "setcf":
			h.handleSetCFCommand(update.Message)
		case "adddomain":
			h.handleAddDomainCommand(update.Message)
		case "removedomain":
			h.handleRemoveDomainCommand(update.Message)
		}
		return
	}

	switch update.Message.Text {
	case "🌐 Ganti Redirect":
		h.handleRedirectCommand(update.Message)
	case "🔀 Bulk Redirect":
		h.handleBulkCommand(update.Message)
	case "📋 List URL":
		h.handleListCommand(update.Message)
	case "📜 History":
		h.handleHistoryCommand(update.Message)
	case "📖 Help":
		h.handleHelpCommand(update.Message)
	case "ℹ️ Info":
		h.handleInfoCommand(update.Message)
	case "✅ Ya, Ganti":
		h.handleConfirmRedirectYes(update.Message)
	case "✅ Ya, Rollback":
		h.handleConfirmRedirectYes(update.Message)
	case "✅ Ya, Ganti Semua":
		h.handleBulkConfirmYes(update.Message)
	case "✅ Ya, Hapus":
		h.handleConfirmRemoveDomain(update.Message)
	case "❌ Cancel":
		h.sessions.Delete(userID)
		h.bulk.Delete(userID)
		deleteRedirectAwaitDomain(userID)
		deleteRollbackAwait(userID)
		deleteRemoveDomainAwait(userID)
		deleteRemoveDomainSelectAwait(userID)
		h.cleanupNewDomainSession(userID)
		addDomainSessions.delete(userID)
		setupSessions.delete(userID)
		listFilters.delete(userID)
		h.sendWithReplyKeyboard(chatID, userID, "🚫 Dibatalkan.")
	case "✅ Sudah, Lanjut":
		h.startAddDomainWizard(chatID, userID)
	case "➕ Tambah Domain":
		h.handleCallbackSetupDomainNew(&tgbotapi.CallbackQuery{
			From:    update.Message.From,
			Message: update.Message,
		})
	case "🔗 Sudah Ada di CF":
		h.startAddDomainWizard(chatID, userID)
	case "➕ Tambah Lagi":
		h.showDomainChoiceButtons(chatID)
	case "✅ Selesai Setup":
		h.sendWithReplyKeyboard(chatID, userID,
			"🎉 *Setup selesai! Bot siap digunakan.*\n\n"+
				"Gunakan tombol di bawah untuk mulai mengelola redirect domain kamu.\n\n"+
				"💡 Tips:\n"+
				"• `📋 List URL` — lihat semua URL redirect saat ini\n"+
				"• `🌐 Ganti Redirect` — ganti 1 domain\n"+
				"• `🔀 Bulk Redirect` — ganti banyak domain sekaligus\n"+
				"• `/list namalabel` — filter domain berdasarkan label",
		)
	case "⚙️ Kelola Domain":
		h.showDomainChoiceButtons(chatID)
	case "☁️ Kelola Akun CF":
		h.showCFAccountMenu(chatID)
	case "➕ Tambah Akun CF":
		setupSessions.set(userID, &SetupSession{Phase: setupAddCFName})
		h.sendWizardMsg(chatID,
			"☁️ *Tambah Akun CF Baru*\n\n"+
				"Masukkan *nama* untuk akun ini.\n\n"+
				"Nama digunakan untuk identifikasi saat tambah domain.\n\n"+
				"Contoh:\n`bisnis`\n`personal`\n`client-abc`",
		)
	case "🗑️ Hapus Akun CF":
		if len(h.cfg.CFAccounts) == 0 {
			h.sendWithReplyKeyboard(chatID, userID, "⚠️ Belum ada akun CF yang terdaftar.")
			return
		}
		if len(h.cfg.CFAccounts) == 1 {
			setupSessions.set(userID, &SetupSession{Phase: setupDeleteCFSelect})
			h.sendDeleteCFAccountPicker(chatID)
		} else {
			setupSessions.set(userID, &SetupSession{Phase: setupDeleteCFSelect})
			h.sendDeleteCFAccountPicker(chatID)
		}
	case "✅ Ya, Hapus Akun":
		sess, ok := setupSessions.get(userID)
		if !ok || sess.Phase != setupDeleteCFConfirm {
			h.sendWithReplyKeyboard(chatID, userID, "⏰ Sesi sudah habis. Coba lagi dari ☁️ Kelola Akun CF.")
			return
		}
		name := sess.TempOldName
		newList := make([]config.CFAccount, 0, len(h.cfg.CFAccounts))
		for _, acc := range h.cfg.CFAccounts {
			if acc.Name != name {
				newList = append(newList, acc)
			}
		}
		old := h.cfg.CFAccounts
		h.cfg.CFAccounts = newList
		if err := h.cfg.Save(h.configPath); err != nil {
			log.Printf("save config error: %v", err)
			h.cfg.CFAccounts = old
			h.sendWithReplyKeyboard(chatID, userID, "❌ Gagal menyimpan config.")
			return
		}
		setupSessions.delete(userID)
		h.sendWithReplyKeyboard(chatID, userID, fmt.Sprintf(
			"✅ *Akun CF berhasil dihapus dari bot.*\n\n☁️ Nama: *%s*", name,
		))
	case "✏️ Edit Nama Akun":
		if len(h.cfg.CFAccounts) == 1 {
			setupSessions.set(userID, &SetupSession{Phase: setupEditCFName, TempOldName: h.cfg.CFAccounts[0].Name})
			h.sendWizardMsg(chatID, fmt.Sprintf(
				"✏️ *Edit Nama Akun*\n\nAkun: *%s*\n\nMasukkan *nama baru* untuk akun ini:\n\n"+
					"Contoh:\n`bisnis-baru`\n`akun-utama`",
				h.cfg.CFAccounts[0].Name,
			))
		} else {
			setupSessions.set(userID, &SetupSession{Phase: setupEditCFSelect})
			h.sendEditCFAccountPicker(chatID)
		}
	case "🗑️ Hapus Domain":
		if len(h.cfg.Domains) == 0 {
			h.sendWithReplyKeyboard(chatID, userID, "⚠️ Belum ada domain yang terdaftar.")
			return
		}
		setRemoveDomainSelectAwait(userID)
		h.sendRemoveDomainSelectKeyboard(chatID)
	case "🔍 Check Status":
		h.handleCheckNSStatus(chatID, userID)
	default:
		h.handleURLInput(update.Message)
	}
}

// --- callback dispatcher ---

func (h *Handler) handleCallback(cb *tgbotapi.CallbackQuery) {
	// Callback bisa dari grup atau private (jika inline mode) — cukup ignore jika bukan dari allowed chat
	if !h.isAllowedChat(cb.Message.Chat.ID) {
		h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
		return
	}

	userID := cb.From.ID
	data := cb.Data
	ack := func() { h.api.Send(tgbotapi.NewCallback(cb.ID, "")) }

	switch {
	case data == "cancel":
		ack()
		h.sessions.Delete(userID)
		h.bulk.Delete(userID)
		deleteRedirectAwaitDomain(userID)
		deleteRollbackAwait(userID)
		deleteRemoveDomainAwait(userID)
		deleteRemoveDomainSelectAwait(userID)
		h.cleanupNewDomainSession(userID)
		addDomainSessions.delete(userID)
		setupSessions.delete(userID)
		listFilters.delete(userID)
		h.sendWithReplyKeyboard(cb.Message.Chat.ID, userID, "🚫 Dibatalkan.")
	case data == "confirm_yes":
		ack()
		h.handleCallbackConfirmYes(cb)
	case data == "confirm_no":
		ack()
		h.handleCallbackConfirmNo(cb)
	case data == "bulk_done":
		h.handleCallbackBulkDone(cb)
	case data == "bulk_confirm_yes":
		ack()
		h.handleCallbackBulkConfirmYes(cb)
	case data == "bulk_confirm_no":
		ack()
		h.handleCallbackBulkConfirmNo(cb)
	case strings.HasPrefix(data, "bulk_toggle:"):
		ack()
		h.handleCallbackBulkToggle(cb, strings.TrimPrefix(data, "bulk_toggle:"))
	case strings.HasPrefix(data, "rollback:"):
		ack()
		h.handleCallbackRollback(cb, strings.TrimPrefix(data, "rollback:"))
	case strings.HasPrefix(data, "adddomain_type:"):
		ack()
		h.handleCallbackAddDomainType(cb, strings.TrimPrefix(data, "adddomain_type:"))
	case data == "adddomain_skip_label":
		ack()
		h.handleCallbackAddDomainSkipLabel(cb)
	case data == "adddomain_confirm":
		ack()
		h.handleCallbackAddDomainConfirm(cb)
	case data == "setup_domain_new":
		ack()
		h.handleCallbackSetupDomainNew(cb)
	case data == "setup_domain_existing":
		ack()
		h.handleCallbackSetupDomainExisting(cb)
	case data == "setup_domain_start":
		ack()
		h.handleCallbackSetupDomainStart(cb)
	case data == "setup_add_more":
		ack()
		h.handleCallbackSetupAddMore(cb)
	case data == "setup_done":
		ack()
		h.handleCallbackSetupDone(cb)
	case strings.HasPrefix(data, "check_ns_status:"):
		ack()
		h.handleCallbackCheckNSStatus(cb, strings.TrimPrefix(data, "check_ns_status:"))
	}
}
