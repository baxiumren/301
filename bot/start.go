package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func (h *Handler) sendWelcome(chatID int64, userID int64) {
	text := "✨ *Selamat datang di CF Redirect Bot!*\n\n" +
		"Bot ini digunakan untuk mengganti URL tujuan redirect domain Cloudflare.\n\n" +
		"*Commands:*\n" +
		"/redirect — Ganti URL 1 domain\n" +
		"/bulk — Ganti URL beberapa domain sekaligus\n" +
		"/list — Lihat semua domain | /list namalabel untuk filter\n" +
		"/history — Lihat riwayat + tombol rollback\n" +
		"/help — Tampilkan bantuan ini\n\n" +
		"Atau gunakan tombol di bawah 👇"
	h.sendWithReplyKeyboard(chatID, userID, text)
}

func (h *Handler) handleStartCommand(msg *tgbotapi.Message) {
	h.sendWelcome(msg.Chat.ID, msg.From.ID)
}

func (h *Handler) handleHelpCommand(msg *tgbotapi.Message) {
	text := "📖 *CF Redirect Bot — Bantuan*\n\n" +
		"━━━━━━━━━━━━━━━━━━━━\n" +
		"*🔀 Redirect*\n" +
		"• 🌐 *Ganti Redirect* — Ganti URL redirect 1 domain\n" +
		"• 🔀 *Bulk Redirect* — Ganti beberapa domain ke URL yang sama\n\n" +
		"*📋 Lihat*\n" +
		"• 📋 *List URL* — Lihat semua domain & URL redirect aktif\n" +
		"• `/list <label>` — Filter domain berdasarkan label\n" +
		"• 📜 *History* — Riwayat perubahan + tombol ↩️ Rollback\n\n" +
		"*⚙️ Konfigurasi*\n" +
		"• ⚙️ *Kelola Domain* — Tambah/atur domain redirect\n" +
		"• `/adddomain` — Tambah domain baru (wizard step-by-step)\n" +
		"• 🗑️ *Hapus Domain* — Hapus domain (via ⚙️ Kelola Domain)\n" +
		"• `/setcf <email> <api_key>` — Ganti Cloudflare credentials\n" +
		"• `/info` — Status bot & Cloudflare\n\n" +
		"━━━━━━━━━━━━━━━━━━━━\n" +
		"*🌐 Cara Ganti Redirect:*\n" +
		"1. Tekan 🌐 *Ganti Redirect*\n" +
		"2. Pilih domain _(otomatis jika hanya 1 domain)_\n" +
		"3. Kirim URL baru `https://...`\n" +
		"4. Konfirmasi ✅ → Selesai!\n\n" +
		"*🔀 Cara Bulk Redirect:*\n" +
		"1. Tekan 🔀 *Bulk Redirect*\n" +
		"2. Centang ☑️ domain yang mau diganti\n" +
		"3. Tekan *✅ Selesai Pilih*\n" +
		"4. Kirim URL baru `https://...`\n" +
		"5. Konfirmasi ✅ → Selesai!"
	h.sendWithReplyKeyboard(msg.Chat.ID, msg.From.ID, text)
}
