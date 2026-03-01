// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main implements a WebSocket server demonstrating bidirectional
// streaming with the Gemini Live API using the Google GenAI Go SDK.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

const (
	appName   = "bidi-demo"
	agentName = "google_search_agent"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	// Load environment variables from .env file.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	mux := http.NewServeMux()

	// Serve index.html at root.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "static/index.html")
	})

	// Serve static assets.
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// WebSocket endpoint.
	mux.HandleFunc("/ws/", handleWebSocket)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Graceful shutdown.
	go func() {
		log.Printf("Starting %s server on :%s", appName, port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced shutdown: %v", err)
	}
	log.Println("Server exited")
}

// handleWebSocket handles WebSocket connections for bidirectional streaming.
// Path format: /ws/{user_id}/{session_id}
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Parse path parameters.
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/ws/"), "/")
	if len(parts) < 2 {
		http.Error(w, "invalid path: expected /ws/{user_id}/{session_id}", http.StatusBadRequest)
		return
	}
	userID := parts[0]
	sessionID := parts[1]

	// Parse query parameters for RunConfig options.
	proactivity := r.URL.Query().Get("proactivity") == "true"
	affectiveDialog := r.URL.Query().Get("affective_dialog") == "true"

	log.Printf("WebSocket connection request: user_id=%s, session_id=%s, proactivity=%v, affective_dialog=%v",
		userID, sessionID, proactivity, affectiveDialog)

	// Upgrade to WebSocket.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()
	log.Println("WebSocket connection accepted")

	// ========================================
	// Phase 2: Session Initialization
	// ========================================

	ctx := context.Background()

	// Create GenAI client (reads GOOGLE_API_KEY or Vertex AI config from env).
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		log.Printf("Failed to create GenAI client: %v", err)
		return
	}

	// Determine model name.
	modelName := os.Getenv("DEMO_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-2.5-flash-native-audio-preview-12-2025"
	}

	isNativeAudio := strings.Contains(strings.ToLower(modelName), "native-audio")

	// Build LiveConnectConfig with automatic modality detection.
	config := &genai.LiveConnectConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText("You are a helpful assistant that can search the web.")},
		},
		Tools:             []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}},
		SessionResumption: &genai.SessionResumptionConfig{},
	}

	if isNativeAudio {
		config.ResponseModalities = []genai.Modality{genai.ModalityAudio}
		config.InputAudioTranscription = &genai.AudioTranscriptionConfig{}
		config.OutputAudioTranscription = &genai.AudioTranscriptionConfig{}
		// TODO: proactivity and affective dialog may require additional
		// LiveConnectConfig fields depending on genai SDK version.
		log.Printf("Native audio model detected: %s, using AUDIO response modality, proactivity=%v, affective_dialog=%v",
			modelName, proactivity, affectiveDialog)
	} else {
		config.ResponseModalities = []genai.Modality{genai.ModalityText}
		log.Printf("Half-cascade model detected: %s, using TEXT response modality", modelName)
		if proactivity || affectiveDialog {
			log.Printf("Warning: proactivity and affective dialog are only supported on native audio models. These settings will be ignored.")
		}
	}

	// Connect to the Gemini Live API.
	session, err := client.Live.Connect(ctx, modelName, config)
	if err != nil {
		log.Printf("Failed to connect to Live API: %v", err)
		return
	}
	defer session.Close()
	log.Println("Connected to Live API")

	// ========================================
	// Phase 3: Active Session (concurrent bidirectional communication)
	// ========================================

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Downstream task: receive from Gemini Live API, send to browser WebSocket.
	wg.Add(1)
	go func() {
		defer wg.Done()
		downstreamTask(conn, session, done)
	}()

	// Upstream task: receive from browser WebSocket, send to Gemini Live API.
	upstreamTask(conn, session, done)

	// ========================================
	// Phase 4: Session Termination
	// ========================================

	close(done)
	wg.Wait()
	log.Println("Session terminated")
}

