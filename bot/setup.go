package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"cf-redirect-bot/cloudflare"
	"cf-redirect-bot/config"
)

// ─────────────────────────────────────────
//  Prompt text constants
// ─────────────────────────────────────────

const zoneIDPrompt = "🌐 *Langkah 3/5 — Zone ID*\n\n" +
	"Masukkan *Zone ID* domain ini.\n\n" +
	"📍 Cara ambil:\n" +
	"1. Buka cloudflare.com → pilih domain\n" +
	"2. Scroll ke kanan bawah halaman *Overview*\n" +
	"3. Salin *Zone ID* yang tertera\n\n" +
	"Contoh:\n`1a2b3c4d5e6f7890abcdef1234567890`"

const rulesetIDPrompt = "🌐 *Langkah 4/5 — Ruleset ID*\n\n" +
	"Masukkan *Ruleset ID* dari redirect rule kamu.\n\n" +
	"📍 Cara ambil:\n" +
	"1. Cloudflare → pilih domain → *Rules → Redirect Rules*\n" +
	"2. Klik nama ruleset\n" +
	"3. Salin ID dari URL browser:\n" +
	"`...cloudflare.com/.../rulesets/` *RULESET\\_ID*\n\n" +
	"Contoh:\n`1a2b3c4d5e6f7890abcdef1234567890`"

const ruleIDPromptRedirect = "🌐 *Langkah 5/5 — Rule ID*\n\n" +
	"Masukkan *Rule ID* dari redirect rule spesifik.\n\n" +
	"📍 Cara ambil:\n" +
	"1. Cloudflare → pilih domain → *Rules → Redirect Rules*\n" +
	"2. Klik rule yang kamu buat\n" +
	"3. Salin ID dari URL browser:\n" +
	"`...rulesets/RULESET_ID/rules/` *RULE\\_ID*\n\n" +
	"Contoh:\n`9z8y7x6w5v4u3t2s1r0q9p8o7n6m5l4k`"

const ruleIDPromptPageRules = "🌐 *Langkah 4/4 — Rule ID*\n\n" +
	"Masukkan *Rule ID* dari Page Rule kamu.\n\n" +
	"📍 Cara ambil:\n" +
	"1. Cloudflare → pilih domain → *Rules → Page Rules*\n" +
	"2. Klik *Edit* pada rule yang kamu buat\n" +
	"3. Salin ID dari URL browser:\n" +
	"`...cloudflare.com/.../page-rules/` *RULE\\_ID*\n\n" +
	"Contoh:\n`12345678`"

// ─────────────────────────────────────────
//  Setup Wizard Session (CF credentials)
// ─────────────────────────────────────────

type setupPhase string

const (
	setupCFEmail  setupPhase = "cf_email"
	setupCFAPIKey setupPhase = "cf_apikey"
	// Add CF account flow
	setupAddCFName  setupPhase = "add_cf_name"
	setupAddCFEmail setupPhase = "add_cf_email"
	setupAddCFKey   setupPhase = "add_cf_key"
	// Edit CF account name flow
	setupEditCFSelect setupPhase = "edit_cf_select"
	setupEditCFName   setupPhase = "edit_cf_name"
	// Delete CF account flow
	setupDeleteCFSelect  setupPhase = "delete_cf_select"
	setupDeleteCFConfirm setupPhase = "delete_cf_confirm"
)

type SetupSession struct {
	Phase       setupPhase
	TempEmail   string
	TempName    string // untuk add CF account flow
	TempOldName string // untuk edit CF account name flow
	ExpiresAt   time.Time
}

type setupSessionStore struct {
	mu    sync.Mutex
	store map[int64]*SetupSession
}

var setupSessions = &setupSessionStore{store: make(map[int64]*SetupSession)}

func (s *setupSessionStore) set(userID int64, sess *SetupSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.ExpiresAt = time.Now().Add(10 * time.Minute)
	s.store[userID] = sess
}

func (s *setupSessionStore) get(userID int64) (*SetupSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.store[userID]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.store, userID)
		return nil, false
	}
	return sess, true
}

func (s *setupSessionStore) delete(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, userID)
}

// ─────────────────────────────────────────
//  Add Domain Session
// ─────────────────────────────────────────

type addDomainStep string

const (
	stepCFAccount        addDomainStep = "cf_account" // pilih akun CF (jika ada >1)
	stepName             addDomainStep = "name"
	stepLabel            addDomainStep = "label"
	stepZoneID           addDomainStep = "zone_id"
	stepType             addDomainStep = "type"
	stepPickRule         addDomainStep = "pick_rule"  // pilih dari list auto-fetch
	stepRulesetID        addDomainStep = "ruleset_id" // fallback manual
	stepRuleID           addDomainStep = "rule_id"    // fallback manual
	stepConfirm          addDomainStep = "confirm"
	// Khusus flow domain baru (IsNewDomain=true)
	stepNewDomainType        addDomainStep = "new_domain_type"      // pilih V1/V2
	stepNewDomainURL         addDomainStep = "new_domain_url"       // URL redirect tujuan → AddZone
	stepNewDomainDNSCheck    addDomainStep = "new_domain_dns_check" // cek DNS existing
	stepNewDomainDNSType     addDomainStep = "new_domain_dns_type"  // A/AAAA/CNAME
	stepNewDomainDNSName     addDomainStep = "new_domain_dns_name"  // nama record
	stepNewDomainDNSVal      addDomainStep = "new_domain_dns_val"   // IP / hostname
	stepNewDomainDNSPrx      addDomainStep = "new_domain_dns_prx"   // proxy on/off
	stepNewDomainDNSMoreOrGo addDomainStep = "new_domain_dns_more"  // tambah dns lagi / lanjut
	// Hapus DNS: checkbox selection
	stepNewDomainDNSDelete addDomainStep = "new_domain_dns_delete"
	// Edit DNS: pilih record dulu, lalu wizard
	stepNewDomainDNSEditSelect addDomainStep = "new_domain_dns_edit_select"
	stepNewDomainDNSEditType   addDomainStep = "new_domain_dns_edit_type"
	stepNewDomainDNSEditName   addDomainStep = "new_domain_dns_edit_name"
	stepNewDomainDNSEditVal    addDomainStep = "new_domain_dns_edit_val"
	stepNewDomainDNSEditProxy  addDomainStep = "new_domain_dns_edit_proxy"
)

type AddDomainSession struct {
	Step          addDomainStep
	Domain        config.Domain
	FromSetup     bool   // true = ada tombol "Tambah Lagi / Selesai Setup" setelah save
	IsNewDomain   bool   // true = daftarkan domain baru ke CF (bukan auto-discover)
	CFAccountName string // nama akun CF yang dipilih
	ExpiresAt     time.Time
	// Auto-discovered rules (diisi saat stepPickRule)
	DiscoveredRules     []cloudflare.DiscoveredRule
	DiscoveredPageRules []cloudflare.DiscoveredPageRule
	// Untuk flow new domain
	TempZoneID      string   // zone ID setelah AddZone (untuk cleanup saat cancel)
	TempNameServers []string // nameservers dari AddZone
	TempRedirectURL string   // URL redirect tujuan
	TempDNSType     string   // tipe DNS record
	TempDNSName     string   // nama DNS record
	TempDNSValue    string   // value/IP DNS record
	TempDNSProxy    bool     // proxy status
	// Untuk fitur hapus/edit DNS existing
	ExistingDNSRecords []cloudflare.DNSRecord // DNS records yang sudah ada di zone
	DNSDeleteSelected  map[int]bool           // index record yang dipilih untuk dihapus
	DNSEditIdx         int                    // index record yang sedang diedit
}

type addDomainStore struct {
	mu    sync.Mutex
	store map[int64]*AddDomainSession
}

var addDomainSessions = &addDomainStore{store: make(map[int64]*AddDomainSession)}

func (a *addDomainStore) set(userID int64, sess *AddDomainSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sess.ExpiresAt = time.Now().Add(10 * time.Minute)
	a.store[userID] = sess
}

func (a *addDomainStore) get(userID int64) (*AddDomainSession, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sess, ok := a.store[userID]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(a.store, userID)
		return nil, false
	}
	return sess, true
}

func (a *addDomainStore) delete(userID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.store, userID)
}

// ─────────────────────────────────────────
//  Pending NS Check Store (zona menunggu aktif)
// ─────────────────────────────────────────

var pendingNSCheck = struct {
	mu    sync.Mutex
	store map[int64]string // userID → zoneID
}{store: make(map[int64]string)}

func setPendingNSCheck(userID int64, zoneID string) {
	pendingNSCheck.mu.Lock()
	defer pendingNSCheck.mu.Unlock()
	pendingNSCheck.store[userID] = zoneID
}

func getPendingNSCheck(userID int64) (string, bool) {
	pendingNSCheck.mu.Lock()
	defer pendingNSCheck.mu.Unlock()
	zoneID, ok := pendingNSCheck.store[userID]
	return zoneID, ok
}

func deletePendingNSCheck(userID int64) {
	pendingNSCheck.mu.Lock()
	defer pendingNSCheck.mu.Unlock()
	delete(pendingNSCheck.store, userID)
}

// ─────────────────────────────────────────
//  Setup mode helpers
// ─────────────────────────────────────────

func (h *Handler) isSetupMode() bool {
	return h.cfg.AllowedChatID == 0
}

// sendSetupWelcome dipanggil saat grup pertama kali terdaftar (/start di grup).
// Langsung mulai wizard CF — minta email.
func (h *Handler) sendSetupWelcome(chatID int64, userID int64) {
	h.sendMd(chatID,
		"✨ *Grup ini sekarang terdaftar sebagai grup bot!*\n\n"+
			"Mari kita setup Cloudflare dalam beberapa langkah singkat.\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"*📧 Langkah 1 dari 2 — Cloudflare Email*\n\n"+
			"Masukkan email yang kamu pakai untuk login ke Cloudflare.\n\n"+
			"📍 Cek di: cloudflare.com → klik foto profil kanan atas\n\n"+
			"Contoh:\n`user@gmail.com`",
	)
	setupSessions.set(userID, &SetupSession{Phase: setupCFEmail})
}

