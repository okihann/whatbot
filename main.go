package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"bot/ai"
	_ "bot/commands/animanga"
	_ "bot/commands/audiofx"
	_ "bot/commands/bot"
	_ "bot/commands/downloader"
	_ "bot/commands/fun"
	_ "bot/commands/game"
	_ "bot/commands/general"
	"bot/commands/groupadmin"
	_ "bot/commands/groupsec"
	_ "bot/commands/minigame"
	"bot/commands/owner"
	_ "bot/commands/search"
	_ "bot/commands/sticker"
	_ "bot/commands/tool"
	_ "bot/commands/tools"
	db "bot/databases"
	"bot/handlers"
	"bot/ngrok"
	"bot/pairing"
	"bot/web"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waWa6"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var (
	activeClients = make(map[string]*whatsmeow.Client)
	clientsMu     sync.Mutex
	dbContainer   *sqlstore.Container
	botStartTime  time.Time
	publicTunnel  = &ngrok.Tunnel{}
	ngrokConfig   ngrok.Config
	webHub        *web.Hub
	pairQueue     *pairing.Queue
)

// botAccountManager bridges the web package with main's active WhatsApp clients.
type botAccountManager struct{}

func (m *botAccountManager) ListAccounts() []web.AccountInfo {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	list := make([]web.AccountInfo, 0, len(activeClients))
	for id, client := range activeClients {
		if client == nil || client.Store == nil || client.Store.ID == nil {
			continue
		}
		list = append(list, web.AccountInfo{
			ID:        id,
			User:      client.Store.ID.ToNonAD().User,
			PushName:  client.Store.PushName,
			Connected: client.IsConnected(),
			Platform:  "Android Companion",
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].User < list[j].User
	})
	return list
}

func (m *botAccountManager) GetClient(accountID string) (*whatsmeow.Client, bool) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	if c, ok := activeClients[accountID]; ok {
		return c, true
	}
	for _, c := range activeClients {
		if c != nil && c.Store != nil && c.Store.ID != nil && c.Store.ID.ToNonAD().User == accountID {
			return c, true
		}
	}
	return nil, false
}

func (m *botAccountManager) StartPairing() error {
	if pairQueue == nil {
		return fmt.Errorf("pairing queue not initialized")
	}
	// If a ticket is already actively pairing, let the browser show its live QR.
	if tok, _, _, _ := pairQueue.ActiveQR(); tok != "" {
		return nil
	}
	_, err := pairQueue.Enqueue(pairing.EnqueueRequest{
		Label: "Admin Web Pair",
	})
	return err
}

func (m *botAccountManager) RemoveAccount(accountID string) error {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	targetKey := ""
	var targetClient *whatsmeow.Client
	if c, ok := activeClients[accountID]; ok {
		targetKey = accountID
		targetClient = c
	} else {
		for k, c := range activeClients {
			if c != nil && c.Store != nil && c.Store.ID != nil && c.Store.ID.ToNonAD().User == accountID {
				targetKey = k
				targetClient = c
				break
			}
		}
	}
	if targetClient == nil {
		return fmt.Errorf("account '%s' not found", accountID)
	}

	userNum := ""
	if targetClient.Store != nil && targetClient.Store.ID != nil {
		userNum = targetClient.Store.ID.ToNonAD().User
	}
	targetClient.Disconnect()
	_ = targetClient.Store.Delete(context.Background())
	delete(activeClients, targetKey)
	if bs := db.GetBotStore(); bs != nil && userNum != "" {
		_ = bs.DeleteTenant(userNum)
	}
	return nil
}

func snapshotClients() []*whatsmeow.Client {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	out := make([]*whatsmeow.Client, 0, len(activeClients))
	for _, c := range activeClients {
		if c != nil && c.IsConnected() {
			out = append(out, c)
		}
	}
	return out
}

