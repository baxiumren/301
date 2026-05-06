package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func (h *Handler) sendWelcome(chatID int64) {
	text := "✨ *Selamat datang di CF Redirect Bot!*\n\n" +
		"Bot ini digunakan untuk mengganti URL tujuan redirect domain Cloudflare.\n\n" +
		"*Commands:*\n" +
		"/redirect — Ganti URL 1 domain\n" +
		"/bulk — Ganti URL beberapa domain sekaligus\n" +
		"/status — Lihat URL redirect semua domain\n" +
		"/history — Lihat riwayat perubahan\n" +
		"/help — Tampilkan bantuan ini\n\n" +
		"Atau gunakan tombol di bawah 👇"
	h.sendWithReplyKeyboard(chatID, text)
}

func (h *Handler) handleStartCommand(msg *tgbotapi.Message) {
	h.sendWelcome(msg.Chat.ID)
}

func (h *Handler) handleHelpCommand(msg *tgbotapi.Message) {
	text := "📖 *CF Redirect Bot — Bantuan*\n\n" +
		"*Redirect & Bulk:*\n" +
		"/redirect — Pilih 1 domain, ganti URL-nya\n" +
		"/bulk — Centang beberapa domain, ganti ke URL yang sama\n" +
		"/status — Lihat URL redirect semua domain saat ini\n" +
		"/history — Lihat 10 riwayat perubahan terakhir\n\n" +
		"*Whitelist:*\n" +
		"/adduser `<user_id>` — Tambah user ke whitelist\n" +
		"/removeuser `<user_id>` — Hapus user dari whitelist\n" +
		"/listusers — Lihat daftar user whitelist\n\n" +
		"*Cara Ganti Redirect:*\n" +
		"1. Tekan *🌐 Ganti Redirect*\n" +
		"2. Pilih domain\n" +
		"3. Kirim URL baru (`https://...`)\n" +
		"4. Konfirmasi → Selesai ✅\n\n" +
		"*Cara Bulk Redirect:*\n" +
		"1. Tekan *🔀 Bulk Redirect*\n" +
		"2. Centang domain yang mau diganti\n" +
		"3. Tekan *✅ Selesai Pilih*\n" +
		"4. Kirim URL baru\n" +
		"5. Konfirmasi → Selesai ✅"
	h.sendWithReplyKeyboard(msg.Chat.ID, text)
}