// handleSetupInput menangani teks saat setup wizard CF credentials berlangsung.
func (h *Handler) handleSetupInput(msg *tgbotapi.Message, sess *SetupSession) {
	input := strings.TrimSpace(msg.Text)

	switch sess.Phase {
	case setupCFEmail:
		if !strings.Contains(input, "@") {
			h.send(msg.Chat.ID, "⚠️ Email tidak valid. Coba lagi:")
			return
		}
		sess.TempEmail = input
		sess.Phase = setupCFAPIKey
		setupSessions.set(msg.From.ID, sess)
		h.sendMd(msg.Chat.ID,
			fmt.Sprintf("✅ Email: *%s*\n\n"+
				"━━━━━━━━━━━━━━━━━━━━\n"+
				"*🔑 Langkah 2 dari 2 — Global API Key*\n\n"+
				"Masukkan *Global API Key* akun Cloudflare kamu.\n\n"+
				"📍 Cara ambil:\n"+
				"1. Buka cloudflare.com\n"+
				"2. Klik foto profil → *My Profile*\n"+
				"3. Scroll ke bawah → *API Tokens*\n"+
				"4. Klik *View* di bagian *Global API Key*\n"+
				"5. Masukkan password → salin kode yang muncul\n\n"+
				"Contoh:\n`1a2b3c4d5e6f7890abcdef1234567890abcde`",
				input),
		)

	case setupCFAPIKey:
		if len(input) < 10 {
			h.send(msg.Chat.ID, "⚠️ API Key terlalu pendek. Coba lagi:")
			return
		}
		// Update or create "default" account
		found := false
		for i := range h.cfg.CFAccounts {
			if h.cfg.CFAccounts[i].Name == "default" {
				h.cfg.CFAccounts[i].Email = sess.TempEmail
				h.cfg.CFAccounts[i].APIKey = input
				found = true
				break
			}
		}
		if !found {
			h.cfg.CFAccounts = append(h.cfg.CFAccounts, config.CFAccount{
				Name:   "default",
				Email:  sess.TempEmail,
				APIKey: input,
			})
		}
		if err := h.cfg.Save(h.configPath); err != nil {
			log.Printf("save config error: %v", err)
			h.send(msg.Chat.ID, "❌ Gagal menyimpan config. Coba lagi.")
			return
		}
		setupSessions.delete(msg.From.ID)
		// Hapus pesan yang berisi API Key dari history chat
		h.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID))
		h.sendMd(msg.Chat.ID, fmt.Sprintf(
			"✅ *Cloudflare terhubung!*\n📧 Email: %s\n\nSekarang tambahkan domain kamu 👇",
			sess.TempEmail,
		))
		h.showDomainChoiceButtons(msg.Chat.ID)

	// ── Add CF account wizard ─────────────────────────────────────────────────

	case setupAddCFName:
		name := strings.TrimSpace(input)
		if name == "" || strings.ContainsAny(name, " \t") {
			h.send(msg.Chat.ID, "⚠️ Nama tidak boleh kosong atau mengandung spasi. Coba lagi:")
			return
		}
		if h.cfg.FindAccount(name) != nil {
			h.sendMd(msg.Chat.ID, fmt.Sprintf("⚠️ Akun bernama *%s* sudah ada. Gunakan nama lain:", name))
			return
		}
		sess.TempName = name
		sess.Phase = setupAddCFEmail
		setupSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✅ Nama: *%s*\n\n"+
				"*📧 Email Cloudflare*\n\n"+
				"Masukkan email yang dipakai login ke akun CF ini:\n\n"+
				"Contoh:\n`user2@gmail.com`",
			name,
		))

	case setupAddCFEmail:
		if !strings.Contains(input, "@") {
			h.send(msg.Chat.ID, "⚠️ Email tidak valid. Coba lagi:")
			return
		}
		sess.TempEmail = input
		sess.Phase = setupAddCFKey
		setupSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✅ Email: *%s*\n\n"+
				"*🔑 Global API Key*\n\n"+
				"Masukkan Global API Key untuk akun ini.\n\n"+
				"📍 cloudflare.com → My Profile → API Tokens → *Global API Key*\n\n"+
				"Contoh:\n`1a2b3c4d5e6f7890abcdef1234567890abcde`",
			input,
		))

	case setupAddCFKey:
		if len(input) < 10 {
			h.send(msg.Chat.ID, "⚠️ API Key terlalu pendek. Coba lagi:")
			return
		}
		newAcc := config.CFAccount{
			Name:   sess.TempName,
			Email:  sess.TempEmail,
			APIKey: input,
		}
		h.cfg.CFAccounts = append(h.cfg.CFAccounts, newAcc)
		if err := h.cfg.Save(h.configPath); err != nil {
			log.Printf("save config error: %v", err)
			h.cfg.CFAccounts = h.cfg.CFAccounts[:len(h.cfg.CFAccounts)-1]
			h.send(msg.Chat.ID, "❌ Gagal menyimpan config.")
			return
		}
		setupSessions.delete(msg.From.ID)
		h.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID))
		h.sendWithReplyKeyboard(msg.Chat.ID, msg.From.ID, fmt.Sprintf(
			"✅ *Akun CF berhasil ditambahkan!*\n\n☁️ Nama: *%s*\n📧 Email: `%s`\n\n"+
				"Domain baru yang ditambahkan sekarang bisa pakai akun ini.",
			sess.TempName, sess.TempEmail,
		))

	// ── Edit CF account name flow ─────────────────────────────────────────────

	case setupEditCFSelect:
		// Strip prefix "✏️ " dari tombol
		name := strings.TrimPrefix(input, "✏️ ")
		if h.cfg.FindAccount(name) == nil {
			h.sendEditCFAccountPicker(msg.Chat.ID)
			return
		}
		sess.TempOldName = name
		sess.Phase = setupEditCFName
		setupSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✏️ *Edit Nama Akun*\n\nAkun: *%s*\n\nMasukkan *nama baru* untuk akun ini:\n\n"+
				"Contoh:\n`bisnis-baru`\n`akun-utama`",
			name,
		))

	case setupEditCFName:
		newName := strings.TrimSpace(input)
		if newName == "" || strings.ContainsAny(newName, " \t") {
			h.send(msg.Chat.ID, "⚠️ Nama tidak boleh kosong atau mengandung spasi. Coba lagi:")
			return
		}
		if newName != sess.TempOldName && h.cfg.FindAccount(newName) != nil {
			h.sendMd(msg.Chat.ID, fmt.Sprintf("⚠️ Nama *%s* sudah dipakai akun lain. Gunakan nama lain:", newName))
			return
		}
		// Update nama di CFAccounts
		for i := range h.cfg.CFAccounts {
			if h.cfg.CFAccounts[i].Name == sess.TempOldName {
				h.cfg.CFAccounts[i].Name = newName
				break
			}
		}
		// Update referensi di semua domain yang pakai akun ini
		for i := range h.cfg.Domains {
			if h.cfg.Domains[i].CFAccount == sess.TempOldName {
				h.cfg.Domains[i].CFAccount = newName
			}
		}
		if err := h.cfg.Save(h.configPath); err != nil {
			log.Printf("save config error: %v", err)
			// Rollback
			for i := range h.cfg.CFAccounts {
				if h.cfg.CFAccounts[i].Name == newName {
					h.cfg.CFAccounts[i].Name = sess.TempOldName
					break
				}
			}
			for i := range h.cfg.Domains {
				if h.cfg.Domains[i].CFAccount == newName {
					h.cfg.Domains[i].CFAccount = sess.TempOldName
				}
			}
			h.send(msg.Chat.ID, "❌ Gagal menyimpan config.")
			return
		}
		setupSessions.delete(msg.From.ID)
		h.sendWithReplyKeyboard(msg.Chat.ID, msg.From.ID, fmt.Sprintf(
			"✅ *Nama akun berhasil diubah!*\n\n*%s* → *%s*",
			sess.TempOldName, newName,
		))

	// ── Delete CF account flow ────────────────────────────────────────────────

	case setupDeleteCFSelect:
		name := strings.TrimPrefix(input, "🗑️ ")
		if h.cfg.FindAccount(name) == nil {
			h.sendDeleteCFAccountPicker(msg.Chat.ID)
			return
		}
		// Cek apakah ada domain yang pakai akun ini
		var linked []string
		for _, d := range h.cfg.Domains {
			if d.CFAccount == name {
				linked = append(linked, d.Name)
			}
		}
		if len(linked) > 0 {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("⚠️ *Akun %s tidak bisa dihapus.*\n\n", name))
			sb.WriteString("Domain berikut masih menggunakan akun ini:\n")
			for _, d := range linked {
				sb.WriteString("• " + d + "\n")
			}
			sb.WriteString("\nHapus atau pindahkan domain-domain tersebut dulu.")
			setupSessions.delete(msg.From.ID)
			h.sendWithReplyKeyboard(msg.Chat.ID, msg.From.ID, sb.String())
			return
		}
		sess.TempOldName = name
		sess.Phase = setupDeleteCFConfirm
		setupSessions.set(msg.From.ID, sess)
		acc := h.cfg.FindAccount(name)
		kb := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("✅ Ya, Hapus Akun"),
				tgbotapi.NewKeyboardButton("❌ Cancel"),
			),
		)
		kb.ResizeKeyboard = true
		out := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
			"🗑️ *Hapus Akun CF*\n\n"+
				"Nama: *%s*\n"+
				"Email: `%s`\n\n"+
				"⚠️ Akun ini hanya dihapus dari daftar bot.\n"+
				"Akun Cloudflare kamu *tidak* akan terpengaruh.\n\n"+
				"Yakin mau hapus?",
			name, acc.Email,
		))
		out.ParseMode = "Markdown"
		out.ReplyMarkup = kb
		if _, err := h.api.Send(out); err != nil {
			log.Printf("send error: %v", err)
		}
	}
}

// showCFAccountMenu: tampilkan daftar akun CF + opsi tambah akun baru.
func (h *Handler) showCFAccountMenu(chatID int64) {
	var sb strings.Builder
	sb.WriteString("☁️ *Akun Cloudflare*\n\n")
	if len(h.cfg.CFAccounts) == 0 {
		sb.WriteString("_Belum ada akun CF terdaftar._\n\n")
	} else {
		for i, acc := range h.cfg.CFAccounts {
			sb.WriteString(fmt.Sprintf("%d. *%s*\n📧 `%s`\n\n", i+1, acc.Name, acc.Email))
		}
	}

	var rows [][]tgbotapi.KeyboardButton
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("➕ Tambah Akun CF"),
	))
	if len(h.cfg.CFAccounts) > 0 {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✏️ Edit Nama Akun"),
			tgbotapi.NewKeyboardButton("🗑️ Hapus Akun CF"),
		))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	out := tgbotapi.NewMessage(chatID, sb.String())
	out.ParseMode = "Markdown"
	out.ReplyMarkup = kb
	if _, err := h.api.Send(out); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendEditCFAccountPicker: tampilkan pilihan akun CF yang mau diedit namanya.
func (h *Handler) sendEditCFAccountPicker(chatID int64) {
	var rows [][]tgbotapi.KeyboardButton
	var sb strings.Builder
	sb.WriteString("✏️ *Edit Nama Akun CF*\n\nPilih akun yang mau diganti namanya:\n\n")
	for i, acc := range h.cfg.CFAccounts {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✏️ "+acc.Name),
		))
		sb.WriteString(fmt.Sprintf("%d. *%s* — `%s`\n", i+1, acc.Name, acc.Email))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	out := tgbotapi.NewMessage(chatID, sb.String())
	out.ParseMode = "Markdown"
	out.ReplyMarkup = kb
	if _, err := h.api.Send(out); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendDeleteCFAccountPicker: tampilkan pilihan akun CF yang mau dihapus.