// configureAndroidCompanion presents the client as a linked Android companion
// device (removing WebInfo) so WhatsApp delivers view-once media instead of
// blocking it with <unavailable type="view_once">. Existing sessions paired
// under Web/iPad must be re-paired once via QR for WhatsApp to update their
// registered platform type.
func configureAndroidCompanion() {
	if store.DeviceProps != nil {
		store.DeviceProps.Os = proto.String("Android")
		store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_ANDROID_PHONE.Enum()
		store.DeviceProps.RequireFullSync = proto.Bool(false)
		store.DeviceProps.Version = &waCompanionReg.DeviceProps_AppVersion{
			Primary:    proto.Uint32(2),
			Secondary:  proto.Uint32(24),
			Tertiary:   proto.Uint32(24),
			Quaternary: proto.Uint32(0),
		}
	}
	if store.BaseClientPayload != nil && store.BaseClientPayload.UserAgent != nil {
		store.BaseClientPayload.UserAgent.Platform = waWa6.ClientPayload_UserAgent_ANDROID.Enum()
		store.BaseClientPayload.UserAgent.Device = proto.String("SM-G900F")
		store.BaseClientPayload.UserAgent.Manufacturer = proto.String("Samsung")
		store.BaseClientPayload.UserAgent.OsVersion = proto.String("14")
		store.BaseClientPayload.UserAgent.OsBuildNumber = proto.String("UP1A.231005.007")
		store.BaseClientPayload.WebInfo = nil
	}
}

func registerClient(client *whatsmeow.Client) {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return
	}
	clientsMu.Lock()
	activeClients[client.Store.ID.String()] = client
	clientsMu.Unlock()
	if webHub != nil {
		webHub.Broadcast("accounts_updated", nil)
	}
}

func attachClientHandlers(client *whatsmeow.Client) {
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			fmt.Printf("\n✅ Bot %s is live and connected!\n", client.Store.ID)
			registerClient(client)
		case *events.Disconnected:
			if webHub != nil {
				webHub.Broadcast("accounts_updated", nil)
			}
		case *events.HistorySync:
			if webHub != nil {
				webHub.IngestHistorySync(client, v)
			}
		case *events.GroupInfo:
			handlers.HandleGroupParticipant(client, v)
		case *events.CallOffer:
			handlers.HandleCallOffer(client, v)
		case *events.UndecryptableMessage:
			owner.HandleUnavailableViewOnce(client, v)
		case *events.Message:
			if webHub != nil {
				webHub.IngestMessage(client, v)
			}
			// Allow a 15s clock-skew grace window; older offline messages are
			// cached for anti-delete / /get without triggering commands.
			if v.Info.Timestamp.Before(botStartTime.Add(-15 * time.Second)) {
				owner.CacheOnlyMessage(client, v)
				return
			}
			owner.HandleAutomations(client, v)
			handlers.HandleCommand(client, v)
		}
	})
}

func setupExistingClient(deviceStore *store.Device) {
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	attachClientHandlers(client)

	if err := client.Connect(); err != nil {
		fmt.Println("Failed to connect existing device:", err)
		return
	}
	registerClient(client)
}

// pairNewAndroidCompanion is the PairSessionFunc invoked strictly one-at-a-time
// by pairing.Queue so multiple buyers never collide on QR codes.
func pairNewAndroidCompanion(ctx context.Context, emitQR func(code string)) (string, error) {
	if dbContainer == nil {
		return "", fmt.Errorf("database container not initialized")
	}
	deviceStore := dbContainer.NewDevice()
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	attachClientHandlers(client)

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return "", fmt.Errorf("get QR channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		if webHub != nil {
			webHub.PublishQREvent("error")
		}
		return "", fmt.Errorf("connect: %w", err)
	}

	paired := false
	for evt := range qrChan {
		if evt.Event == "code" {
			emitQR(evt.Code)
			if webHub != nil {
				webHub.PublishQR(evt.Code)
			}
			config := qrterminal.Config{Level: qrterminal.L, Writer: os.Stdout, HalfBlocks: true}
			fmt.Println("\n=================================================")
			fmt.Println("Scan this QR code to link a new WhatsApp account:")
			fmt.Println("(Also live in the WhatsApp Web UI & /pair/<token>)")
			fmt.Println("=================================================")
			qrterminal.GenerateWithConfig(evt.Code, config)
		} else {
			fmt.Println("Login event:", evt.Event)
			if webHub != nil {
				webHub.PublishQREvent(evt.Event)
			}
			if evt.Event == "success" {
				paired = true
			}
		}
	}

	if paired && client.Store.ID != nil {
		registerClient(client)
		return client.Store.ID.ToNonAD().User, nil
	}

	client.Disconnect()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", fmt.Errorf("pairing did not complete")
}

