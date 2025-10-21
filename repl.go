package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gorilla/websocket"
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
					eventChan <- fmt.Sprintf("[%s] %s: %v", namespace, msgType, payload)
				}
			}
		}
	}()

	// TODO: get full control of the user's terminal to avoid input clashed
	go func() {
		for event := range eventChan {
			fmt.Printf("🔔 %s\n", event)
		}
	}()

	fmt.Println("Type 'build' to trigger a build, 'exit' or 'quit' to leave the REPL.")

	// Main REPL loop
	for {

		fmt.Print("ftc> ")
		os.Stdout.Sync()
		os.Stderr.Sync()

		var input string
		_, err := fmt.Scan(&input)
		if err != nil {
			return fmt.Errorf("scanln: %w", err)
		}

		switch input {
		case "exit", "quit":
			fmt.Println("Goodbye!")
			return nil

		case "build":
			fmt.Println("🔨 Triggering build...")

			// Send build request via websocket
			err = conn.WriteJSON(onBotJavaEvent{
				Namespace: "ONBOTJAVA",
				Type:      "build:launch",
				Payload:   "",
			})
			if err != nil {
				fmt.Printf("❌ Error sending build command: %v\n", err)
			} else {
				fmt.Println("✅ Build command sent! Waiting for events...")
			}

			// Wait for events
			// TODO: spinner for long builds
			res, err := client.Get("http://" + *remoteAddress + "/java/build/wait")
			if err != nil {
				fmt.Printf("❌ Error waiting for build events: %v\n", err)
				continue
			}
			defer res.Body.Close()
			debugResponse(res)

			bits, err := io.ReadAll(res.Body)
			if err != nil {
				fmt.Printf("❌ Error reading build events: %v\n", err)
				continue
			}

			if len(bits) == 0 {
				fmt.Println("✅ Build succeeded with no output.")
				continue
			}
			fmt.Printf("📦 Build result:\n%s", string(bits))

		case "help":
			fmt.Println("Available commands:")
			fmt.Println("  build - Trigger a build")
			fmt.Println("  help  - Show this help")
			fmt.Println("  exit  - Exit the REPL")
			fmt.Println("  quit  - Exit the REPL")

		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", input)
		}
	}
}