func (h *Handler) sendDeleteCFAccountPicker(chatID int64) {
	var rows [][]tgbotapi.KeyboardButton
	var sb strings.Builder
	sb.WriteString("🗑️ *Hapus Akun CF*\n\nPilih akun yang mau dihapus dari bot:\n\n")
	for i, acc := range h.cfg.CFAccounts {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🗑️ "+acc.Name),
		))
		sb.WriteString(fmt.Sprintf("%d. *%s* — `%s`\n", i+1, acc.Name, acc.Email))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ Cancel"),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	out := tgbotapi.NewMessage(chatID, sb.String())
	out.ParseMode = "Markdown"
	out.ReplyMarkup = kb
	if _, err := h.api.Send(out); err != nil {
		log.Printf("send error: %v", err)
	}
}

// showDomainChoiceButtons tampilkan menu kelola domain di reply keyboard bawah.
func (h *Handler) showDomainChoiceButtons(chatID int64) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Tambah Domain"),
			tgbotapi.NewKeyboardButton("🔗 Sudah Ada di CF"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🗑️ Hapus Domain"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, "⚙️ *Kelola Domain*\n\nMau ngapain?")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendRemoveDomainSelectKeyboard: tampilkan semua domain sebagai pilihan yang mau dihapus.
func (h *Handler) sendRemoveDomainSelectKeyboard(chatID int64) {
	var rows [][]tgbotapi.KeyboardButton
	var sb strings.Builder
	sb.WriteString("🗑️ *Hapus Domain*\n\nPilih domain yang mau dihapus:\n\n")
	for i, d := range h.cfg.Domains {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🗑️ "+domainLabel(d.Name, d.Label)),
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

// handleRemoveDomainSelect: dipanggil dari handleURLInput ketika user sedang memilih domain yang mau dihapus.
func (h *Handler) handleRemoveDomainSelect(msg *tgbotapi.Message) {
	userID := msg.From.ID
	input := strings.TrimSpace(msg.Text)

	// Strip prefix "🗑️ " dari teks tombol
	label := strings.TrimPrefix(input, "🗑️ ")

	found := h.findDomainByLabel(label)
	if found == nil {
		// Tidak cocok → tampilkan ulang pilihan
		h.sendRemoveDomainSelectKeyboard(msg.Chat.ID)
		return
	}

	deleteRemoveDomainSelectAwait(userID)
	setRemoveDomainAwait(userID, found.Name)

	text := fmt.Sprintf(
		"🗑️ *Hapus Domain*\n\n"+
			"Domain: *%s*\n\n"+
			"⚠️ Aksi ini hanya menghapus domain dari daftar bot.\n"+
			"Data di Cloudflare *tidak* akan terpengaruh.\n\n"+
			"Yakin mau hapus?",
		domainLabel(found.Name, found.Label),
	)
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Ya, Hapus"),
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	out := tgbotapi.NewMessage(msg.Chat.ID, text)
	out.ParseMode = "Markdown"
	out.ReplyMarkup = kb
	if _, err := h.api.Send(out); err != nil {
		log.Printf("send error: %v", err)
	}
}

// handleCallbackSetupDomainNew: daftarkan domain baru ke Cloudflare, buat redirect rule otomatis.
func (h *Handler) handleCallbackSetupDomainNew(cb *tgbotapi.CallbackQuery) {
	h.startNewDomainWizard(cb.Message.Chat.ID, cb.From.ID)
}

// startNewDomainWizard: mulai wizard tambah domain baru ke CF (AddZone + CreateRedirectRuleV2).
func (h *Handler) startNewDomainWizard(chatID int64, userID int64) {
	sess := &AddDomainSession{
		Step:        stepName,
		FromSetup:   true,
		IsNewDomain: true,
	}
	if len(h.cfg.CFAccounts) > 1 {
		sess.Step = stepCFAccount
		addDomainSessions.set(userID, sess)
		h.sendCFAccountPicker(chatID)
		return
	}
	if len(h.cfg.CFAccounts) == 1 {
		sess.CFAccountName = h.cfg.CFAccounts[0].Name
	}
	addDomainSessions.set(userID, sess)
	h.sendWizardMsg(chatID,
		"🌐 *Tambah Domain Baru — Step 1: Nama Domain*\n\n"+
			"Masukkan *nama domain* yang ingin kamu daftarkan ke Cloudflare.\n\n"+
			"📍 Domain ini belum perlu ada di Cloudflare — bot akan mendaftarkannya otomatis\n\n"+
			"Contoh:\n`domain.com`\n`tokobaju.net`\n`bisnismu.id`",
	)
}

// handleCallbackSetupDomainExisting: domain sudah ada di CF, langsung minta IDs.
func (h *Handler) handleCallbackSetupDomainExisting(cb *tgbotapi.CallbackQuery) {
	h.startAddDomainWizard(cb.Message.Chat.ID, cb.From.ID)
}

// handleCallbackSetupDomainStart: setelah baca instruksi, mulai isi IDs.
func (h *Handler) handleCallbackSetupDomainStart(cb *tgbotapi.CallbackQuery) {
	h.startAddDomainWizard(cb.Message.Chat.ID, cb.From.ID)
}

// startAddDomainWizard mulai flow pengisian domain (dari wizard, label wajib).
func (h *Handler) startAddDomainWizard(chatID int64, userID int64) {
	sess := &AddDomainSession{
		Step:      stepName,
		FromSetup: true,
	}
	if len(h.cfg.CFAccounts) > 1 {
		sess.Step = stepCFAccount
		addDomainSessions.set(userID, sess)
		h.sendCFAccountPicker(chatID)
		return
	}
	if len(h.cfg.CFAccounts) == 1 {
		sess.CFAccountName = h.cfg.CFAccounts[0].Name
	}
	addDomainSessions.set(userID, sess)
	h.sendWizardMsg(chatID,
		"➕ *Tambah Domain — Langkah 1/5*\n\n"+
			"Masukkan *nama domain* yang sudah ada di Cloudflare kamu.\n\n"+
			"📍 Pastikan domain sudah terdaftar di dashboard Cloudflare\n\n"+
			"Contoh:\n`domain.com`\n`subdomain.domain.com`",
	)
}

// sendCFAccountPicker: tampilkan pilihan akun CF saat ada >1 akun.
func (h *Handler) sendCFAccountPicker(chatID int64) {
	var rows [][]tgbotapi.KeyboardButton
	var sb strings.Builder
	sb.WriteString("☁️ *Pilih Akun Cloudflare:*\n\n")
	for i, acc := range h.cfg.CFAccounts {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("☁️ "+acc.Name),
		))
		sb.WriteString(fmt.Sprintf("%d. *%s* (%s)\n", i+1, acc.Name, acc.Email))
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

// ─────────────────────────────────────────
//  Add Domain Input Handler
// ─────────────────────────────────────────

func (h *Handler) handleAddDomainInput(msg *tgbotapi.Message, sess *AddDomainSession) {
	input := strings.TrimSpace(msg.Text)

	switch sess.Step {
	case stepCFAccount:
		// Strip "☁️ " prefix from button text
		name := strings.TrimPrefix(input, "☁️ ")
		if h.cfg.FindAccount(name) == nil {
			h.sendCFAccountPicker(msg.Chat.ID)
			return
		}
		sess.CFAccountName = name
		if sess.IsNewDomain {
			sess.Step = stepName
			addDomainSessions.set(msg.From.ID, sess)
			h.sendWizardMsg(msg.Chat.ID,
				"🌐 *Tambah Domain Baru — Step 1: Nama Domain*\n\n"+
					"Masukkan *nama domain* yang ingin kamu daftarkan ke Cloudflare.\n\n"+
					"📍 Domain ini belum perlu ada di Cloudflare — bot akan mendaftarkannya otomatis\n\n"+
					"Contoh:\n`domain.com`\n`tokobaju.net`\n`bisnismu.id`",
			)
		} else {
			sess.Step = stepName
			addDomainSessions.set(msg.From.ID, sess)
			h.sendWizardMsg(msg.Chat.ID,
				"➕ *Tambah Domain — Langkah 1/5*\n\n"+
					"Masukkan *nama domain* yang sudah ada di Cloudflare kamu.\n\n"+
					"📍 Pastikan domain sudah terdaftar di dashboard Cloudflare\n\n"+
					"Contoh:\n`domain.com`\n`subdomain.domain.com`",
			)
		}
		return

	case stepName:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Nama domain tidak boleh kosong.\n\nContoh: `domain.com`")
			return
		}
		sess.Domain.Name = strings.ToLower(input)
		sess.Step = stepLabel
		addDomainSessions.set(msg.From.ID, sess)

		totalSteps := "5"
		if sess.IsNewDomain {
			totalSteps = "3"
		}
		text := fmt.Sprintf(
			"✅ Domain: *%s*\n\n"+
				"🌐 *Langkah 2/%s — Label*\n\n"+
				"Masukkan *label* singkat untuk domain ini.\n"+
				"Label dipakai untuk mengelompokkan dan filter domain dengan `/list`.\n\n"+
				"Contoh:\n`MAIN` `PROMO` `STORE` `BLOG`",
			sess.Domain.Name, totalSteps,
		)
		// Label wajib saat setup wizard domain yg sudah ada di CF.
		// Untuk new domain atau manual add: label bisa dilewati.
		if sess.FromSetup && !sess.IsNewDomain {
			h.sendWizardMsg(msg.Chat.ID, text)
		} else {
			h.sendWizardMsgSkip(msg.Chat.ID, text)
		}

	case stepLabel:
		// handle skip label
		if input == "⏩ Skip Label" {
			// Label wajib hanya untuk domain existing saat setup wizard
			if sess.FromSetup && !sess.IsNewDomain {
				h.sendWizardMsg(msg.Chat.ID, "⚠️ Label wajib diisi saat setup.\n\nContoh: `MAIN` `PROMO` `STORE`")
				return
			}
			sess.Domain.Label = ""
			addDomainSessions.set(msg.From.ID, sess)
			if sess.IsNewDomain {
				sess.Step = stepNewDomainType
				addDomainSessions.set(msg.From.ID, sess)
				h.sendWizardMsg(msg.Chat.ID, "⏩ Label dilewati.")
				h.sendNewDomainTypeSelectMsg(msg.Chat.ID)
			} else {
				h.sendWizardMsg(msg.Chat.ID, "⏩ Label dilewati.\n\n🔍 *Mencari domain di Cloudflare...*\n\nMohon tunggu sebentar.")
				h.autoDiscoverDomain(msg.Chat.ID, msg.From.ID, sess)
			}
			return
		}
		if input == "" {
			if sess.FromSetup && !sess.IsNewDomain {
				h.sendWizardMsg(msg.Chat.ID, "⚠️ Label wajib diisi.\n\nContoh: `MAIN` `PROMO` `STORE`")
				return
			}
		}
		sess.Domain.Label = strings.ToUpper(input)
		addDomainSessions.set(msg.From.ID, sess)

		if sess.IsNewDomain {
			// Domain baru: lanjut pilih versi redirect
			sess.Step = stepNewDomainType
			addDomainSessions.set(msg.From.ID, sess)
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf("✅ Label: *%s*", sess.Domain.Label))
			h.sendNewDomainTypeSelectMsg(msg.Chat.ID)
		} else {
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
				"✅ Label: *%s*\n\n🔍 *Mencari domain di Cloudflare...*\n\nMohon tunggu sebentar.",
				sess.Domain.Label,
			))
			h.autoDiscoverDomain(msg.Chat.ID, msg.From.ID, sess)
		}

	case stepNewDomainType:
		switch input {
		case "V2 - Redirect Rules":
			sess.Domain.Type = "redirect_rules"
		case "V1 - Page Rules":
			sess.Domain.Type = "page_rules"
		default:
			h.sendNewDomainTypeSelectMsg(msg.Chat.ID)
			return
		}
		sess.Step = stepNewDomainURL
		addDomainSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID,
			"🌐 *Step 3: URL Redirect Tujuan*\n\n"+
				"Masukkan *URL tujuan* redirect domain ini.\n"+
				"(Bisa diganti kapanpun lewat tombol *🌐 Ganti Redirect*)\n\n"+
				"📍 Harus diawali `https://`\n\n"+
				"Contoh:\n`https://toko-utama.com`\n`https://landing.example.com/promo`",
		)

	case stepNewDomainURL:
		if !strings.HasPrefix(input, "https://") {
			h.sendWizardMsg(msg.Chat.ID,
				"⚠️ URL harus diawali dengan `https://`\n\n"+
					"Contoh:\n`https://toko-utama.com`\n`https://landing.example.com/promo`",
			)
			return
		}
		sess.TempRedirectURL = input
		addDomainSessions.set(msg.From.ID, sess)

		// Daftarkan domain ke Cloudflare
		h.sendWizardMsg(msg.Chat.ID, "⏳ *Mendaftarkan domain ke Cloudflare...*\n\nMohon tunggu sebentar.")
		zoneInfo, err := h.cfForAccountName(sess.CFAccountName).AddZone(sess.Domain.Name)
		if err != nil {
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
				"❌ *Gagal mendaftarkan domain.*\n\nError: _%s_\n\n"+
					"Pastikan domain belum pernah terdaftar di akun CF kamu, lalu coba lagi.",
				err.Error(),
			))
			return
		}
		sess.TempZoneID = zoneInfo.ZoneID
		sess.TempNameServers = zoneInfo.NameServers
		addDomainSessions.set(msg.From.ID, sess)

		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✅ *Domain berhasil didaftarkan ke Cloudflare!*\n🔑 Zone ID: `%s`\n\n"+
				"🔍 *Mengecek DNS record yang sudah ada...*",
			zoneInfo.ZoneID,
		))

		// Cek apakah sudah ada DNS record di zone ini
		existingDNS, _ := h.cfForAccountName(sess.CFAccountName).ListDNSRecords(zoneInfo.ZoneID)
		if len(existingDNS) > 0 {
			// Ada DNS existing → simpan ke session dan tanya user
			sess.ExistingDNSRecords = existingDNS
			sess.Step = stepNewDomainDNSCheck
			addDomainSessions.set(msg.From.ID, sess)
			h.showExistingDNSMsg(msg.Chat.ID, existingDNS)
		} else {
			// Tidak ada → langsung ke step tambah DNS
			sess.Step = stepNewDomainDNSType
			addDomainSessions.set(msg.From.ID, sess)
			h.sendWizardMsg(msg.Chat.ID, "Tidak ada DNS record. Sekarang tambahkan DNS record untuk domain ini. 👇")
			h.sendDNSTypeSelectMsg(msg.Chat.ID)
		}

	case stepNewDomainDNSCheck:
		switch input {
		case "✅ Pakai DNS yang Ada":
			// Skip DNS creation, langsung ke step "selesai atau tambah lagi"
			sess.Step = stepNewDomainDNSMoreOrGo
			addDomainSessions.set(msg.From.ID, sess)
			h.sendDNSMoreOrNextMsg(msg.Chat.ID, "", "", "", sess.ExistingDNSRecords)
		case "➕ Tambah DNS Baru":
			sess.Step = stepNewDomainDNSType
			addDomainSessions.set(msg.From.ID, sess)
			h.sendDNSTypeSelectMsg(msg.Chat.ID)
		case "🗑️ Hapus DNS":
			// Pindah ke step delete selection
			if sess.DNSDeleteSelected == nil {
				sess.DNSDeleteSelected = make(map[int]bool)
			}
			sess.Step = stepNewDomainDNSDelete
			addDomainSessions.set(msg.From.ID, sess)
			h.sendDNSDeleteSelectMsg(msg.Chat.ID, sess.ExistingDNSRecords, sess.DNSDeleteSelected)
		case "✏️ Edit DNS":
			// Pindah ke step pilih record yang mau diedit
			sess.Step = stepNewDomainDNSEditSelect
			addDomainSessions.set(msg.From.ID, sess)
			h.sendDNSEditSelectMsg(msg.Chat.ID, sess.ExistingDNSRecords)
		default:
			// Resend pilihan dengan data terbaru dari CF
			existingDNS, _ := h.cfForAccountName(sess.CFAccountName).ListDNSRecords(sess.TempZoneID)
			sess.ExistingDNSRecords = existingDNS
			addDomainSessions.set(msg.From.ID, sess)
			h.showExistingDNSMsg(msg.Chat.ID, existingDNS)
		}

	case stepNewDomainDNSType:
		switch input {
		case "A", "AAAA", "CNAME":
			sess.TempDNSType = input
		default:
			h.sendDNSTypeSelectMsg(msg.Chat.ID)
			return
		}
		sess.Step = stepNewDomainDNSName
		addDomainSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID,
			fmt.Sprintf("✅ Tipe: *%s*\n\n", input)+
				"🌐 *Step 5: Nama DNS Record*\n\n"+
				"Masukkan *nama* untuk DNS record ini.\n\n"+
				"📍 Gunakan `@` untuk root domain, atau subdomain seperti `www`\n\n"+
				"Contoh:\n`@` (untuk domain.com)\n`www` (untuk www.domain.com)",
		)

	case stepNewDomainDNSName:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Nama record tidak boleh kosong.\n\nContoh: `@` atau `www`")
			return
		}
		sess.TempDNSName = input
		sess.Step = stepNewDomainDNSVal
		addDomainSessions.set(msg.From.ID, sess)
		valueLabel := "IPv4 Address"
		valueExample := "`1.2.3.4`"
		if sess.TempDNSType == "AAAA" {
			valueLabel = "IPv6 Address"
			valueExample = "`2001:db8::1`"
		} else if sess.TempDNSType == "CNAME" {
			valueLabel = "Target Hostname"
			valueExample = "`target.example.com`"
		}
		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✅ Nama: *%s*\n\n"+
				"🌐 *Step 6: %s*\n\n"+
				"Masukkan *%s* untuk record ini.\n\n"+
				"Contoh:\n%s",
			input, valueLabel, strings.ToLower(valueLabel), valueExample,
		))

	case stepNewDomainDNSVal:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Value tidak boleh kosong.")
			return
		}
		sess.TempDNSValue = input
		sess.Step = stepNewDomainDNSPrx
		addDomainSessions.set(msg.From.ID, sess)
		h.sendDNSProxySelectMsg(msg.Chat.ID)

	case stepNewDomainDNSPrx:
		switch input {
		case "✅ Proxy ON":
			sess.TempDNSProxy = true
		case "❌ Proxy OFF":
			sess.TempDNSProxy = false
		default:
			h.sendDNSProxySelectMsg(msg.Chat.ID)
			return
		}

		// Buat DNS record sekarang
		h.sendWizardMsg(msg.Chat.ID, "⏳ *Membuat DNS record...*\n\nMohon tunggu sebentar.")
		if err := h.cfForAccountName(sess.CFAccountName).CreateDNSRecord(sess.TempZoneID, sess.TempDNSType, sess.TempDNSName, sess.TempDNSValue, sess.TempDNSProxy); err != nil {
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
				"❌ *Gagal membuat DNS record.*\n\nError: _%s_\n\nCoba lagi atau ketik ❌ Cancel.",
				err.Error(),
			))
			return
		}

		// DNS berhasil → simpan ke ExistingDNSRecords + tanya tambah lagi atau lanjut
		sess.ExistingDNSRecords = append(sess.ExistingDNSRecords, cloudflare.DNSRecord{
			Type:    sess.TempDNSType,
			Name:    sess.TempDNSName,
			Content: sess.TempDNSValue,
			Proxied: sess.TempDNSProxy,
		})
		sess.Step = stepNewDomainDNSMoreOrGo
		addDomainSessions.set(msg.From.ID, sess)
		h.sendDNSMoreOrNextMsg(msg.Chat.ID, sess.TempDNSType, sess.TempDNSName, sess.TempDNSValue, sess.ExistingDNSRecords)

	case stepNewDomainDNSMoreOrGo:
		switch input {
		case "➕ Tambah DNS Lagi":
			// Reset temp DNS, balik ke pilih tipe
			sess.TempDNSType = ""
			sess.TempDNSName = ""
			sess.TempDNSValue = ""
			sess.TempDNSProxy = false
			sess.Step = stepNewDomainDNSType
			addDomainSessions.set(msg.From.ID, sess)
			h.sendDNSTypeSelectMsg(msg.Chat.ID)
		case "✅ Selesai DNS":
			h.finalizeNewDomain(msg.Chat.ID, msg.From.ID, sess)
		default:
			h.sendDNSMoreOrNextMsg(msg.Chat.ID, sess.TempDNSType, sess.TempDNSName, sess.TempDNSValue, sess.ExistingDNSRecords)
		}

	case stepNewDomainDNSDelete:
		switch input {
		case "🗑️ Hapus yang Dipilih":
			// Hitung yang dipilih
			selectedCount := 0
			for _, v := range sess.DNSDeleteSelected {
				if v {
					selectedCount++
				}
			}
			if selectedCount == 0 {
				h.sendWizardMsg(msg.Chat.ID, "⚠️ Pilih minimal 1 DNS record dulu.\n\n_(Tap nama record untuk centang/uncentang)_")
				h.sendDNSDeleteSelectMsg(msg.Chat.ID, sess.ExistingDNSRecords, sess.DNSDeleteSelected)
				return
			}
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf("⏳ Menghapus %d DNS record...", selectedCount))
			errCount := 0
			for idx, isSelected := range sess.DNSDeleteSelected {
				if !isSelected || idx >= len(sess.ExistingDNSRecords) {
					continue
				}
				if err := h.cfForAccountName(sess.CFAccountName).DeleteDNSRecord(sess.TempZoneID, sess.ExistingDNSRecords[idx].ID); err != nil {
					log.Printf("DeleteDNSRecord error: %v", err)
					errCount++
				}
			}
			if errCount > 0 {
				h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf("⚠️ %d record gagal dihapus.", errCount))
			}
			// Re-fetch DNS
			updated, _ := h.cfForAccountName(sess.CFAccountName).ListDNSRecords(sess.TempZoneID)
			sess.ExistingDNSRecords = updated
			sess.DNSDeleteSelected = nil
			if len(updated) == 0 {
				sess.Step = stepNewDomainDNSMoreOrGo
				addDomainSessions.set(msg.From.ID, sess)
				h.sendWizardMsg(msg.Chat.ID, "✅ Semua DNS record dihapus.")
				h.sendDNSMoreOrNextMsg(msg.Chat.ID, "", "", "", nil)
			} else {
				sess.Step = stepNewDomainDNSCheck
				addDomainSessions.set(msg.From.ID, sess)
				h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf("✅ %d record berhasil dihapus.", selectedCount-errCount))
				h.showExistingDNSMsg(msg.Chat.ID, updated)
			}
		case "❌ Batal":
			sess.DNSDeleteSelected = nil
			sess.Step = stepNewDomainDNSCheck
			addDomainSessions.set(msg.From.ID, sess)
			h.showExistingDNSMsg(msg.Chat.ID, sess.ExistingDNSRecords)
		default:
			// User mengetuk salah satu record button → toggle checkbox
			idx := parseDNSButtonIdx(input)
			if idx >= 0 && idx < len(sess.ExistingDNSRecords) {
				if sess.DNSDeleteSelected == nil {
					sess.DNSDeleteSelected = make(map[int]bool)
				}
				sess.DNSDeleteSelected[idx] = !sess.DNSDeleteSelected[idx]
				addDomainSessions.set(msg.From.ID, sess)
			}
			h.sendDNSDeleteSelectMsg(msg.Chat.ID, sess.ExistingDNSRecords, sess.DNSDeleteSelected)
		}

	case stepNewDomainDNSEditSelect:
		switch input {
		case "❌ Batal":
			sess.Step = stepNewDomainDNSCheck
			addDomainSessions.set(msg.From.ID, sess)
			h.showExistingDNSMsg(msg.Chat.ID, sess.ExistingDNSRecords)
		default:
			idx := parseDNSButtonIdx(input)
			if idx < 0 || idx >= len(sess.ExistingDNSRecords) {
				h.sendDNSEditSelectMsg(msg.Chat.ID, sess.ExistingDNSRecords)
				return
			}
			record := sess.ExistingDNSRecords[idx]
			sess.DNSEditIdx = idx
			sess.TempDNSType = record.Type
			sess.TempDNSName = record.Name
			sess.TempDNSValue = record.Content
			sess.TempDNSProxy = record.Proxied
			sess.Step = stepNewDomainDNSEditType
			addDomainSessions.set(msg.From.ID, sess)
			proxyStr := "⚪ DNS Only"
			if record.Proxied {
				proxyStr = "🟠 Proxied"
			}
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
				"✏️ *Edit DNS Record #%d*\n\n"+
					"*Nilai saat ini:*\n"+
					"📌 Tipe: *%s*\n"+
					"📍 Nama: `%s`\n"+
					"🌐 Value: `%s`\n"+
					"🔗 Proxy: %s\n\n"+
					"Pilih tipe DNS baru 👇",
				idx+1, record.Type, record.Name, record.Content, proxyStr,
			))
			h.sendDNSTypeSelectMsg(msg.Chat.ID)
		}

	case stepNewDomainDNSEditType:
		switch input {
		case "A", "AAAA", "CNAME":
			sess.TempDNSType = input
		default:
			h.sendDNSTypeSelectMsg(msg.Chat.ID)
			return
		}
		sess.Step = stepNewDomainDNSEditName
		addDomainSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✅ Tipe: *%s*\n\n"+
				"✏️ *Edit DNS — Nama Record*\n\n"+
				"Masukkan nama baru (sebelumnya: `%s`):\n\n"+
				"📍 Gunakan `@` untuk root domain\n\n"+
				"Contoh:\n`@`\n`www`",
			input, sess.TempDNSName,
		))

	case stepNewDomainDNSEditName:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Nama record tidak boleh kosong.\n\nContoh: `@` atau `www`")
			return
		}
		sess.TempDNSName = input
		sess.Step = stepNewDomainDNSEditVal
		addDomainSessions.set(msg.From.ID, sess)
		valueLabel := "IPv4 Address"
		valueExample := "`1.2.3.4`"
		if sess.TempDNSType == "AAAA" {
			valueLabel = "IPv6 Address"
			valueExample = "`2001:db8::1`"
		} else if sess.TempDNSType == "CNAME" {
			valueLabel = "Target Hostname"
			valueExample = "`target.example.com`"
		}
		h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
			"✅ Nama: *%s*\n\n"+
				"✏️ *Edit DNS — %s*\n\n"+
				"Masukkan %s baru (sebelumnya: `%s`):\n\n"+
				"Contoh:\n%s",
			input, valueLabel, strings.ToLower(valueLabel), sess.TempDNSValue, valueExample,
		))

	case stepNewDomainDNSEditVal:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Value tidak boleh kosong.")
			return
		}
		sess.TempDNSValue = input
		sess.Step = stepNewDomainDNSEditProxy
		addDomainSessions.set(msg.From.ID, sess)
		h.sendDNSProxySelectMsg(msg.Chat.ID)

	case stepNewDomainDNSEditProxy:
		switch input {
		case "✅ Proxy ON":
			sess.TempDNSProxy = true
		case "❌ Proxy OFF":
			sess.TempDNSProxy = false
		default:
			h.sendDNSProxySelectMsg(msg.Chat.ID)
			return
		}

		// Update DNS record di Cloudflare
		h.sendWizardMsg(msg.Chat.ID, "⏳ *Mengupdate DNS record...*\n\nMohon tunggu sebentar.")
		if sess.DNSEditIdx >= len(sess.ExistingDNSRecords) {
			h.sendWizardMsg(msg.Chat.ID, "❌ Record tidak ditemukan. Coba lagi.")
			return
		}
		record := sess.ExistingDNSRecords[sess.DNSEditIdx]
		if err := h.cfForAccountName(sess.CFAccountName).UpdateDNSRecord(sess.TempZoneID, record.ID, sess.TempDNSType, sess.TempDNSName, sess.TempDNSValue, sess.TempDNSProxy); err != nil {
			log.Printf("UpdateDNSRecord %s error: %v", record.ID, err)
			h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf(
				"❌ *Gagal mengupdate DNS record.*\n\nError: _%s_\n\nCoba lagi atau ketik ❌ Cancel.",
				err.Error(),
			))
			return
		}

		// Re-fetch dan kembali ke menu DNS check
		updated, _ := h.cfForAccountName(sess.CFAccountName).ListDNSRecords(sess.TempZoneID)
		sess.ExistingDNSRecords = updated
		sess.DNSEditIdx = 0
		sess.TempDNSType = ""
		sess.TempDNSName = ""
		sess.TempDNSValue = ""
		sess.TempDNSProxy = false
		sess.Step = stepNewDomainDNSCheck
		addDomainSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID, "✅ *DNS record berhasil diupdate!*")
		h.showExistingDNSMsg(msg.Chat.ID, updated)

	case stepZoneID:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Zone ID tidak boleh kosong.\n\n"+zoneIDPrompt)
			return
		}
		sess.Domain.ZoneID = input
		sess.Step = stepType
		addDomainSessions.set(msg.From.ID, sess)
		h.sendTypeSelectMsg(msg.Chat.ID)

	case stepType:
		switch input {
		case "V2 - Redirect Rules":
			h.applyDomainType(msg.Chat.ID, msg.From.ID, "redirect_rules", sess)
		case "V1 - Page Rules":
			h.applyDomainType(msg.Chat.ID, msg.From.ID, "page_rules", sess)
		default:
			h.sendTypeSelectMsg(msg.Chat.ID)
		}

	case stepPickRule:
		n, err := strconv.Atoi(input)
		if err != nil {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Ketik *angka* sesuai nomor rule di atas.\n\nContoh: `1`")
			return
		}
		if sess.Domain.Type == "redirect_rules" {
			if n < 1 || n > len(sess.DiscoveredRules) {
				h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf("⚠️ Pilih angka antara 1 sampai %d.", len(sess.DiscoveredRules)))
				return
			}
			picked := sess.DiscoveredRules[n-1]
			sess.Domain.RulesetID = picked.RulesetID
			sess.Domain.RuleID = picked.RuleID
		} else {
			if n < 1 || n > len(sess.DiscoveredPageRules) {
				h.sendWizardMsg(msg.Chat.ID, fmt.Sprintf("⚠️ Pilih angka antara 1 sampai %d.", len(sess.DiscoveredPageRules)))
				return
			}
			picked := sess.DiscoveredPageRules[n-1]
			sess.Domain.RuleID = picked.RuleID
		}
		sess.Step = stepConfirm
		addDomainSessions.set(msg.From.ID, sess)
		h.showAddDomainConfirm(msg.Chat.ID, sess)

	case stepRulesetID:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Ruleset ID tidak boleh kosong.\n\n"+rulesetIDPrompt)
			return
		}
		sess.Domain.RulesetID = input
		sess.Step = stepRuleID
		addDomainSessions.set(msg.From.ID, sess)
		h.sendWizardMsg(msg.Chat.ID,
			fmt.Sprintf("✅ Ruleset ID tersimpan.\n\n"+ruleIDPromptRedirect),
		)

	case stepRuleID:
		if input == "" {
			h.sendWizardMsg(msg.Chat.ID, "⚠️ Rule ID tidak boleh kosong.\n\n"+ruleIDPromptRedirect)
			return
		}
		sess.Domain.RuleID = input
		sess.Step = stepConfirm
		addDomainSessions.set(msg.From.ID, sess)
		h.showAddDomainConfirm(msg.Chat.ID, sess)

	case stepConfirm:
		if input == "✅ Simpan Domain" {
			h.executeSaveDomain(msg.Chat.ID, msg.From.ID, sess)
		} else {
			h.showAddDomainConfirm(msg.Chat.ID, sess)
		}
	}
}