func startTenantExpiryReaper(stopCh <-chan struct{}, mgr *botAccountManager) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case now := <-ticker.C:
				bs := db.GetBotStore()
				if bs == nil {
					continue
				}
				tenants, _ := bs.ListTenants()
				for _, t := range tenants {
					if t.ExpiresAt > 0 && now.Unix() > t.ExpiresAt {
						fmt.Printf("⏰ [TENANT] Subscription expired for %s — disconnecting...\n", t.AccountID)
						_ = mgr.RemoveAccount(t.AccountID)
						if webHub != nil {
							webHub.Broadcast("accounts_updated", nil)
						}
					}
				}
			}
		}
	}()
}

func startNgrok() {
	if err := publicTunnel.Start(ngrokConfig); err != nil {
		fmt.Printf("⚠️ [NGROK] %v\n", err)
	}
}

func stopNgrok() {
	if err := publicTunnel.Stop(); err != nil {
		fmt.Printf("⚠️ [NGROK] Shutdown error: %v\n", err)
	}
}

func main() {
	configureAndroidCompanion()

	ai.LoadEnv()
	ngrokConfig = ngrok.LoadConfig(ai.AppConfig.ServerPort)

	botStartTime = time.Now()
	handlers.SetStartTime(botStartTime)

	dbLog := waLog.Stdout("Database", "INFO", true)
	dbPath := "db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Println("Database directory not found, creating...")
		if err := os.MkdirAll(dbPath, 0755); err != nil {
			panic(fmt.Sprintf("FATAL: Could not create database directory: %v", err))
		}
	}

	// 1. Sequential WAL SQLite for whatsmeow Signal/device store.
	sessionDB, err := db.OpenSequentialSQLite(filepath.Join(dbPath, "session.db"))
	if err != nil {
		panic(fmt.Sprintf("FATAL: Could not open session SQLite: %v", err))
	}

	// 2. Dedicated sequential WAL + batched-writer SQLite for the WhatsApp Web UI.
	chatStore, err := db.OpenChatStore(
		filepath.Join(dbPath, "webchat.db"),
		filepath.Join(dbPath, "webmedia"),
	)
	if err != nil {
		panic(fmt.Sprintf("FATAL: Could not open webchat SQLite: %v", err))
	}

	// 3. Dedicated sequential WAL SQLite for tenants, pairing queue, profiles, groups, and games.
	botStore, err := db.OpenBotStore(filepath.Join(dbPath, "botdata.db"))
	if err != nil {
		panic(fmt.Sprintf("FATAL: Could not open botdata SQLite: %v", err))
	}
	db.SetBotStore(botStore)

	dbContainer = sqlstore.NewWithDB(sessionDB, "sqlite", dbLog)
	if err := dbContainer.Upgrade(context.Background()); err != nil {
		panic(fmt.Sprintf("FATAL: Failed to upgrade database schema: %v", err))
	}

	accountMgr := &botAccountManager{}
	webHub = web.NewHub(chatStore, sessionDB, accountMgr)
	webHub.SetBotStore(botStore)

	// 4. Strict FIFO Sequential QR Pairing Queue (90s timeout per slot) for selling bots.
	pairQueue = pairing.NewQueue(botStore, pairNewAndroidCompanion, pairing.DefaultPairingTimeout, func() {
		if webHub != nil {
			webHub.Broadcast("queue_updated", nil)
		}
	})
	pairing.SetDefaultQueue(pairQueue)
	webHub.SetPairingQueue(pairQueue)

	// Start HTTP server with WhatsApp Web UI + Sequential /pair/{token} + Arcade /games + AI API.
	go ai.StartServerWithExtra(webHub.Mount)

	// NGROK_ENABLED controls startup. Credentials remain environment-only.
	startNgrok()

	stopBg := make(chan struct{})
	groupadmin.StartGroupScheduler(stopBg, snapshotClients)
	startTenantExpiryReaper(stopBg, accountMgr)

	devices, err := dbContainer.GetAllDevices(context.Background())
	if err != nil {
		panic(err)
	}

	if len(devices) == 0 {
		fmt.Println("No linked accounts found. Starting Android Companion sequential pairing (terminal + Web UI)...")
		_ = accountMgr.StartPairing()
	} else {
		fmt.Printf("Found %d saved accounts. Booting them up...\n", len(devices))
		for _, device := range devices {
			setupExistingClient(device)
		}
	}

	fmt.Println("\n==============================================================")
	fmt.Println("Multi-Device Manager is Running. Press CTRL+C to exit.")
	fmt.Printf("WhatsApp Web UI (Android Companion): http://localhost%s\n", ai.AppConfig.ServerPort)
	fmt.Printf("Web Arcade MiniGames               : http://localhost%s/games\n", ai.AppConfig.ServerPort)
	fmt.Printf("Registered Primary Commands        : %d\n", len(handlers.PrimaryCommands))
	fmt.Println("SQLite Mode: Sequential WAL (session.db + webchat.db + botdata.db)")
	fmt.Println("QR Pairing : Strict FIFO Sequential Queue (90s per buyer)")
	fmt.Println("Commands you can type in this console:")
	fmt.Println(" - 'add'       : Queue a new sequential QR pairing slot")
	fmt.Println(" - 'list'      : Show connected bot numbers")
	fmt.Println(" - 'remove ID' : Wipe and unlink an account")
	fmt.Println(" - 'ngrok on'  : Start the configured ngrok tunnel")
	fmt.Println(" - 'ngrok off' : Stop the ngrok tunnel")
	fmt.Println(" - 'ngrok'     : Show current ngrok status")
	fmt.Println("==============================================================")

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.ToLower(strings.TrimSpace(scanner.Text()))

			if strings.HasPrefix(text, "remove ") {
				target := strings.TrimSpace(strings.TrimPrefix(text, "remove"))
				if err := accountMgr.RemoveAccount(target); err != nil {
					fmt.Printf("  [ERROR] %v. Type 'list' to see valid IDs.\n", err)
				} else {
					fmt.Printf("  [SUCCESS] Wiped and removed bot account: %s\n", target)
					webHub.Broadcast("accounts_updated", nil)
				}
				continue
			}

			switch text {
			case "add":
				_ = accountMgr.StartPairing()
			case "list":
				clientsMu.Lock()
				fmt.Printf("\n--- Active Bot Accounts (%d) ---\n", len(activeClients))
				for id := range activeClients {
					fmt.Printf("  %s\n", id)
				}
				fmt.Println("--------------------------------")
				clientsMu.Unlock()
			case "ngrok on":
				startNgrok()
			case "ngrok off":
				stopNgrok()
			case "ngrok":
				if publicTunnel.Enabled() {
					fmt.Println("  [NGROK] Tunnel is ON")
				} else {
					fmt.Println("  [NGROK] Tunnel is OFF")
				}
			}
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	close(stopBg)
	pairQueue.Close()
	stopNgrok()
	fmt.Println("\nShutting down all WhatsApp clients gracefully...")
	clientsMu.Lock()
	for _, client := range activeClients {
		client.Disconnect()
	}
	clientsMu.Unlock()

	fmt.Println("Flushing and checkpointing SQLite WAL databases...")
	if err := chatStore.Close(); err != nil {
		fmt.Printf("⚠️ [SQLITE] webchat close error: %v\n", err)
	}
	if err := botStore.Close(); err != nil {
		fmt.Printf("⚠️ [SQLITE] botdata close error: %v\n", err)
	}
	if err := db.CheckpointAndClose(sessionDB); err != nil {
		fmt.Printf("⚠️ [SQLITE] session close error: %v\n", err)
	}
}
