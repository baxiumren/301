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
	cf         cloudflare.Client
	sessions   *SessionStore
	bulk       *BulkStore
	configPath string
}

func NewHandler(api *tgbotapi.BotAPI, cfg *config.Config, cf cloudflare.Client, configPath string) *Handler {
	return &Handler{
		api:        api,
		cfg:        cfg,
		cf:         cf,
		sessions:   NewSessionStore(),
		bulk:       NewBulkStore(),
		configPath: configPath,
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

// --- keyboard helpers ---

func (h *Handler) replyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🌐 Ganti Redirect"),
			tgbotapi.NewKeyboardButton("🔀 Bulk Redirect"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Status"),
			tgbotapi.NewKeyboardButton("📜 History"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📖 Help"),
		),
	)
	kb.ResizeKeyboard = true
	return kb
}

func (h *Handler) cancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
		},
	)
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

func (h *Handler) sendWithReplyKeyboard(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = h.replyKeyboard()
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

func (h *Handler) sendWithInlineKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// --- main dispatcher ---

func (h *Handler) Handle(update tgbotapi.Update) {
	if update.MyChatMember != nil {
		newStatus := update.MyChatMember.NewChatMember.Status
		if newStatus == "member" || newStatus == "administrator" {
			h.sendWelcome(update.MyChatMember.Chat.ID)
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

	userID := update.Message.From.ID
	if !h.isAllowed(userID) {
		h.sendMd(update.Message.Chat.ID, fmt.Sprintf(
			"⛔ Kamu tidak memiliki akses.\n\nUser ID kamu: `%d`\nMinta admin untuk menambahkan ID kamu ke whitelist.",
			userID,
		))
		return
	}

	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			h.handleStartCommand(update.Message)
		case "help":
			h.handleHelpCommand(update.Message)
		case "redirect":
			h.handleRedirectCommand(update.Message)
		case "bulk":
			h.handleBulkCommand(update.Message)
		case "status":
			h.handleStatusCommand(update.Message)
		case "history":
			h.handleHistoryCommand(update.Message)
		case "adduser":
			h.handleAddUserCommand(update.Message)
		case "removeuser":
			h.handleRemoveUserCommand(update.Message)
		case "listusers":
			h.handleListUsersCommand(update.Message)
		}
		return
	}

	switch update.Message.Text {
	case "🌐 Ganti Redirect":
		h.handleRedirectCommand(update.Message)
	case "🔀 Bulk Redirect":
		h.handleBulkCommand(update.Message)
	case "📊 Status":
		h.handleStatusCommand(update.Message)
	case "📜 History":
		h.handleHistoryCommand(update.Message)
	case "📖 Help":
		h.handleHelpCommand(update.Message)
	default:
		h.handleURLInput(update.Message)
	}
}

// --- callback dispatcher ---

func (h *Handler) handleCallback(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID

	if !h.isAllowed(userID) {
		h.api.Send(tgbotapi.NewCallback(cb.ID, ""))
		h.send(cb.Message.Chat.ID, "⛔ Kamu tidak memiliki akses.")
		return
	}

	data := cb.Data
	ack := func() { h.api.Send(tgbotapi.NewCallback(cb.ID, "")) }

	switch {
	case data == "cancel":
		ack()
		h.sessions.Delete(userID)
		h.bulk.Delete(userID)
		h.sendWithReplyKeyboard(cb.Message.Chat.ID, "🚫 Dibatalkan.")
	case data == "confirm_yes":
		ack()
		h.handleCallbackConfirmYes(cb)
	case data == "confirm_no":
		ack()
		h.handleCallbackConfirmNo(cb)
	case data == "bulk_done":
		h.handleCallbackBulkDone(cb) // ack dihandle di dalam (bisa kirim alert)
	case data == "bulk_confirm_yes":
		ack()
		h.handleCallbackBulkConfirmYes(cb)
	case data == "bulk_confirm_no":
		ack()
		h.handleCallbackBulkConfirmNo(cb)
	case strings.HasPrefix(data, "bulk_toggle:"):
		ack()
		h.handleCallbackBulkToggle(cb, strings.TrimPrefix(data, "bulk_toggle:"))
	case strings.HasPrefix(data, "domain:"):
		ack()
		h.handleCallbackDomain(cb, strings.TrimPrefix(data, "domain:"))
	}
}