// autoDiscoverDomain: auto-fetch Zone ID → deteksi tipe → fetch rules, semua otomatis.
func (h *Handler) autoDiscoverDomain(chatID int64, userID int64, sess *AddDomainSession) {
	// 1. Auto-fetch Zone ID
	zoneID, err := h.cfForAccountName(sess.CFAccountName).GetZoneID(sess.Domain.Name)
	if err != nil {
		h.sendWizardMsg(chatID,
			"⚠️ Domain tidak ditemukan di akun Cloudflare kamu.\n\n"+
				"Pastikan domain sudah ditambahkan ke Cloudflare dan credentials CF sudah benar.\n\n"+
				"Masukkan ulang nama domain atau ketik ❌ Cancel untuk batal.",
		)
		sess.Step = stepName
		addDomainSessions.set(userID, sess)
		return
	}
	sess.Domain.ZoneID = zoneID

	// 2. Coba fetch Redirect Rules (v2) dulu
	redirectRules, _ := h.cfForAccountName(sess.CFAccountName).ListRedirectRules(zoneID)

	// 3. Coba fetch Page Rules (v1)
	pageRules, _ := h.cfForAccountName(sess.CFAccountName).ListPageRules(zoneID)

	hasV2 := len(redirectRules) > 0
	hasV1 := len(pageRules) > 0

	switch {
	case hasV2 && !hasV1:
		// Hanya ada Redirect Rules → langsung pakai
		sess.Domain.Type = "redirect_rules"
		addDomainSessions.set(userID, sess)
		h.sendWizardMsg(chatID, fmt.Sprintf(
			"✅ Domain ditemukan!\n🔑 Zone ID: `%s`\n\n📋 Terdeteksi: *Redirect Rules (v2)*\n\n🔍 Mengambil daftar rule...",
			zoneID,
		))
		h.applyDiscoveredRedirectRules(chatID, userID, sess, redirectRules)

	case hasV1 && !hasV2:
		// Hanya ada Page Rules → langsung pakai
		sess.Domain.Type = "page_rules"
		addDomainSessions.set(userID, sess)
		h.sendWizardMsg(chatID, fmt.Sprintf(
			"✅ Domain ditemukan!\n🔑 Zone ID: `%s`\n\n📄 Terdeteksi: *Page Rules (v1)*\n\n🔍 Mengambil daftar rule...",
			zoneID,
		))
		h.applyDiscoveredPageRules(chatID, userID, sess, pageRules)

	case hasV2 && hasV1:
		// Dua-duanya ada → tanya user
		sess.DiscoveredRules = redirectRules
		sess.DiscoveredPageRules = pageRules
		sess.Step = stepType
		addDomainSessions.set(userID, sess)
		h.sendWizardMsg(chatID, fmt.Sprintf(
			"✅ Domain ditemukan!\n🔑 Zone ID: `%s`\n\n⚠️ Terdeteksi *dua tipe rule*. Pilih yang kamu gunakan 👇",
			zoneID,
		))
		h.sendTypeSelectMsg(chatID)

	default:
		// Tidak ada rule sama sekali
		h.sendWizardMsg(chatID,
			"⚠️ Domain ditemukan di Cloudflare, tapi *belum ada redirect rule* yang aktif.\n\n"+
				"Buat redirect rule dulu di Cloudflare, lalu coba tambah domain lagi.\n\n"+
				"Ketik ❌ Cancel untuk batal.",
		)
		addDomainSessions.delete(userID)
	}
}

