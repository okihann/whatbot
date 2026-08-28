package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"bot/ai"
	"bot/handlers"
	"bot/ngrok"
	"bot/commands/owner"
	_ "bot/commands/tool"

	_ "modernc.org/sqlite"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"google.golang.org/protobuf/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	activeClients = make(map[string]*whatsmeow.Client)
	clientsMu     sync.Mutex
	dbContainer   *sqlstore.Container
	botStartTime  time.Time
	publicTunnel  = &ngrok.Tunnel{}
	ngrokConfig   ngrok.Config
)

func setupClient(deviceStore *store.Device) {
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	if client.Store.DeviceProps != nil {
		client.Store.DeviceProps.Os = proto.String("Mac OS")
		client.Store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_SAFARI.Enum()
	}

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			fmt.Printf("\n✅ Bot %s is live and connected!\n", client.Store.ID)
		case *events.Message:
			if v.Info.Timestamp.Before(botStartTime) {
				return
			}
			owner.HandleAutomations(client, v)
			handlers.HandleCommand(client, v)
		}
	})

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			fmt.Println("Failed to connect:", err)
			return
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				config := qrterminal.Config{Level: qrterminal.L, Writer: os.Stdout, HalfBlocks: true}
				fmt.Println("\n=================================================")
				fmt.Println("Scan this QR code to link a new WhatsApp account:")
				fmt.Println("=================================================")
				qrterminal.GenerateWithConfig(evt.Code, config)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		err := client.Connect()
		if err != nil {
			fmt.Println("Failed to connect existing device:", err)
			return
		}
	}

	clientsMu.Lock()
	if client.Store.ID != nil {
		activeClients[client.Store.ID.String()] = client
	}
	clientsMu.Unlock()
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
	ai.LoadEnv()
	ngrokConfig = ngrok.LoadConfig(ai.AppConfig.ServerPort)
	go ai.StartServer()

	botStartTime = time.Now()
	handlers.SetStartTime(botStartTime)

	// NGROK_ENABLED controls startup. Credentials remain environment-only.
	startNgrok()

	dbLog := waLog.Stdout("Database", "INFO", true)
	dbPath := "db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Println("Database directory not found, creating...")
		if err := os.MkdirAll(dbPath, 0755); err != nil {
			panic(fmt.Sprintf("FATAL: Could not create database directory: %v", err))
		}
	}

	dbFilePath := filepath.Join(dbPath, "session.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=wal&_busy_timeout=5000", dbFilePath))
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1)

	dbContainer = sqlstore.NewWithDB(db, "sqlite", dbLog)
	err = dbContainer.Upgrade(context.Background())
	if err != nil {
		panic(fmt.Sprintf("FATAL: Failed to upgrade database schema: %v", err))
	}

	devices, err := dbContainer.GetAllDevices(context.Background())
	if err != nil {
		panic(err)
	}

	if len(devices) == 0 {
		fmt.Println("No linked accounts found. Starting pairing process...")
		setupClient(dbContainer.NewDevice())
	} else {
		fmt.Printf("Found %d saved accounts. Booting them up...\n", len(devices))
		for _, device := range devices {
			setupClient(device)
		}
	}

	fmt.Println("\n==============================================================")
	fmt.Println("Multi-Device Manager is Running. Press CTRL+C to exit.")
	fmt.Println("Commands you can type in this console:")
	fmt.Println(" - 'add'       : Generate a new QR code")
	fmt.Println(" - 'list'      : Show connected bot numbers")
	fmt.Println(" - 'ngrok on'  : Start the configured ngrok tunnel")
	fmt.Println(" - 'ngrok off' : Stop the ngrok tunnel")
	fmt.Println(" - 'ngrok'     : Show current ngrok status")
	fmt.Println("==============================================================")

	go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				text := strings.ToLower(strings.TrimSpace(scanner.Text()))

				// --- 1. NEW REMOVE LOGIC GOES HERE (Before the switch) ---
				if strings.HasPrefix(text, "remove ") {
					target := strings.TrimSpace(strings.TrimPrefix(text, "remove"))
					clientsMu.Lock()
					if client, ok := activeClients[target]; ok {
						client.Disconnect()
						client.Store.Delete(context.Background()) // <-- FIX: Added context.Background()
						delete(activeClients, target)
						fmt.Printf("  [SUCCESS] Wiped and removed bot account: %s\n", target)
					} else {
						fmt.Printf("  [ERROR] ID '%s' not found. Type 'list' to see valid IDs.\n", target)
					}
					clientsMu.Unlock()
					continue // Skip the switch below
				}

				// --- 2. EXISTING SWITCH LOGIC REMAINS UNCHANGED ---
				switch text {
				case "add":
					setupClient(dbContainer.NewDevice())
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

	stopNgrok()
	fmt.Println("\nShutting down all WhatsApp clients gracefully...")
	clientsMu.Lock()
	for _, client := range activeClients {
		client.Disconnect()
	}
	clientsMu.Unlock()
}
