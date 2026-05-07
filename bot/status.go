package bot

import (
	"fmt"
	"log"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/config"
)

// ─────────────────────────────────────────
//  List filter session
// ─────────────────────────────────────────

type listFilterStore struct {
	mu    sync.Mutex
	store map[int64]bool
}

var listFilters = &listFilterStore{store: make(map[int64]bool)}

func (s *listFilterStore) set(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[userID] = true
}

func (s *listFilterStore) has(userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store[userID]
}

func (s *listFilterStore) delete(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, userID)
}

// ─────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────

type domainResult struct {
	index      int
	name       string
	label      string
	domainType string
	url        string
	err        error
	cfAccount  string
}

// uniqueLabels returns sorted unique non-empty labels from domains.
func uniqueLabels(domains []config.Domain) []string {
	seen := make(map[string]bool)
	var out []string
	for _, d := range domains {
		if d.Label != "" && !seen[d.Label] {
			seen[d.Label] = true
			out = append(out, d.Label)
		}
	}
	return out
}

// ─────────────────────────────────────────
//  Label picker keyboard
// ─────────────────────────────────────────

func (h *Handler) showListLabelPicker(chatID int64, labels []string) {
	var rows [][]tgbotapi.KeyboardButton

	// Tombol ALL paling atas
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("🌐 Semua Domain"),
	))

	// Label per 2 kolom
	var row []tgbotapi.KeyboardButton
	for i, label := range labels {
		row = append(row, tgbotapi.NewKeyboardButton("🏷 "+label))
		if len(row) == 2 || i == len(labels)-1 {
			rows = append(rows, row)
			row = nil
		}
	}

	// Cancel di bawah
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))

	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, "📋 *Pilih label yang ingin dilihat:*")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// ─────────────────────────────────────────
//  handleListCommand — entry point
// ─────────────────────────────────────────

func (h *Handler) handleListCommand(msg *tgbotapi.Message) {
	if !h.isCFConfigured() {
		h.sendMd(msg.Chat.ID, "⚠️ Cloudflare belum dikonfigurasi.\n\nGunakan `/setcf <email> <api_key>` terlebih dahulu.")
		return
	}
	if len(h.cfg.Domains) == 0 {
		h.sendMd(msg.Chat.ID, "⚠️ Belum ada domain.\n\nGunakan ⚙️ *Kelola Domain* untuk menambahkan domain.")
		return
	}

	// Kalau ada argumen (misal /list PROMO), langsung fetch
	filter := strings.ToLower(strings.TrimSpace(msg.CommandArguments()))
	if filter != "" {
		h.fetchAndShowList(msg.Chat.ID, msg.From.ID, filter)
		return
	}

	// Kalau tidak ada argumen, cek jumlah label unik
	labels := uniqueLabels(h.cfg.Domains)
	if len(labels) <= 1 {
		// 0 atau 1 label → langsung tampil semua
		h.fetchAndShowList(msg.Chat.ID, msg.From.ID, "")
		return
	}

	// Lebih dari 1 label → tampil picker
	listFilters.set(msg.From.ID)
	h.showListLabelPicker(msg.Chat.ID, labels)
}

// handleListFilterInput: dipanggil dari handleURLInput saat listFilter aktif.
func (h *Handler) handleListFilterInput(msg *tgbotapi.Message) {
	listFilters.delete(msg.From.ID)

	input := strings.TrimSpace(msg.Text)
	if input == "🌐 Semua Domain" {
		h.fetchAndShowList(msg.Chat.ID, msg.From.ID, "")
		return
	}
	// Label button format: "🏷 LABELNAME"
	label := strings.TrimPrefix(input, "🏷 ")
	h.fetchAndShowList(msg.Chat.ID, msg.From.ID, strings.ToLower(label))
}

// ─────────────────────────────────────────
//  Fetch & display
// ─────────────────────────────────────────

