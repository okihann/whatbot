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

	"bot/handlers"
   	"bot/ai"
	_ "bot/commands/owner"
	_ "bot/commands/tool"

	_ "modernc.org/sqlite"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	// Store all active clients dynamically instead of a single global variable
	activeClients = make(map[string]*whatsmeow.Client)
	clientsMu     sync.Mutex
	dbContainer   *sqlstore.Container
	botStartTime  time.Time
)

// setupClient initializes the connection and event handlers for a specific device
func setupClient(deviceStore *store.Device) {
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Attach the event handler directly to THIS specific client
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			fmt.Printf("\n✅ Bot %s is live and connected!\n", client.Store.ID)
		case *events.Message:
			// Ignore old messages from before the bot booted up
			if v.Info.Timestamp.Before(botStartTime) {
				return
			}
			// Route the message to the handler, passing this specific client
			handlers.HandleCommand(client, v)
		}
	})

	if client.Store.ID == nil {
		// --- NEW DEVICE PAIRING FLOW ---
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			fmt.Println("Failed to connect:", err)
			return
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				config := qrterminal.Config{
					Level:      qrterminal.L,
					Writer:     os.Stdout,
					HalfBlocks: true,
				}
				fmt.Println("\n=================================================")
				fmt.Println("Scan this QR code to link a new WhatsApp account:")
				fmt.Println("=================================================")
				qrterminal.GenerateWithConfig(evt.Code, config)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		// --- EXISTING DEVICE RECONNECTION FLOW ---
		err := client.Connect()
		if err != nil {
			fmt.Println("Failed to connect existing device:", err)
			return
		}
	}

	// Add the successfully connected client to our global manager
	clientsMu.Lock()
	if client.Store.ID != nil {
		activeClients[client.Store.ID.String()] = client
	}
	clientsMu.Unlock()
}

func main() {
    ai.LoadEnv()
	go ai.StartServer()
	
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

	// Fetch ALL devices from the database, not just the first one
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
	fmt.Println(" - 'add'  : Generate a new QR code to link another phone number")
	fmt.Println(" - 'list' : Show all currently connected bot numbers")
	fmt.Println("==============================================================")

	// --- TERMINAL COMMAND LISTENER ---
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "add" {
				setupClient(dbContainer.NewDevice())
			} else if text == "list" {
				clientsMu.Lock()
				fmt.Printf("\n--- Active Bot Accounts (%d) ---\n", len(activeClients))
				for id := range activeClients {
					fmt.Printf("📱 %s\n", id)
				}
				fmt.Println("--------------------------------")
				clientsMu.Unlock()
			}
		}
	}()

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down all WhatsApp clients gracefully...")
	clientsMu.Lock()
	for _, client := range activeClients {
		client.Disconnect()
	}
	clientsMu.Unlock()
}