// applyDiscoveredRedirectRules: handle setelah rules v2 berhasil difetch.
func (h *Handler) applyDiscoveredRedirectRules(chatID int64, userID int64, sess *AddDomainSession, rules []cloudflare.DiscoveredRule) {
	if len(rules) == 1 {
		sess.Domain.RulesetID = rules[0].RulesetID
		sess.Domain.RuleID = rules[0].RuleID
		sess.Step = stepConfirm
		addDomainSessions.set(userID, sess)
		h.showAddDomainConfirm(chatID, sess)
		return
	}
	sess.DiscoveredRules = rules
	sess.Step = stepPickRule
	addDomainSessions.set(userID, sess)
	h.showRulePickerRedirect(chatID, rules)
}

// applyDiscoveredPageRules: handle setelah page rules berhasil difetch.
func (h *Handler) applyDiscoveredPageRules(chatID int64, userID int64, sess *AddDomainSession, rules []cloudflare.DiscoveredPageRule) {
	if len(rules) == 1 {
		sess.Domain.RuleID = rules[0].RuleID
		sess.Step = stepConfirm
		addDomainSessions.set(userID, sess)
		h.showAddDomainConfirm(chatID, sess)
		return
	}
	sess.DiscoveredPageRules = rules
	sess.Step = stepPickRule
	addDomainSessions.set(userID, sess)
	h.showRulePickerPageRules(chatID, rules)
}

