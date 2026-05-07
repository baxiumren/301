package main

import (
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/bot"
	"cf-redirect-bot/config"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	api, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		log.Fatalf("failed to connect to Telegram: %v", err)
	}
	log.Printf("Authorized on account %s", api.Self.UserName)

	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Mulai bot & tampilkan menu"},
		{Command: "help", Description: "Panduan cara penggunaan"},
		{Command: "info", Description: "Status bot & Cloudflare"},
		{Command: "redirect", Description: "Ganti URL redirect 1 domain"},
		{Command: "bulk", Description: "Ganti URL beberapa domain sekaligus"},
		{Command: "list", Description: "Lihat URL redirect semua domain"},
		{Command: "history", Description: "Riwayat perubahan + tombol rollback"},
		{Command: "adddomain", Description: "Tambah domain baru (wizard step-by-step)"},
		{Command: "removedomain", Description: "Hapus domain dari bot"},
		{Command: "setcf", Description: "Ganti Cloudflare email & API key"},
	}
	if _, err := api.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("failed to set commands: %v", err)
	}

	handler := bot.NewHandler(api, cfg, "config.yaml")

	// Kirim notif startup ke grup jika allowed_chat_id sudah dikonfigurasi
	if cfg.AllowedChatID != 0 {
		startTime := time.Now().Format("02 Jan 2006 15:04:05")
		notif := fmt.Sprintf(
			"🟢 *Bot Online*\n\n"+
				"⏰ `%s`\n"+
				"🤖 @%s siap menerima perintah.",
			startTime, api.Self.UserName,
		)
		msg := tgbotapi.NewMessage(cfg.AllowedChatID, notif)
		msg.ParseMode = "Markdown"
		if _, err := api.Send(msg); err != nil {
			log.Printf("failed to send startup notification: %v", err)
		}
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "callback_query", "my_chat_member"}
	updates := api.GetUpdatesChan(u)

	log.Println("Bot started, listening for updates...")
	for update := range updates {
		handler.Handle(update)
	}
}
