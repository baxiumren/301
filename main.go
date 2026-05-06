package main

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/bot"
	"cf-redirect-bot/cloudflare"
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
		{Command: "redirect", Description: "Ganti URL redirect domain"},
		{Command: "status", Description: "Lihat URL redirect semua domain"},
	}
	if _, err := api.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("failed to set commands: %v", err)
	}

	cfClient := cloudflare.New(cfg.Cloudflare.Email, cfg.Cloudflare.APIKey)
	handler := bot.NewHandler(api, cfg, cfClient)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := api.GetUpdatesChan(u)

	log.Println("Bot started, listening for updates...")
	for update := range updates {
		handler.Handle(update)
	}
}