// applyDomainType: dipanggil saat user pilih tipe (hanya kalau ada 2 tipe sekaligus).
// Rules sudah ada di session dari autoDiscoverDomain sebelumnya.
func (h *Handler) applyDomainType(chatID int64, userID int64, domainType string, sess *AddDomainSession) {
	sess.Domain.Type = domainType
	addDomainSessions.set(userID, sess)

	if domainType == "redirect_rules" {
		h.applyDiscoveredRedirectRules(chatID, userID, sess, sess.DiscoveredRules)
	} else {
		h.applyDiscoveredPageRules(chatID, userID, sess, sess.DiscoveredPageRules)
	}
}

func (h *Handler) showRulePickerRedirect(chatID int64, rules []cloudflare.DiscoveredRule) {
	text := "✅ *Ditemukan beberapa redirect rule:*\n\nKetik *nomor* rule yang ingin kamu kelola:\n\n"
	for i, r := range rules {
		url := r.TargetURL
		if url == "" {
			url = "(tidak ada target URL)"
		}
		text += fmt.Sprintf("%d. Target: `%s`\n", i+1, url)
	}
	h.sendWizardMsg(chatID, text)
}

func (h *Handler) showRulePickerPageRules(chatID int64, rules []cloudflare.DiscoveredPageRule) {
	text := "✅ *Ditemukan beberapa page rule:*\n\nKetik *nomor* rule yang ingin kamu kelola:\n\n"
	for i, r := range rules {
		text += fmt.Sprintf("%d. Pattern: `%s`\n   → `%s`\n\n", i+1, r.Pattern, r.TargetURL)
	}
	h.sendWizardMsg(chatID, text)
}

// executeSaveDomain: simpan domain ke config. Dipanggil dari stepConfirm atau callback.
func (h *Handler) executeSaveDomain(chatID int64, userID int64, sess *AddDomainSession) {
	sess.Domain.CFAccount = sess.CFAccountName
	h.cfg.Domains = append(h.cfg.Domains, sess.Domain)
	if err := h.cfg.Save(h.configPath); err != nil {
		log.Printf("save config error: %v", err)
		h.cfg.Domains = h.cfg.Domains[:len(h.cfg.Domains)-1]
		h.sendWizardMsg(chatID, "❌ Gagal menyimpan config. Coba lagi atau ketik /cancel.")
		return
	}

	fromSetup := sess.FromSetup
	labelInfo := sess.Domain.Label
	if labelInfo == "" {
		labelInfo = "(tidak ada)"
	}
	successText := fmt.Sprintf("✅ *Domain berhasil ditambahkan!*\n\n🌐 %s\n🏷️ Label: %s",
		domainLabel(sess.Domain.Name, sess.Domain.Label), labelInfo)

	addDomainSessions.delete(userID)

	if fromSetup {
		kb := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("➕ Tambah Lagi"),
				tgbotapi.NewKeyboardButton("✅ Selesai Setup"),
			),
		)
		kb.ResizeKeyboard = true
		out := tgbotapi.NewMessage(chatID, successText+"\n\nMau tambah domain lagi?")
		out.ParseMode = "Markdown"
		out.ReplyMarkup = kb
		if _, err := h.api.Send(out); err != nil {
			log.Printf("send error: %v", err)
		}
	} else {
		h.sendWithReplyKeyboard(chatID, userID,
			successText+"\n\nGunakan /adddomain untuk tambah domain lagi.")
	}
}

// finalizeNewDomain: buat redirect rule, simpan config, tampilkan nameserver + tombol check status.
// DNS record sudah dibuat sebelumnya di stepNewDomainDNSPrx.
func (h *Handler) finalizeNewDomain(chatID int64, userID int64, sess *AddDomainSession) {
	h.sendWizardMsg(chatID, "⏳ *Membuat redirect rule...*\n\nMohon tunggu sebentar.")

	zoneID := sess.TempZoneID

	// Buat redirect rule (V1 atau V2)
	var rulesetID, ruleID string
	if sess.Domain.Type == "page_rules" {
		pattern := fmt.Sprintf("*%s/*", sess.Domain.Name)
		id, err := h.cfForAccountName(sess.CFAccountName).CreatePageRule(zoneID, pattern, sess.TempRedirectURL)
		if err != nil {
			log.Printf("CreatePageRule error for %s: %v", sess.Domain.Name, err)
			h.sendWizardMsg(chatID, fmt.Sprintf(
				"❌ *Gagal membuat Page Rule.*\n\nError: _%s_\n\nCoba lagi atau ketik ❌ Cancel.",
				err.Error(),
			))
			return
		}
		ruleID = id
	} else {
		rsID, rID, err := h.cfForAccountName(sess.CFAccountName).CreateRedirectRuleV2(zoneID, sess.TempRedirectURL)
		if err != nil {
			log.Printf("CreateRedirectRuleV2 error for %s: %v", sess.Domain.Name, err)
			h.sendWizardMsg(chatID, fmt.Sprintf(
				"❌ *Gagal membuat Redirect Rule.*\n\nError: _%s_\n\nCoba lagi atau ketik ❌ Cancel.",
				err.Error(),
			))
			return
		}
		rulesetID, ruleID = rsID, rID
	}

	// Simpan domain ke config
	sess.Domain.ZoneID = zoneID
	sess.Domain.RulesetID = rulesetID
	sess.Domain.RuleID = ruleID
	sess.Domain.CFAccount = sess.CFAccountName

	h.cfg.Domains = append(h.cfg.Domains, sess.Domain)
	if err := h.cfg.Save(h.configPath); err != nil {
		log.Printf("save config error: %v", err)
		h.cfg.Domains = h.cfg.Domains[:len(h.cfg.Domains)-1]
		h.sendWizardMsg(chatID, "❌ Gagal menyimpan config. Coba lagi atau ketik ❌ Cancel.")
		return
	}

	// Session selesai
	addDomainSessions.delete(userID)
	// Simpan zone ID untuk tombol "🔍 Check Status"
	setPendingNSCheck(userID, zoneID)

	// Bangun teks nameserver
	nsText := ""
	for i, ns := range sess.TempNameServers {
		nsText += fmt.Sprintf("%d. `%s`\n", i+1, ns)
	}
	if nsText == "" {
		nsText = "_(cek di Cloudflare Dashboard → Overview domain)_\n"
	}

	labelInfo := sess.Domain.Label
	if labelInfo == "" {
		labelInfo = "(tidak ada)"
	}

	nsMsg := fmt.Sprintf(
		"✅ *Semua berhasil disetup!*\n\n"+
			"🌐 Domain: *%s*\n"+
			"🏷️ Label: %s\n"+
			"🎯 Redirect ke: `%s`\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"📡 *Nameserver Cloudflare*\n\n"+
			"Login ke registrar domain kamu dan ganti nameserver ke:\n\n%s\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"⏳ *Menunggu propagasi DNS...*\n\n"+
			"Setelah nameserver diubah, tekan *🔍 Check Status* di bawah untuk cek apakah domain sudah aktif.\n"+
			"_(Propagasi DNS biasanya 1–48 jam)_",
		domainLabel(sess.Domain.Name, sess.Domain.Label), labelInfo, sess.TempRedirectURL, nsText,
	)

	h.showCheckStatusKeyboard(chatID, nsMsg)
}