func (h *Handler) fetchAndShowList(chatID int64, userID int64, filter string) {
	var targets []config.Domain
	for _, d := range h.cfg.Domains {
		if filter == "" || strings.ToLower(d.Label) == filter {
			targets = append(targets, d)
		}
	}

	if len(targets) == 0 {
		h.sendWithReplyKeyboard(chatID, userID, fmt.Sprintf(
			"❌ Tidak ada domain dengan label *%s*.",
			strings.ToUpper(filter),
		))
		return
	}

	// Kirim loading indicator dulu
	loadingMsg, loadErr := h.api.Send(tgbotapi.NewMessage(chatID, "⏳ Mengambil data dari Cloudflare..."))

	// Fetch semua domain secara concurrent
	results := make([]domainResult, len(targets))
	var wg sync.WaitGroup
	for i, d := range targets {
		wg.Add(1)
		go func(idx int, domain config.Domain) {
			defer wg.Done()
			url, err := h.cfFor(domain).GetCurrentURL(domain)
			results[idx] = domainResult{
				index:      idx,
				name:       domain.Name,
				label:      domain.Label,
				domainType: domain.Type,
				url:        url,
				err:        err,
				cfAccount:  domain.CFAccount,
			}
		}(i, d)
	}
	wg.Wait()

	// Hapus loading message
	if loadErr == nil {
		h.api.Request(tgbotapi.NewDeleteMessage(chatID, loadingMsg.MessageID))
	}

	// Kelompokkan hasil per akun CF (pertahankan urutan akun)
	type accountGroup struct {
		accountName string
		email       string
		results     []domainResult
	}
	var groups []accountGroup
	groupIdx := map[string]int{} // accountName → index di groups

	for _, r := range results {
		accName := r.cfAccount
		if accName == "" {
			accName = "default"
		}
		if _, exists := groupIdx[accName]; !exists {
			email := ""
			if acc := h.cfg.FindAccount(accName); acc != nil {
				email = acc.Email
			}
			groupIdx[accName] = len(groups)
			groups = append(groups, accountGroup{accountName: accName, email: email})
		}
		idx := groupIdx[accName]
		groups[idx].results = append(groups[idx].results, r)
	}

	var sb strings.Builder
	if filter != "" {
		sb.WriteString(fmt.Sprintf("📋 *List Domain* [%s]:\n\n", strings.ToUpper(filter)))
	} else {
		sb.WriteString("📋 *List Semua Domain:*\n\n")
	}

	multiAccount := len(groups) > 1
	for _, g := range groups {
		// Header pemisah per akun (hanya tampil kalau ada >1 akun)
		if multiAccount {
			sb.WriteString(fmt.Sprintf("━━━ ☁️ *%s* (`%s`) ━━━\n\n", g.accountName, g.email))
		}
		for _, r := range g.results {
			display := domainLabel(r.name, r.label)
			typeLabel := "Redirect Rules"
			if r.domainType == "page_rules" {
				typeLabel = "Page Rules"
			}
			if r.err != nil {
				log.Printf("list error for %s: %v", r.name, r.err)
				sb.WriteString(fmt.Sprintf(
					"🔹 *%s* _%s_\n❌ Gagal mengambil URL\n\n",
					display, typeLabel,
				))
				continue
			}
			sb.WriteString(fmt.Sprintf("🔹 *%s* _%s_\n→ `%s`\n\n", display, typeLabel, r.url))
		}
	}

	listMsg := tgbotapi.NewMessage(chatID, sb.String())
	listMsg.ParseMode = "Markdown"
	listMsg.ReplyMarkup = h.replyKeyboardFor(userID)
	if _, err := h.api.Send(listMsg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// ─────────────────────────────────────────
//  /info command
// ─────────────────────────────────────────

func (h *Handler) handleInfoCommand(msg *tgbotapi.Message) {
	var cfText string
	if len(h.cfg.CFAccounts) == 0 {
		cfText = "❌ Belum dikonfigurasi"
	} else {
		for _, acc := range h.cfg.CFAccounts {
			cfText += fmt.Sprintf("• *%s*: `%s` ✅\n", acc.Name, acc.Email)
		}
		cfText = strings.TrimRight(cfText, "\n")
	}

	domainCount := len(h.cfg.Domains)
	var domainText string
	if domainCount == 0 {
		domainText = "⚠️ Belum ada domain\n_Gunakan ⚙️ Kelola Domain untuk tambah domain_"
	} else {
		domainText = fmt.Sprintf("✅ %d domain terdaftar", domainCount)
	}

	text := fmt.Sprintf(
		"ℹ️ *Informasi Bot*\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"☁️ *Cloudflare Accounts:*\n"+
			"%s\n\n"+
			"🌐 *Domain*\n"+
			"%s\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"💡 Gunakan `📋 List URL` untuk lihat semua domain + URL aktifnya",
		cfText, domainText,
	)
	h.sendWithReplyKeyboard(msg.Chat.ID, msg.From.ID, text)
}
