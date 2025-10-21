package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

type onBotJavaEvent struct {
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
}

func repl([]string) error {
	// WARNING: this is on a separate PORT than the main HTTP server
	websocketURL := "ws://192.168.49.1:8081"

	// Connect to websocket
	fmt.Printf("Connecting to %s...\n", websocketURL)
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Subscribe to ONBOTJAVA events
	err = conn.WriteJSON(onBotJavaEvent{
		Namespace: "system",
		Type:      "subscribeToNamespace",
		Payload:   "ONBOTJAVA",
	})
	if err != nil {
		return fmt.Errorf("write json: %w", err)
	}

	fmt.Println("Connected! Subscribed to ONBOTJAVA events.")

	// Set up terminal in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set terminal to raw mode: %w", err)
	}
	defer func() {
		term.Restore(int(os.Stdin.Fd()), oldState)
		fmt.Println() // Add newline when exiting
	}()

	// Terminal control variables
	inputBuffer := ""
	prompt := "ftc> "

	// Start goroutine to listen for websocket events
	eventChan := make(chan string, 10)
	go func() {
		for {
			var message map[string]interface{}
			err := conn.ReadJSON(&message)
			if err != nil {
				eventChan <- fmt.Sprintf("WebSocket error: %v", err)
				return
			}

			// Pretty print the message
			if namespace, ok := message["namespace"].(string); ok && namespace == "ONBOTJAVA" {
				if msgType, ok := message["type"].(string); ok {
					payload := message["payload"]
					eventChan <- fmt.Sprintf("🔔 [%s] %s: %v", namespace, msgType, payload)
				}
			}
		}
	}()

	// Helper function to clear current line and rewrite it
	clearLine := func() {
		fmt.Print("\r\033[K") // Move to beginning of line and clear it
	}

	redrawPrompt := func() {
		fmt.Print(prompt + inputBuffer)
	}

	// Helper function to handle events
	handleEvent := func(event string) {
		clearLine()
		fmt.Printf("%s\r\n", event)
		redrawPrompt()
	}

	fmt.Print("Type 'build' to trigger a build, 'exit' or 'quit' to leave the REPL.\r\n")
	fmt.Print(prompt)

	// Create a channel for stdin input to make it non-blocking
	stdinChan := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			stdinChan <- buf[0]
		}
	}()

	// Main input loop with proper event handling
	for {
		select {
		case event := <-eventChan:
			handleEvent(event)

		case char := <-stdinChan:
			switch char {
			case '\r', '\n': // Enter key
				fmt.Print("\r\n") // Proper newline in raw mode (carriage return + line feed)

				command := strings.TrimSpace(inputBuffer)
				inputBuffer = ""

				if command == "" {
					fmt.Print(prompt)
					continue
				}

				err := handleCommand(conn, command, eventChan)
				if err == io.EOF {
					return nil // Exit requested
				}
				if err != nil {
					fmt.Printf("Error: %v\r\n", err)
				}

				fmt.Print(prompt)

			case 127, 8: // Backspace/Delete
				if len(inputBuffer) > 0 {
					inputBuffer = inputBuffer[:len(inputBuffer)-1]
					clearLine()
					redrawPrompt()
				}

			case 3: // Ctrl+C
				fmt.Println()
				return nil

			default:
				// Regular character
				if char >= 32 && char < 127 { // Printable ASCII
					inputBuffer += string(char)
					fmt.Print(string(char))
				}
			}

		case <-time.After(50 * time.Millisecond):
			// Small timeout to prevent busy waiting
			continue
		}
	}
}

// handleCommand processes user commands and returns io.EOF for exit
func handleCommand(conn *websocket.Conn, command string, eventChan chan<- string) error {
	switch command {
	case "exit", "quit":
		fmt.Print("Goodbye!")
		return io.EOF

	case "build":
		fmt.Print("🔨 Triggering build...\r\n")

		// Send build request via websocket
		err := conn.WriteJSON(onBotJavaEvent{
			Namespace: "ONBOTJAVA",
			Type:      "build:launch",
			Payload:   "",
		})
		if err != nil {
			fmt.Printf("❌ Error sending build command: %v\r\n", err)
			return nil
		}

		fmt.Print("✅ Build command sent! Waiting for events...\r\n")

		// Wait for events
		go watchBuild(eventChan)

	case "help":
		fmt.Println("Available commands:\r")
		fmt.Println("  build - Trigger a build\r")
		fmt.Println("  help  - Show this help\r")
		fmt.Println("  exit  - Exit the REPL\r")
		fmt.Println("  quit  - Exit the REPL\r")

	default:
		fmt.Printf("Unknown command: %s (type 'help' for available commands)\r\n", command)
	}

	return nil
}

func watchBuild(eventChan chan<- string) {
	res, err := client.Get("http://" + *remoteAddress + "/java/build/wait")
	if err != nil {
		eventChan <- fmt.Sprintf("❌ Error waiting for build events: %v", err)
		return
	}
	defer res.Body.Close()

	bits, err := io.ReadAll(res.Body)
	if err != nil {
		eventChan <- fmt.Sprintf("❌ Error reading build events: %v", err)
		return
	}

	if len(bits) == 0 {
		eventChan <- "✅ Build succeeded with no output."
	} else {
		eventChan <- fmt.Sprintf("📦 Build result:\r\n%s", string(bits))
	}
}
