package i18n

import (
	"fmt"
	"strings"

	db "bot/databases"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	LangID      = "id"
	LangEN      = "en"
	DefaultLang = LangID
)

// ResolveLang determines the active language for a message:
// 1. User's personal language preference (if explicitly set via .setlang)
// 2. Group's language preference (if in a group)
// 3. DefaultLang ("id" — Indonesian)
func ResolveLang(client *whatsmeow.Client, msg *events.Message) string {
	store := db.GetBotStore()
	if store == nil || msg == nil {
		return DefaultLang
	}
	if sender := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt); sender != "" {
		if u, err := store.GetUser(sender); err == nil && u != nil && (u.Lang == LangID || u.Lang == LangEN) {
			return u.Lang
		}
	}
	if msg.Info.IsGroup {
		if cfg, err := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String()); err == nil && cfg != nil && (cfg.Lang == LangID || cfg.Lang == LangEN) {
			return cfg.Lang
		}
	}
	return DefaultLang
}

// T looks up a localized format string for lang (falling back to English, then key)
// and formats it with args when provided.
func T(lang, key string, args ...any) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if l != LangID && l != LangEN {
		l = DefaultLang
	}
	tpl, ok := catalogs[l][key]
	if !ok {
		tpl, ok = catalogs[LangEN][key]
		if !ok {
			tpl = key
		}
	}
	if len(args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, args...)
}

// TMsg resolves the language for msg and formats key with args.
func TMsg(client *whatsmeow.Client, msg *events.Message, key string, args ...any) string {
	return T(ResolveLang(client, msg), key, args...)
}