// upstreamTask receives messages from the browser WebSocket and forwards them
// to the Gemini Live API session.
func upstreamTask(conn *websocket.Conn, session *genai.Session, done chan struct{}) {
	log.Println("upstream_task started")
	for {
		select {
		case <-done:
			return
		default:
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Println("Client disconnected normally")
			} else {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			// Binary frames are raw PCM audio (16kHz, 16-bit).
			log.Printf("Received binary audio chunk: %d bytes", len(data))
			if err := session.SendRealtimeInput(genai.LiveRealtimeInput{
				Media: &genai.Blob{
					MIMEType: "audio/pcm;rate=16000",
					Data:     data,
				},
			}); err != nil {
				log.Printf("SendRealtimeInput audio error: %v", err)
				return
			}

		case websocket.TextMessage:
			// Text frames are JSON messages.
			log.Printf("Received text message: %.100s", data)

			var jsonMsg map[string]interface{}
			if err := json.Unmarshal(data, &jsonMsg); err != nil {
				log.Printf("Invalid JSON message: %v", err)
				continue
			}

			switch jsonMsg["type"] {
			case "text":
				text, _ := jsonMsg["text"].(string)
				log.Printf("Sending text content: %s", text)
				if err := session.SendClientContent(genai.LiveClientContentInput{
					Turns: []*genai.Content{{
						Parts: []*genai.Part{genai.NewPartFromText(text)},
						Role:  "user",
					}},
					TurnComplete: true,
				}); err != nil {
					log.Printf("SendClientContent error: %v", err)
					return
				}

			case "image":
				log.Println("Received image data")
				dataStr, _ := jsonMsg["data"].(string)
				imageData, err := base64.StdEncoding.DecodeString(dataStr)
				if err != nil {
					log.Printf("Base64 decode error: %v", err)
					continue
				}
				mimeType := "image/jpeg"
				if mt, ok := jsonMsg["mimeType"].(string); ok {
					mimeType = mt
				}
				log.Printf("Sending image: %d bytes, type: %s", len(imageData), mimeType)
				if err := session.SendRealtimeInput(genai.LiveRealtimeInput{
					Media: &genai.Blob{
						MIMEType: mimeType,
						Data:     imageData,
					},
				}); err != nil {
					log.Printf("SendRealtimeInput image error: %v", err)
					return
				}
			}
		}
	}
}

// downstreamTask receives LiveServerMessages from the Gemini Live API and
// forwards them as ADK-compatible event JSON to the browser WebSocket.
func downstreamTask(conn *websocket.Conn, session *genai.Session, done chan struct{}) {
	log.Println("downstream_task started")
	for {
		select {
		case <-done:
			return
		default:
		}

		msg, err := session.Receive()
		if err != nil {
			select {
			case <-done:
				return
			default:
			}
			log.Printf("Receive error: %v", err)
			return
		}

		// Convert LiveServerMessage to ADK-compatible event format.
		events := convertToFrontendEvents(msg)
		for _, evt := range events {
			data, err := json.Marshal(evt)
			if err != nil {
				log.Printf("JSON marshal error: %v", err)
				continue
			}
			log.Printf("[SERVER] Event: %s", truncate(string(data), 200))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		}
	}
}

// convertToFrontendEvents transforms a genai LiveServerMessage into one or more
// event maps that the bidi-demo frontend (app.js) can parse. The frontend
// expects events in the ADK Event format with top-level fields like
// "content", "turnComplete", "interrupted", "inputTranscription", etc.
//
// The conversion first marshals the LiveServerMessage to a raw JSON map to
// handle any genai SDK version differences gracefully, then extracts the
// fields the frontend expects.
func convertToFrontendEvents(msg *genai.LiveServerMessage) []map[string]interface{} {
	// Marshal to intermediate JSON map for flexible field access.
	raw := marshalToMap(msg)
	if raw == nil {
		return nil
	}

	var events []map[string]interface{}

	// Handle setup complete (no frontend event needed, just log).
	if _, ok := raw["setupComplete"]; ok {
		log.Println("Live API setup complete")
		return nil
	}

	// Handle server content (model responses).
	if sc, ok := raw["serverContent"].(map[string]interface{}); ok {
		// Model turn content (text or audio).
		if mt, ok := sc["modelTurn"].(map[string]interface{}); ok {
			if parts, ok := mt["parts"].([]interface{}); ok && len(parts) > 0 {
				events = append(events, map[string]interface{}{
					"content": map[string]interface{}{
						"parts": parts,
						"role":  "model",
					},
					"author":  agentName,
					"partial": true,
				})
			}
		}

		// Input transcription (user's spoken words).
		if it, ok := sc["inputTranscription"].(map[string]interface{}); ok {
			events = append(events, map[string]interface{}{
				"inputTranscription": it,
				"author":            agentName,
			})
		}

		// Output transcription (model's spoken words).
		if ot, ok := sc["outputTranscription"].(map[string]interface{}); ok {
			events = append(events, map[string]interface{}{
				"outputTranscription": ot,
				"author":             agentName,
			})
		}

		// Turn complete.
		if tc, ok := sc["turnComplete"].(bool); ok && tc {
			events = append(events, map[string]interface{}{
				"turnComplete": true,
				"author":       agentName,
			})
		}

		// Interrupted.
		if intr, ok := sc["interrupted"].(bool); ok && intr {
			events = append(events, map[string]interface{}{
				"interrupted": true,
				"author":      agentName,
			})
		}
	}

	// Handle tool calls. For GoogleSearch, the model executes the search
	// server-side, so we don't need to respond. Log it for visibility.
	if tc, ok := raw["toolCall"]; ok {
		log.Printf("Tool call received: %+v", tc)
	}

	// Handle usage metadata.
	if um, ok := raw["usageMetadata"].(map[string]interface{}); ok {
		events = append(events, map[string]interface{}{
			"usageMetadata": um,
			"author":        agentName,
		})
	}

	return events
}

// marshalToMap converts a struct to a map via JSON for flexible field access.
func marshalToMap(v interface{}) map[string]interface{} {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("marshalToMap: marshal error: %v", err)
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("marshalToMap: unmarshal error: %v", err)
		return nil
	}
	return m
}

// truncate returns a string truncated to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