// showCheckStatusKeyboard: tampilkan pesan dengan reply keyboard [🔍 Check Status].
func (h *Handler) showCheckStatusKeyboard(chatID int64, text string) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔍 Check Status"),
		),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// handleCheckNSStatus: cek status zone CF via reply keyboard button.
func (h *Handler) handleCheckNSStatus(chatID, userID int64) {
	zoneID, ok := getPendingNSCheck(userID)
	if !ok {
		h.send(chatID, "⚠️ Tidak ada domain yang sedang menunggu aktivasi.")
		return
	}

	cfAccountName := ""
	if addSess, ok := addDomainSessions.get(userID); ok {
		cfAccountName = addSess.CFAccountName
	}
	status, err := h.cfForAccountName(cfAccountName).GetZoneStatus(zoneID)
	if err != nil {
		h.send(chatID, "❌ Gagal cek status. Coba lagi nanti.")
		return
	}

	if status == "active" {
		deletePendingNSCheck(userID)
		h.sendWithReplyKeyboard(chatID, userID,
			"🎉 *Domain sudah aktif!*\n\n"+
				"Nameserver sudah terpropagasi. Redirect berjalan normal! 🚀\n\n"+
				"Gunakan tombol di bawah untuk mengelola redirect.",
		)
	} else {
		h.showCheckStatusKeyboard(chatID, fmt.Sprintf(
			"⏳ *Status: %s*\n\n"+
				"Nameserver belum terpropagasi. Pastikan sudah diubah di registrar.\n\n"+
				"Coba lagi beberapa saat.\n"+
				"_(Propagasi DNS biasanya 1–48 jam)_",
			status,
		))
	}
}

// handleCallbackCheckNSStatus: fallback untuk inline button lama (redirect ke reply keyboard handler).
func (h *Handler) handleCallbackCheckNSStatus(cb *tgbotapi.CallbackQuery, zoneID string) {
	setPendingNSCheck(cb.From.ID, zoneID)
	h.handleCheckNSStatus(cb.Message.Chat.ID, cb.From.ID)
}

func (h *Handler) showAddDomainConfirm(chatID int64, sess *AddDomainSession) {
	d := sess.Domain
	typeLabel := "Redirect Rules (v2)"
	if d.Type == "page_rules" {
		typeLabel = "Page Rules (v1)"
	}
	labelText := d.Label
	if labelText == "" {
		labelText = "(tidak ada)"
	}
	rulesetText := d.RulesetID
	if rulesetText == "" {
		rulesetText = "—"
	}

	text := fmt.Sprintf(
		"📋 *Konfirmasi Domain*\n\n"+
			"🌐 Domain: *%s*\n"+
			"🏷️ Label: *%s*\n"+
			"🔑 Zone ID: `%s`\n"+
			"📌 Tipe: %s\n"+
			"📦 Ruleset ID: `%s`\n"+
			"🔖 Rule ID: `%s`\n\n"+
			"Klik *✅ Simpan Domain* untuk menyimpan 👇",
		d.Name, labelText, d.ZoneID, typeLabel, rulesetText, d.RuleID,
	)
	h.sendConfirmDomainMsg(chatID, text)
}

// cleanupNewDomainSession: hapus zone dari CF jika cancel saat new domain wizard sedang berjalan.
func (h *Handler) cleanupNewDomainSession(userID int64) {
	sess, ok := addDomainSessions.get(userID)
	if !ok {
		return
	}
	if sess.IsNewDomain && sess.TempZoneID != "" {
		if err := h.cfForAccountName(sess.CFAccountName).DeleteZone(sess.TempZoneID); err != nil {
			log.Printf("DeleteZone %s error (cancel cleanup): %v", sess.TempZoneID, err)
		} else {
			log.Printf("DeleteZone %s: berhasil dihapus saat cancel", sess.TempZoneID)
		}
	}
}

// ─────────────────────────────────────────
//  Add Domain Callbacks
// ─────────────────────────────────────────

func (h *Handler) handleCallbackAddDomainType(cb *tgbotapi.CallbackQuery, domainType string) {
	sess, ok := addDomainSessions.get(cb.From.ID)
	if !ok {
		h.sendWizardMsg(cb.Message.Chat.ID, "⏰ Sesi sudah habis. Mulai ulang dengan /adddomain")
		return
	}
	h.applyDomainType(cb.Message.Chat.ID, cb.From.ID, domainType, sess)
}

func (h *Handler) handleCallbackAddDomainSkipLabel(cb *tgbotapi.CallbackQuery) {
	sess, ok := addDomainSessions.get(cb.From.ID)
	if !ok {
		h.sendWizardMsg(cb.Message.Chat.ID, "⏰ Sesi sudah habis.")
		return
	}
	sess.Domain.Label = ""
	sess.Step = stepZoneID
	addDomainSessions.set(cb.From.ID, sess)
	h.sendWizardMsg(cb.Message.Chat.ID,
		"🔑 Masukkan *Zone ID* domain ini:\n📍 Ada di halaman Overview domain di Cloudflare.",
	)
}

func (h *Handler) handleCallbackAddDomainConfirm(cb *tgbotapi.CallbackQuery) {
	sess, ok := addDomainSessions.get(cb.From.ID)
	if !ok {
		h.send(cb.Message.Chat.ID, "⏰ Sesi sudah habis.")
		return
	}
	h.executeSaveDomain(cb.Message.Chat.ID, cb.From.ID, sess)
}

// ─────────────────────────────────────────
//  Setup Done / Add More
// ─────────────────────────────────────────

func (h *Handler) handleCallbackSetupDone(cb *tgbotapi.CallbackQuery) {
	h.sendWithReplyKeyboard(cb.Message.Chat.ID, cb.From.ID,
		"🎉 *Setup selesai! Bot siap digunakan.*\n\n"+
			"Gunakan tombol di bawah untuk mulai mengelola redirect domain kamu.\n\n"+
			"💡 Tips:\n"+
			"• `📋 List URL` — lihat semua URL redirect saat ini\n"+
			"• `🌐 Ganti Redirect` — ganti 1 domain\n"+
			"• `🔀 Bulk Redirect` — ganti banyak domain sekaligus\n"+
			"• `/list namalabel` — filter domain berdasarkan label",
	)
}

func (h *Handler) handleCallbackSetupAddMore(cb *tgbotapi.CallbackQuery) {
	h.showDomainChoiceButtons(cb.Message.Chat.ID)
}

// ─────────────────────────────────────────
//  /adddomain command (manual, bukan wizard)
// ─────────────────────────────────────────

func (h *Handler) handleAddDomainCommand(msg *tgbotapi.Message) {
	sess := &AddDomainSession{Step: stepName, FromSetup: false}
	if len(h.cfg.CFAccounts) > 1 {
		sess.Step = stepCFAccount
		addDomainSessions.set(msg.From.ID, sess)
		h.sendCFAccountPicker(msg.Chat.ID)
		return
	}
	if len(h.cfg.CFAccounts) == 1 {
		sess.CFAccountName = h.cfg.CFAccounts[0].Name
	}
	addDomainSessions.set(msg.From.ID, sess)
	h.sendWizardMsg(msg.Chat.ID,
		"➕ *Tambah Domain Baru — Langkah 1/5*\n\n"+
			"Masukkan *nama domain* yang sudah ada di Cloudflare kamu.\n\n"+
			"📍 Pastikan domain sudah terdaftar di dashboard Cloudflare\n\n"+
			"Contoh:\n`domain.com`\n`subdomain.domain.com`",
	)
}

// ─────────────────────────────────────────
//  /removedomain command
// ─────────────────────────────────────────

func (h *Handler) handleRemoveDomainCommand(msg *tgbotapi.Message) {
	name := strings.TrimSpace(msg.CommandArguments())
	if name == "" {
		h.send(msg.Chat.ID, "⚠️ Format: /removedomain <nama_domain>\n\nContoh: /removedomain domain.com")
		return
	}

	// Cari domain
	var found *config.Domain
	for i := range h.cfg.Domains {
		if strings.EqualFold(h.cfg.Domains[i].Name, name) {
			found = &h.cfg.Domains[i]
			break
		}
	}
	if found == nil {
		h.sendMd(msg.Chat.ID, fmt.Sprintf(
			"❌ Domain `%s` tidak ditemukan.\n\nGunakan `📋 List URL` untuk lihat domain yang ada.",
			name,
		))
		return
	}

	// Simpan nama domain dan minta konfirmasi
	setRemoveDomainAwait(msg.From.ID, found.Name)

	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Ya, Hapus"),
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	out := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
		"⚠️ *Konfirmasi Hapus Domain*\n\n"+
			"Domain: *%s*\n\n"+
			"Yakin mau menghapusnya dari konfigurasi bot?\n\n"+
			"_ℹ️ Redirect di Cloudflare tidak akan ikut terhapus, hanya konfigurasi di bot._",
		domainLabel(found.Name, found.Label),
	))
	out.ParseMode = "Markdown"
	out.ReplyMarkup = kb
	if _, err := h.api.Send(out); err != nil {
		log.Printf("send error: %v", err)
	}
}

// handleConfirmRemoveDomain: eksekusi hapus domain setelah user konfirmasi.
func (h *Handler) handleConfirmRemoveDomain(msg *tgbotapi.Message) {
	userID := msg.From.ID
	name, ok := getRemoveDomainAwait(userID)
	if !ok {
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "⏰ Sesi sudah habis. Coba lagi dari ⚙️ *Kelola Domain* → 🗑️ Hapus Domain.")
		return
	}
	deleteRemoveDomainAwait(userID)

	newList := make([]config.Domain, 0, len(h.cfg.Domains))
	var removed *config.Domain
	for i, d := range h.cfg.Domains {
		if strings.EqualFold(d.Name, name) {
			removed = &h.cfg.Domains[i]
			continue
		}
		newList = append(newList, d)
	}
	if removed == nil {
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, fmt.Sprintf("❌ Domain `%s` tidak ditemukan.", name))
		return
	}
	old := h.cfg.Domains
	h.cfg.Domains = newList
	if err := h.cfg.Save(h.configPath); err != nil {
		log.Printf("save config error: %v", err)
		h.cfg.Domains = old
		h.sendWithReplyKeyboard(msg.Chat.ID, userID, "❌ Gagal menyimpan config.")
		return
	}
	h.sendWithReplyKeyboard(msg.Chat.ID, userID, fmt.Sprintf(
		"✅ Domain *%s* berhasil dihapus dari konfigurasi bot.",
		domainLabel(removed.Name, removed.Label),
	))
}

// ─────────────────────────────────────────
//  /setcf command (manual update CF creds)
// ─────────────────────────────────────────

