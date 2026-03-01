# ADK Gemini Live API Toolkit Demo (Go)

A Go implementation of the real-time bidirectional streaming demo using the Gemini Live API via the Google GenAI Go SDK. This application showcases WebSocket-based communication with Gemini models, supporting multimodal requests (text, audio, and image/video input) and flexible responses (text or audio output).

![bidi-demo-screen](assets/bidi-demo-screen.png)

## Overview

This demo implements the complete bidirectional streaming lifecycle:

1. **Application Initialization**: Creates HTTP server with WebSocket endpoint
2. **Session Initialization**: Connects to Gemini Live API with `LiveConnectConfig` per WebSocket connection
3. **Bidirectional Streaming**: Concurrent upstream (client → Live API) and downstream (Live API → client) goroutines
4. **Graceful Termination**: Proper cleanup of Live API session and WebSocket connections

> **Note**: This demo uses the `google.golang.org/genai` SDK directly for Live API access, as the Go ADK (`google.golang.org/adk`) does not yet support `RunLive`/`LiveRequestQueue`. The architecture mirrors the Python ADK bidi-demo.

## Features

- **WebSocket Communication**: Real-time bidirectional streaming via `/ws/{user_id}/{session_id}`
- **Multimodal Requests**: Text, audio, and image/video input with automatic audio transcription
- **Flexible Responses**: Text or audio output, automatically determined based on model architecture
- **Session Resumption**: Reconnection support configured via `LiveConnectConfig`
- **Concurrent Goroutines**: Separate upstream/downstream goroutines for optimal performance
- **Interactive UI**: Web interface with event console for monitoring Live API events
- **Google Search Integration**: Agent configured with Google Search tool

## Architecture

The application follows the same concurrent task pattern as the Python ADK bidi-demo:

```
┌─────────────┐         ┌──────────────────┐         ┌─────────────┐
│             │         │                  │         │             │
│  WebSocket  │────────▶│  Go Server       │────────▶│  Gemini     │
│   Client    │         │  (goroutines)    │         │  Live API   │
│             │◀────────│                  │◀────────│             │
└─────────────┘         └──────────────────┘         └─────────────┘
  Upstream Task            WebSocket Proxy           Downstream Task
```

- **Upstream Goroutine**: Receives WebSocket messages and forwards to `session.SendClientContent()` or `session.SendRealtimeInput()`
- **Downstream Goroutine**: Receives `session.Receive()` messages and sends to WebSocket client

## Prerequisites

- Go 1.23 or higher
- Google API key (for Gemini Live API) or Google Cloud project (for Vertex AI Live API)

## Installation

### 1. Navigate to Demo Directory

```bash
cd go/agents/bidi-demo
```

### 2. Install Dependencies

```bash
go mod tidy
```

### 3. Configure Environment Variables

Create a `.env` file with your credentials:

```bash
# Choose your Live API platform
GOOGLE_GENAI_USE_VERTEXAI=FALSE

# For Gemini Live API (when GOOGLE_GENAI_USE_VERTEXAI=FALSE)
GOOGLE_API_KEY=your_api_key_here

# For Vertex AI Live API (when GOOGLE_GENAI_USE_VERTEXAI=TRUE)
# GOOGLE_CLOUD_PROJECT=your_project_id
# GOOGLE_CLOUD_LOCATION=us-central1

# Model selection (optional, defaults to native audio model)
# See "Supported Models" section below for available model names
DEMO_AGENT_MODEL=gemini-2.5-flash-native-audio-preview-12-2025
```

#### Getting API Credentials

