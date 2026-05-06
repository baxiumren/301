package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (h *Handler) handleAddUserCommand(msg *tgbotapi.Message) {
	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		h.send(msg.Chat.ID, "⚠️ Format: /adduser <user_id>")
		return
	}
	newID, err := strconv.ParseInt(args, 10, 64)
	if err != nil {
		h.send(msg.Chat.ID, "⚠️ User ID tidak valid. Harus berupa angka.")
		return
	}
	for _, id := range h.cfg.Whitelist {
		if id == newID {
			h.send(msg.Chat.ID, fmt.Sprintf("ℹ️ User %d sudah ada di whitelist.", newID))
			return
		}
	}
	h.cfg.Whitelist = append(h.cfg.Whitelist, newID)
	if err := h.cfg.Save(h.configPath); err != nil {
		log.Printf("save config error: %v", err)
		h.cfg.Whitelist = h.cfg.Whitelist[:len(h.cfg.Whitelist)-1]
		h.send(msg.Chat.ID, "❌ Gagal menyimpan config.")
		return
	}
	h.send(msg.Chat.ID, fmt.Sprintf("✅ User %d berhasil ditambahkan ke whitelist.", newID))
}

func (h *Handler) handleRemoveUserCommand(msg *tgbotapi.Message) {
	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		h.send(msg.Chat.ID, "⚠️ Format: /removeuser <user_id>")
		return
	}
	targetID, err := strconv.ParseInt(args, 10, 64)
	if err != nil {
		h.send(msg.Chat.ID, "⚠️ User ID tidak valid. Harus berupa angka.")
		return
	}
	if targetID == msg.From.ID {
		h.send(msg.Chat.ID, "⚠️ Tidak bisa menghapus diri sendiri dari whitelist.")
		return
	}
	newList := make([]int64, 0, len(h.cfg.Whitelist))
	found := false
	for _, id := range h.cfg.Whitelist {
		if id == targetID {
			found = true
			continue
		}
		newList = append(newList, id)
	}
	if !found {
		h.send(msg.Chat.ID, fmt.Sprintf("ℹ️ User %d tidak ada di whitelist.", targetID))
		return
	}
	old := h.cfg.Whitelist
	h.cfg.Whitelist = newList
	if err := h.cfg.Save(h.configPath); err != nil {
		log.Printf("save config error: %v", err)
		h.cfg.Whitelist = old
		h.send(msg.Chat.ID, "❌ Gagal menyimpan config.")
		return
	}
	h.send(msg.Chat.ID, fmt.Sprintf("✅ User %d berhasil dihapus dari whitelist.", targetID))
}

func (h *Handler) handleListUsersCommand(msg *tgbotapi.Message) {
	if len(h.cfg.Whitelist) == 0 {
		h.send(msg.Chat.ID, "ℹ️ Whitelist kosong.")
		return
	}
	var sb strings.Builder
	sb.WriteString("👥 *Daftar User Whitelist:*\n\n")
	for i, id := range h.cfg.Whitelist {
		sb.WriteString(fmt.Sprintf("%d. `%d`\n", i+1, id))
	}
	listMsg := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	listMsg.ParseMode = "Markdown"
	if _, err := h.api.Send(listMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}