var catalogs = map[string]map[string]string{
	LangID: {
		// System & Permissions
		"err.owner_only":     "⛔ Akses Ditolak: Perintah ini khusus untuk *Owner Bot*.",
		"err.group_only":     "👥 Perintah ini hanya bisa digunakan di dalam *Grup*.",
		"err.admin_only":     "🛡️ Perintah ini khusus untuk *Admin Grup*.",
		"err.bot_admin":      "⚠️ Jadikan bot sebagai *Admin Grup* terlebih dahulu.",
		"err.nsfw_group_off": "🔞 NSFW dimatikan di grup ini. Admin dapat mengaktifkannya dengan `.nsfw on`.",
		"err.nsfw_bot_off":   "🔞 NSFW dimatikan di bot ini. Owner dapat mengaktifkannya dengan `.nsfw on`.",
		"err.jadibot_locked": "🔒 *JadiBot Dikunci!* Fitur pairing QR hanya dapat dibuat oleh Owner Bot. Hubungi Owner untuk membeli slot bot.",
		"lang.set_user":      "✅ Bahasa pribadi kamu diubah ke *%s*.",
		"lang.set_group":     "✅ Bahasa grup diubah ke *%s*.",
		"lang.usage":         "🌐 Penggunaan: `.setlang id` (Bahasa Indonesia) atau `.setlang en` (English)",

		// Menu
		"menu.user":           "Pengguna",
		"menu.runtime":        "Aktif",
		"menu.prefix":         "Prefix",
		"menu.total_cmd":      "Total Perintah",
		"menu.platform":       "Platform",
		"menu.language":       "Bahasa",
		"menu.categories":     "KATEGORI",
		"menu.show_all":       "Tampilkan Semua Perintah",
		"menu.footer":         "Balas dengan angka *1-%d* atau *0*, atau ketik `%smenu <kategori>`, `%smenu <perintah>`, atau `%smenu all`.\nGanti bahasa: `.setlang id` / `.setlang en`",
		"menu.all_title":      "📜 *Semua Perintah*",
		"menu.not_found":      "⚠️ Kategori atau perintah `%s` tidak ditemukan.\nKetik `%smenu` untuk melihat daftar kategori.",
		"menu.empty_category": "Kategori `%s` belum memiliki perintah.",
		"menu.no_commands":    "Belum ada perintah yang terdaftar.",
		"menu.cmd_info":       "ℹ️ *Info Perintah*",
		"menu.field_command":  "Perintah",
		"menu.field_category": "Kategori",
		"menu.field_desc":     "Deskripsi",
		"menu.field_aliases":  "Alias",
		"menu.field_usage":    "Penggunaan",
		"menu.no_desc":        "Deskripsi belum tersedia.",
		"menu.owner_denied":   "⛔ Akses Ditolak: Kategori Pemilik khusus untuk *Owner Bot*.",

		// Confess / Menfess
		"confess.usage":       "💌 *Penggunaan Confess / Menfess:*\n`.confess <nomor_wa> | <pesan> [| nama_samaran]`\n\nContoh:\n`.confess 628123456789 | Halo, aku udah lama kagum sama kamu! | Pengagum Rahasia`\n\nPerintah lain:\n• `.balasconfess <ID> <balasan>` — Balas secara anonim\n• `.stopconfess <ID>` — Tutup sesi confess\n• `.blockconfess <ID>` — Blokir pengirim secara anonim\n• `.confessmode on|off` — Aktif/matikan penerimaan confess\n• `.confessinbox` — Lihat riwayat confess kamu",
		"confess.invalid_num": "❌ Nomor tujuan tidak valid. Gunakan format angka seperti `628123456789`.",
		"confess.self":        "😅 Kamu tidak bisa mengirim confess ke diri sendiri!",
		"confess.cooldown":    "⏳ Tunggu `%ds` lagi sebelum mengirim confess berikutnya.",
		"confess.opt_out":     "🚫 Penerima ini telah menonaktifkan penerimaan confess anonim.",
		"confess.blocked":     "🚫 Kamu tidak dapat mengirim confess ke pengguna ini.",
		"confess.delivered":   "💌 *Confess Terkirim Secara Anonim!*\n• ID: `#%s`\n• Nama Samaran: *%s*\n\nJika penerima membalas, balasannya akan diteruskan ke DM kamu secara anonim.",
		"confess.dm_card":     "💌 *ADA CONFESS ANONIM UNTUKMU!*\n╭─────────────\n│ *ID:* `#%s`\n│ *Dari:* %s\n╰─────────────\n\n%s\n\n_Balas langsung pesan ini atau ketik `.balasconfess %s <balasan>` untuk membalas secara anonim._\n_Ketik `.stopconfess %s` untuk menutup atau `.blockconfess %s` untuk memblokir pengirim._",
		"confess.reply_dm":    "💬 *Balasan Anonim (`#%s`)*\n*Dari %s:*\n\n%s\n\n_Balas langsung pesan ini untuk melanjutkan obrolan anonim, atau `.stopconfess %s` untuk berhenti._",
		"confess.reply_sent":  "✅ Balasan anonim untuk `#%s` telah terkirim!",
		"confess.not_found":   "❌ Confess `#%s` tidak ditemukan atau bukan milikmu.",
		"confess.closed":      "🔒 Sesi confess `#%s` sudah ditutup.",
		"confess.stopped":     "🔒 Sesi confess `#%s` berhasil ditutup. Kedua pihak tidak dapat membalas lagi.",
		"confess.blocked_ok":  "🛡️ Pengirim `#%s` telah diblokir secara anonim dan sesi ditutup.",
		"confess.mode_on":     "💌 Penerimaan confess anonim sekarang *AKTIF*.",
		"confess.mode_off":    "🚫 Penerimaan confess anonim sekarang *DIMATIKAN*.",

		// RPG — Coinsteel Empire
		"rpg.no_account":      "⚔️ Kamu belum memulai kerajaanmu! Ketik `.start` (atau `.start 20`) untuk mulai bermain RPG Coinsteel.",
		"rpg.already_started": "👑 Kerajaanmu sudah berdiri! Ketik `.rpgprofile` atau `.empire` untuk melihat statusmu.",
		"rpg.ask_age":         "👑 *Selamat datang, Calon Penguasa Coinsteel!*\n\nUntuk mendirikan kerajaanmu, balas pesan ini dengan usiamu (contoh: `20`), atau ketik `.start 20`.",
		"rpg.invalid_age":     "⚠️ Masukkan angka usia yang valid antara 10 sampai 100 (contoh: `20`).",
		"rpg.created":         "🎉 *Selamat, %s!* Kerajaanmu telah resmi berdiri (Usia %d)!\nKamu mendapatkan modal awal: *¥%d*, *50 Besi*, *20 Sayur*, *20 Buah*, dan *Perisai Raid 24 Jam*.\n\nKetik `.rpgprofile` untuk melihat kerajaanmu, `.work` untuk bekerja, `.build` untuk membangun, `.hunt` untuk berburu, atau `.menu rpg` untuk daftar lengkap!",
		"rpg.no_energy":       "⚡ Energi tidak cukup! Butuh `%d` energi, saat ini kamu hanya punya `%d/%d`.\n_Tips: Makan dengan `.eat` atau beli Energy Potion di `.shop`!_",
		"rpg.cooldown":        "⏳ Sedang cooldown! Coba lagi dalam `%s`.",
		"rpg.work_ok":         "💼 *Bekerja sebagai %s!*\n• Pendapatan: `+¥%d` & `+%d XP`\n• Energi terpakai: `-%d` (Sisa: `%d/%d`)%s",
		"rpg.worksite_none":   "🚧 Kamu tidak punya proyek pembangunan aktif. Ketik `.build` untuk membangun gedung baru!",
		"rpg.worksite_card":   "🚧 *Proyek Aktif: %s (Lv.%d)*\nProgress: `%s` *%.1f%%* (%d/%d)\nEnergi kamu: *%d / %d*\n\nReaksi dengan ⛏️ pada pesan ini atau ketik `.contribute` untuk menyumbang 10 energi!",
		"rpg.worksite_done":   "🎉 *Pembangunan Selesai!* 🎉\n*%s (Lv.%d)* kini aktif dan mulai menghasilkan sumber daya otomatis!\nKetik `.collect` kapan saja untuk mengambil hasilnya.",
		"rpg.collect_none":    "📦 Belum ada hasil produksi yang siap diambil dari bangunan aktifmu. Tunggu beberapa menit lagi!",
		"rpg.collect_ok":      "📦 *Hasil Produksi Kerajaan Diambil!*\n%s\n\n_Bangun lebih banyak tambang & kebun dengan `.build`!_",
	},
	LangEN: {
		// System & Permissions
		"err.owner_only":     "⛔ Access Denied: This command is restricted to the *Bot Owner*.",
		"err.group_only":     "👥 This command can only be used inside a *Group*.",
		"err.admin_only":     "🛡️ *Group Admin* permission is required for this command.",
		"err.bot_admin":      "⚠️ Please make the bot a *Group Admin* first.",
		"err.nsfw_group_off": "🔞 NSFW is disabled in this group. An admin can enable it with `.nsfw on`.",
		"err.nsfw_bot_off":   "🔞 NSFW is disabled on this bot. Owner can enable it with `.nsfw on`.",
		"err.jadibot_locked": "🔒 *JadiBot is Locked!* Only the Bot Owner can generate QR pairing slots. Contact the Owner to purchase a bot slot.",
		"lang.set_user":      "✅ Your personal language has been set to *%s*.",
		"lang.set_group":     "✅ Group language has been set to *%s*.",
		"lang.usage":         "🌐 Usage: `.setlang id` (Indonesian) or `.setlang en` (English)",

		// Menu
		"menu.user":           "User",
		"menu.runtime":        "Runtime",
		"menu.prefix":         "Prefix",
		"menu.total_cmd":      "Total Commands",
		"menu.platform":       "Platform",
		"menu.language":       "Language",
		"menu.categories":     "CATEGORIES",
		"menu.show_all":       "Show All Commands",
		"menu.footer":         "Reply with *1-%d* or *0*, or run `%smenu <category>`, `%smenu <command>`, or `%smenu all`.\nSwitch language: `.setlang id` / `.setlang en`",
		"menu.all_title":      "📜 *All Commands*",
		"menu.not_found":      "⚠️ Category or command `%s` not found.\nUse `%smenu` to view all categories.",
		"menu.empty_category": "Category `%s` has no commands.",
		"menu.no_commands":    "No commands registered yet.",
		"menu.cmd_info":       "ℹ️ *Command Info*",
		"menu.field_command":  "Command",
		"menu.field_category": "Category",
		"menu.field_desc":     "Description",
		"menu.field_aliases":  "Aliases",
		"menu.field_usage":    "Usage",
		"menu.no_desc":        "No description available.",
		"menu.owner_denied":   "⛔ Access Denied: Owner commands are restricted to the *Bot Owner*.",

		// Confess / Menfess
		"confess.usage":       "💌 *Anonymous Confess / Menfess Usage:*\n`.confess <phone> | <message> [| alias]`\n\nExample:\n`.confess 628123456789 | Hey, I've admired you for a long time! | Secret Admirer`\n\nOther commands:\n• `.balasconfess <ID> <reply>` — Reply anonymously\n• `.stopconfess <ID>` — Close confession thread\n• `.blockconfess <ID>` — Anonymously block sender\n• `.confessmode on|off` — Enable/disable receiving confessions\n• `.confessinbox` — List your confession threads",
		"confess.invalid_num": "❌ Invalid target phone number. Use digits like `628123456789`.",
		"confess.self":        "😅 You cannot send a confession to yourself!",
		"confess.cooldown":    "⏳ Please wait `%ds` before sending another confession.",
		"confess.opt_out":     "🚫 This user has opted out of receiving anonymous confessions.",
		"confess.blocked":     "🚫 You cannot send confessions to this user.",
		"confess.delivered":   "💌 *Confession Sent Anonymously!*\n• ID: `#%s`\n• Alias: *%s*\n\nIf they reply, it will be relayed to your DM anonymously.",
		"confess.dm_card":     "💌 *YOU RECEIVED AN ANONYMOUS CONFESSION!*\n╭─────────────\n│ *ID:* `#%s`\n│ *From:* %s\n╰─────────────\n\n%s\n\n_Reply directly to this message or run `.balasconfess %s <reply>` to answer anonymously._\n_Use `.stopconfess %s` to close or `.blockconfess %s` to block the sender._",
		"confess.reply_dm":    "💬 *Anonymous Reply (`#%s`)*\n*From %s:*\n\n%s\n\n_Reply directly to continue anonymously, or `.stopconfess %s` to end._",
		"confess.reply_sent":  "✅ Anonymous reply for `#%s` delivered!",
		"confess.not_found":   "❌ Confession `#%s` not found or not yours.",
		"confess.closed":      "🔒 Confession `#%s` is already closed.",
		"confess.stopped":     "🔒 Confession `#%s` closed. No further replies will be relayed.",
		"confess.blocked_ok":  "🛡️ Sender of `#%s` has been anonymously blocked and thread closed.",
		"confess.mode_on":     "💌 Receiving anonymous confessions is now *ON*.",
		"confess.mode_off":    "🚫 Receiving anonymous confessions is now *OFF*.",

		// RPG — Coinsteel Empire
		"rpg.no_account":      "⚔️ You haven't founded your empire yet! Use `.start` (or `.start 20`) to begin Coinsteel RPG.",
		"rpg.already_started": "👑 You already have an empire! Use `.rpgprofile` or `.empire` to inspect it.",
		"rpg.ask_age":         "👑 *Welcome, Future Ruler of Coinsteel!*\n\nReply with your age (e.g. `20`), or run `.start 20`.",
		"rpg.invalid_age":     "⚠️ Please enter a valid age between 10 and 100 (e.g. `20`).",
		"rpg.created":         "🎉 *Congratulations, %s!* Your empire has been founded at age %d!\nStarting pack: *¥%d*, *50 Iron*, *20 Vegetables*, *20 Fruit*, and a *24h Raid Shield*.\n\nUse `.rpgprofile`, `.work`, `.build`, `.hunt`, or `.menu rpg` to explore!",
		"rpg.no_energy":       "⚡ Not enough energy! Need `%d`, you have `%d/%d`.\n_Tip: Use `.eat` or buy an Energy Potion in `.shop`!_",
		"rpg.cooldown":        "⏳ On cooldown! Try again in `%s`.",
		"rpg.work_ok":         "💼 *Worked as %s!*\n• Earned: `+¥%d` & `+%d XP`\n• Energy spent: `-%d` (Left: `%d/%d`)%s",
		"rpg.worksite_none":   "🚧 You have no active construction project. Use `.build` to start one!",
		"rpg.worksite_card":   "🚧 *Active Worksite: %s (Lv.%d)*\nProgress: `%s` *%.1f%%* (%d/%d)\nEnergy: *%d / %d*\n\nReact with ⛏️ on this message or run `.contribute` to spend 10 energy!",
		"rpg.worksite_done":   "🎉 *Construction Complete!* 🎉\n*%s (Lv.%d)* is now active and producing passive resources!\nUse `.collect` anytime to claim production.",
		"rpg.collect_none":    "📦 No passive production ready to collect yet. Check back in a few minutes!",
		"rpg.collect_ok":      "📦 *Empire Production Collected!*\n%s\n\n_Build more mines & farms with `.build`!_",
	},
}
