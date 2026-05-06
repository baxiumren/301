# Cloudflare Redirect Bot — Design Spec
Date: 2026-05-06

## Overview

Telegram bot berbasis Go untuk mengganti URL tujuan redirect domain Cloudflare tanpa harus buka dashboard. Bot dioperasikan di dalam grup Telegram oleh tim dengan akses whitelist.

## Domains & Tipe Redirect

| Domain | Tipe | CF API |
|--------|------|--------|
| 301maha.store | Versi 1 — Redirect Rules | Ruleset API |
| maha301.lol | Versi 1 — Redirect Rules | Ruleset API |
| maha55.id | Versi 1 — Redirect Rules | Ruleset API |
| maha66.id | Versi 1 — Redirect Rules | Ruleset API |
| mh301sl.store | Versi 2 — Page Rules | Page Rules API |

Semua domain berada dalam satu akun Cloudflare.

## Arsitektur

```
[Telegram Group] → [Bot (Go, Polling)] → [Cloudflare API]
                         ↑
                   [config.yaml]
```

### Struktur File

```
cf-redirect-bot/
├── main.go
├── config/
│   └── config.go
├── bot/
│   └── handler.go
├── cloudflare/
│   └── client.go
└── config.yaml
```

## Stack & Library

- **Bahasa:** Go
- **Mode:** Long polling (tidak butuh domain/SSL)
- **Telegram library:** `go-telegram-bot-api/telegram-bot-api/v5`
- **HTTP client:** Go standard `net/http` untuk Cloudflare API calls
- **Config:** `config.yaml` dibaca saat startup

## Commands

| Command | Fungsi |
|---------|--------|
| `/redirect` | Tampilkan list domain via inline keyboard untuk ganti URL |
| `/status` | Tampilkan URL tujuan redirect semua domain saat ini |

## UX Flow

### Flow Ganti Redirect

```
User: /redirect

Bot: 🌐 Pilih domain yang mau diganti:
     [301maha.store]  [maha301.lol]
     [maha55.id]      [maha66.id]
     [mh301sl.store]
     [❌ Cancel]

User: *klik salah satu domain*

Bot: 📌 301maha.store (Redirect Rules)
     URL sekarang: https://pemaindim.life/daftar?ref=mahaslot

     Kirim URL tujuan baru (atau klik Cancel):
     [❌ Cancel]

User: https://newsite.com/daftar?ref=abc

Bot: ✅ Berhasil diubah!
     Domain : 301maha.store
     URL Baru: https://newsite.com/daftar?ref=abc
```

### Flow Cancel

```
User: *klik tombol Cancel kapanpun saat flow sedang berjalan*

Bot: 🚫 Dibatalkan.
```

### Flow Status

```
User: /status

Bot: 📊 Status Redirect Semua Domain:

     🔹 301maha.store (Redirect Rules)
     → https://pemaindim.life/daftar?ref=mahaslot

     🔹 maha301.lol (Redirect Rules)
     → https://...

     🔹 maha55.id (Redirect Rules)
     → https://...

     🔹 maha66.id (Redirect Rules)
     → https://...

     🔹 mh301sl.store (Page Rules)
     → https://maha-main.store/daftar?ref=hackgacor
```

### Akses Ditolak

```
Bot: ⛔ Kamu tidak memiliki akses untuk menggunakan command ini.
```

State sementara (domain yang dipilih user) disimpan in-memory per user ID selama sesi input URL. Cancel menghapus state tersebut.

## Access Control

- Whitelist berupa list **Telegram User ID** (integer) di `config.yaml`
- Bot mengecek whitelist di setiap command dan callback button
- User yang tidak terdaftar mendapat pesan ⛔ dan request diabaikan
- Pengelolaan whitelist dilakukan manual via edit `config.yaml` + restart bot

## Konfigurasi (config.yaml)

```yaml
telegram:
  token: "BOT_TOKEN_DARI_BOTFATHER"

cloudflare:
  api_token: "CF_API_TOKEN_DENGAN_PERMISSION_EDIT_RULES"

whitelist:
  - 123456789
  - 987654321

domains:
  - name: "301maha.store"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "maha301.lol"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "maha55.id"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "maha66.id"
    zone_id: "ZONE_ID"
    type: "redirect_rules"
    ruleset_id: "RULESET_ID"
    rule_id: "RULE_ID"
  - name: "mh301sl.store"
    zone_id: "ZONE_ID"
    type: "page_rules"
    rule_id: "RULE_ID"
```

## Cloudflare API Integration

### Versi 1 — Redirect Rules (Ruleset API)

```
PATCH /zones/{zone_id}/rulesets/{ruleset_id}/rules/{rule_id}
Authorization: Bearer {api_token}
Content-Type: application/json

{
  "action": "redirect",
  "action_parameters": {
    "from_value": {
      "target_url": {
        "value": "<new_url>"
      },
      "status_code": 301,
      "preserve_query_string": false
    }
  },
  "expression": "true",
  "enabled": true
}
```

### Versi 2 — Page Rules API

```
PATCH /zones/{zone_id}/pagerules/{rule_id}
Authorization: Bearer {api_token}
Content-Type: application/json

{
  "actions": [{
    "id": "forwarding_url",
    "value": {
      "url": "<new_url>",
      "status_code": 301
    }
  }]
}
```

Bot memilih API yang tepat berdasarkan field `type` di config.

## Validasi Input

- URL baru harus diawali `https://` — jika tidak, bot balas pesan error dan minta ulang
- Timeout input user: jika dalam 2 menit tidak ada URL yang dikirim, sesi dibatalkan otomatis

## Error Handling

| Skenario | Response Bot |
|----------|-------------|
| CF API error | ❌ Gagal mengubah URL. Coba lagi. (+ log error di server) |
| URL tidak valid | ⚠️ URL harus diawali dengan https:// |
| User tidak di whitelist | ⛔ Kamu tidak memiliki akses |
| Timeout input | ⏰ Sesi dibatalkan. Kirim /redirect untuk mulai lagi. |
| CF API error saat /status | ❌ Gagal mengambil data domain X. |

## Out of Scope (V1)

- Tambah/hapus domain via Telegram
- Manage whitelist via Telegram
- Enable/disable redirect rule
- Support redirect tipe lain (Bulk Redirects, Workers)
- Dashboard atau web UI