**Gemini Live API:**
1. Visit [Google AI Studio](https://aistudio.google.com/apikey)
2. Create an API key
3. Set `GOOGLE_API_KEY` in `.env`

**Vertex AI Live API:**
1. Enable Vertex AI API in [Google Cloud Console](https://console.cloud.google.com)
2. Set up authentication using `gcloud auth application-default login`
3. Set `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` in `.env`
4. Set `GOOGLE_GENAI_USE_VERTEXAI=TRUE`

## Running the Demo

### Start the Server

```bash
go run .
```

Or build and run:

```bash
go build -o bidi-demo .
./bidi-demo
```

The server starts on port 8000 by default. Set the `PORT` environment variable to change it.

### Access the Application

Open your browser and navigate to:

```
http://localhost:8000
```

## Usage

### Text Mode

1. Type your message in the input field
2. Click "Send" or press Enter
3. Watch the event console for Live API events
4. Receive streamed responses in real-time

### Audio Mode

1. Click "Start Audio" to begin voice interaction
2. Speak into your microphone
3. Receive audio responses with real-time transcription
4. Click "Stop Audio" to end the audio session

## WebSocket API

### Endpoint

```
ws://localhost:8000/ws/{user_id}/{session_id}
```

**Path Parameters:**
- `user_id`: Unique identifier for the user
- `session_id`: Unique identifier for the session

**Query Parameters:**
- `proactivity`: Enable proactive audio (native audio models only)
- `affective_dialog`: Enable affective dialog (native audio models only)

### Message Format

**Client → Server (Text):**
```json
{
  "type": "text",
  "text": "Your message here"
}
```

**Client → Server (Image):**
```json
{
  "type": "image",
  "data": "base64_encoded_image_data",
  "mimeType": "image/jpeg"
}
```

**Client → Server (Audio):**
- Send raw binary frames (PCM audio, 16kHz, 16-bit)

**Server → Client:**
- JSON-encoded events compatible with the ADK Event format

## Project Structure

```
bidi-demo/
├── main.go              # Go server: WebSocket handler + Live API integration
├── go.mod               # Go module definition
├── go.sum               # Dependency checksums
├── .env                 # Environment configuration (not in git)
├── README.md            # This file
├── assets/              # Screenshots and documentation assets
└── static/              # Frontend files (shared with Python demo)
    ├── index.html       # Main UI
    ├── css/
    │   └── style.css    # Styling
    └── js/
        ├── app.js                    # Main application logic
        ├── audio-player.js           # Audio playback
        ├── audio-recorder.js         # Audio recording
        ├── pcm-player-processor.js   # Audio processing
        └── pcm-recorder-processor.js # Audio processing
```

## Configuration

### Supported Models

The demo supports any Gemini model compatible with the Live API:

**Native Audio Models** (recommended for voice):
- `gemini-2.5-flash-native-audio-preview-12-2025` (Gemini Live API)
- `gemini-live-2.5-flash-native-audio` (Vertex AI)

Set the model via `DEMO_AGENT_MODEL` in `.env`.

### Response Modality

The demo automatically configures response modality based on model architecture:

- **Native audio models** (containing "native-audio" in name): Uses `AUDIO` response modality with input/output audio transcription enabled
- **Half-cascade models** (other models): Uses `TEXT` response modality for better performance

## Troubleshooting

### Connection Issues

**Problem**: WebSocket fails to connect

**Solutions**:
- Verify API credentials in `.env`
- Check console for error messages
- Ensure the server is running on the correct port

### Audio Not Working

**Problem**: Audio input/output not functioning

**Solutions**:
- Grant microphone permissions in browser
- Verify browser supports Web Audio API
- Check that audio model is configured (native audio model required)
- Review browser console for errors

### Build Errors

**Problem**: `go mod tidy` fails

**Solutions**:
- Ensure Go 1.23+ is installed
- Check network connectivity for downloading modules
- Try `GOPROXY=https://proxy.golang.org go mod tidy`

## Additional Resources

- **GenAI Go SDK**: https://pkg.go.dev/google.golang.org/genai
- **Gemini Live API**: https://ai.google.dev/gemini-api/docs/live
- **Vertex AI Live API**: https://cloud.google.com/vertex-ai/generative-ai/docs/live-api
- **ADK Documentation**: https://google.github.io/adk-docs/
- **Python bidi-demo**: See `python/agents/bidi-demo/` for the reference implementation

## License

Apache 2.0 - See repository LICENSE file for details.
