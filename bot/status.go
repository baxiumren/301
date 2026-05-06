package bot

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