func (h *Handler) handleSetCFCommand(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) != 2 {
		h.sendMd(msg.Chat.ID,
			"⚠️ Format: `/setcf <email> <api_key>`\n\nContoh:\n`/setcf user@gmail.com cfk_xxxxxx`\n\n"+
				"📍 Global API Key: cloudflare.com → My Profile → API Tokens → *Global API Key*")
		return
	}
	email, apiKey := args[0], args[1]
	if !strings.Contains(email, "@") {
		h.send(msg.Chat.ID, "⚠️ Email tidak valid.")
		return
	}
	// Update "default" account or first account, or create it
	updated := false
	for i := range h.cfg.CFAccounts {
		if h.cfg.CFAccounts[i].Name == "default" || i == 0 {
			h.cfg.CFAccounts[i].Email = email
			h.cfg.CFAccounts[i].APIKey = apiKey
			updated = true
			break
		}
	}
	if !updated {
		h.cfg.CFAccounts = append(h.cfg.CFAccounts, config.CFAccount{
			Name:   "default",
			Email:  email,
			APIKey: apiKey,
		})
	}
	if err := h.cfg.Save(h.configPath); err != nil {
		log.Printf("save config error: %v", err)
		h.send(msg.Chat.ID, "❌ Gagal menyimpan config.")
		return
	}
	// Hapus pesan yang berisi API Key dari history chat
	h.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID))
	h.sendMd(msg.Chat.ID, fmt.Sprintf("✅ *Cloudflare diperbarui!*\n📧 Email: `%s`", email))
}

// ─── Wizard reply-keyboard helpers ───────────────────────────────────────────

// sendWizardMsg: pesan dengan tombol [❌ Cancel] di reply keyboard bawah.
func (h *Handler) sendWizardMsg(chatID int64, text string) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendWizardMsgSkip: pesan dengan [⏩ Skip Label] dan [❌ Cancel].
func (h *Handler) sendWizardMsgSkip(chatID int64, text string) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⏩ Skip Label"),
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendTypeSelectMsg: pesan pilih tipe redirect di reply keyboard bawah.
func (h *Handler) sendTypeSelectMsg(chatID int64) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("V2 - Redirect Rules"),
			tgbotapi.NewKeyboardButton("V1 - Page Rules"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID,
		"🌐 *Langkah 4/5 — Pilih Tipe Redirect*\n\n"+
			"Buka Cloudflare → pilih domain → klik *Rules* di sidebar kiri.\n"+
			"Lihat tab mana yang punya redirect rule aktif.\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"📄 *V1 — Page Rules* (Versi Lama)\n\n"+
			"Pilih ini kalau redirect kamu ada di *Rules → Page Rules*\n\n"+
			"Ciri-cirinya:\n"+
			"• Ada list URL pattern, contoh: `domain.com/*`\n"+
			"• Action-nya *\"Forwarding URL\"* atau *\"301 Redirect\"*\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"📋 *V2 — Redirect Rules* (Versi Baru)\n\n"+
			"Pilih ini kalau redirect kamu ada di *Rules → Redirect Rules*\n\n"+
			"Ciri-cirinya:\n"+
			"• Ada tabel dengan kolom *Name, Matches, Then...*\n"+
			"• Ada tombol *\"Create rule\"* di pojok kanan\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"💡 *Tidak yakin?*\n"+
			"Cek dua-duanya. Pilih yang sudah ada rule redirect aktifnya 👇",
	)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendConfirmDomainMsg: pesan konfirmasi domain dengan [✅ Simpan Domain] dan [❌ Cancel].
func (h *Handler) sendConfirmDomainMsg(chatID int64, text string) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Simpan Domain"),
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendNewDomainTypeSelectMsg: pilih versi redirect untuk domain BARU yang akan dibuat.
func (h *Handler) sendNewDomainTypeSelectMsg(chatID int64) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("V2 - Redirect Rules"),
			tgbotapi.NewKeyboardButton("V1 - Page Rules"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID,
		"🌐 *Step 2: Versi Redirect*\n\n"+
			"Pilih versi redirect yang ingin kamu buat:\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"📋 *V2 — Redirect Rules* (Direkomendasikan)\n\n"+
			"Fitur baru Cloudflare. Lebih fleksibel dan powerful.\n"+
			"Bot akan membuat rule di *Rules → Redirect Rules*.\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"📄 *V1 — Page Rules* (Versi Lama)\n\n"+
			"Fitur lama, masih berfungsi.\n"+
			"Bot akan membuat rule di *Rules → Page Rules*.\n\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"💡 Pilih *V2* kalau tidak yakin 👇",
	)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendDNSTypeSelectMsg: pilih tipe DNS record (A/AAAA/CNAME).
func (h *Handler) sendDNSTypeSelectMsg(chatID int64) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("A"),
			tgbotapi.NewKeyboardButton("AAAA"),
			tgbotapi.NewKeyboardButton("CNAME"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID,
		"🌐 *Step 4: Tipe DNS Record*\n\n"+
			"Pilih tipe DNS record untuk domain ini:\n\n"+
			"• *A* — arahkan ke IPv4 address (paling umum)\n"+
			"• *AAAA* — arahkan ke IPv6 address\n"+
			"• *CNAME* — arahkan ke hostname lain\n\n"+
			"💡 Pilih *A* kalau punya IP server, atau *A* dengan IP dummy jika hanya ingin redirect 👇",
	)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendDNSMoreOrNextMsg: tampilkan list DNS + tanya tambah lagi atau lanjut.
// dnsType/Name/Value kosong berarti baru saja pakai DNS existing (tidak ada yang baru dibuat).
func (h *Handler) sendDNSMoreOrNextMsg(chatID int64, dnsType, dnsName, dnsValue string, records []cloudflare.DNSRecord) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Tambah DNS Lagi"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Selesai DNS"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true

	var text string
	if dnsType != "" {
		text = fmt.Sprintf(
			"✅ *DNS record berhasil ditambahkan!*\n\n"+
				"📋 Tipe: *%s* | Nama: *%s* | Value: `%s`\n",
			dnsType, dnsName, dnsValue,
		)
	} else {
		text = "✅ *DNS sudah siap digunakan.*\n"
	}

	// Tampilkan semua DNS record saat ini
	if len(records) > 0 {
		text += "\n📋 *DNS Record saat ini:*\n"
		for i, r := range records {
			proxy := "⚪"
			if r.Proxied {
				proxy = "🟠"
			}
			text += fmt.Sprintf("%d. *%s* — `%s` → `%s` %s\n", i+1, r.Type, r.Name, r.Content, proxy)
		}
	}

	text += "\nMau *tambah DNS lagi* atau *lanjut buat redirect rule*?"

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// showExistingDNSMsg: tampilkan DNS record yang sudah ada + 4 opsi aksi.
func (h *Handler) showExistingDNSMsg(chatID int64, records []cloudflare.DNSRecord) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Pakai DNS yang Ada"),
			tgbotapi.NewKeyboardButton("➕ Tambah DNS Baru"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🗑️ Hapus DNS"),
			tgbotapi.NewKeyboardButton("✏️ Edit DNS"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true

	text := "📋 *DNS Record yang sudah ada di domain ini:*\n\n"
	for i, r := range records {
		proxy := "⚪ DNS Only"
		if r.Proxied {
			proxy = "🟠 Proxied"
		}
		text += fmt.Sprintf("%d. *%s* — `%s` → `%s` (%s)\n", i+1, r.Type, r.Name, r.Content, proxy)
	}
	text += "\nPilih aksi yang ingin dilakukan:"

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// dnsRecordShort: format DNS record jadi teks singkat untuk button.
func dnsRecordShort(r cloudflare.DNSRecord) string {
	name := r.Name
	if len(name) > 18 {
		name = name[:15] + "..."
	}
	content := r.Content
	if len(content) > 15 {
		content = content[:12] + "..."
	}
	proxy := "⚪"
	if r.Proxied {
		proxy = "🟠"
	}
	return fmt.Sprintf("%s | %s | %s %s", r.Type, name, content, proxy)
}

// parseDNSButtonIdx: ambil index (0-based) dari teks button DNS.
// Format button: "PREFIX N. TYPE | name | content proxy"
// N adalah nomor 1-based, kita kembalikan N-1.
func parseDNSButtonIdx(text string) int {
	// Hapus prefix emoji/checbox
	for _, prefix := range []string{"☑️ ", "☐ ", "✏️ "} {
		text = strings.TrimPrefix(text, prefix)
	}
	dotIdx := strings.Index(text, ".")
	if dotIdx < 0 {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(text[:dotIdx]))
	if err != nil || n < 1 {
		return -1
	}
	return n - 1
}

// sendDNSDeleteSelectMsg: reply keyboard dengan checkbox per record + tombol hapus/batal.
func (h *Handler) sendDNSDeleteSelectMsg(chatID int64, records []cloudflare.DNSRecord, selected map[int]bool) {
	var rows [][]tgbotapi.KeyboardButton
	for i, r := range records {
		check := "☐"
		if selected[i] {
			check = "☑️"
		}
		label := fmt.Sprintf("%s %d. %s", check, i+1, dnsRecordShort(r))
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(label)))
	}
	rows = append(rows,
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🗑️ Hapus yang Dipilih")),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Batal")),
	)
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true

	// Hitung yang sudah dicentang
	count := 0
	for _, v := range selected {
		if v {
			count++
		}
	}
	text := "🗑️ *Pilih DNS record yang ingin dihapus:*\n\n" +
		"_(Tap nama record untuk centang/uncentang)_"
	if count > 0 {
		text += fmt.Sprintf("\n\n✅ *%d record dipilih*", count)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendDNSEditSelectMsg: reply keyboard dengan list record yang bisa dipilih untuk diedit.
func (h *Handler) sendDNSEditSelectMsg(chatID int64, records []cloudflare.DNSRecord) {
	var rows [][]tgbotapi.KeyboardButton
	for i, r := range records {
		label := fmt.Sprintf("✏️ %d. %s", i+1, dnsRecordShort(r))
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton(label)))
	}
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Batal")))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, "✏️ *Pilih DNS record yang ingin diedit:*")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendDNSProxySelectMsg: pilih proxy on/off.
func (h *Handler) sendDNSProxySelectMsg(chatID int64) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Proxy ON"),
			tgbotapi.NewKeyboardButton("❌ Proxy OFF"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("❌ Cancel")),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID,
		"🌐 *Step 7: Proxy Status*\n\n"+
			"Aktifkan Cloudflare Proxy untuk record ini?\n\n"+
			"🟠 *Proxy ON* — Traffic melewati Cloudflare.\n"+
			"Diperlukan agar redirect rule berfungsi!\n\n"+
			"⚪ *Proxy OFF* — DNS only, redirect rule tidak aktif.\n\n"+
			"💡 Pilih *✅ Proxy ON* untuk redirect berfungsi 👇",
	)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}

// sendReadyToStartMsg: pesan instruksi CF dengan [✅ Sudah, Lanjut] dan [❌ Cancel].
func (h *Handler) sendReadyToStartMsg(chatID int64, text string) {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✅ Sudah, Lanjut"),
			tgbotapi.NewKeyboardButton("❌ Cancel"),
		),
	)
	kb.ResizeKeyboard = true
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("send error: %v", err)
	}
}
