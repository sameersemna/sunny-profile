package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sunny-ai/sunny-profile/internal/audio"
	"github.com/sunny-ai/sunny-profile/internal/tts"
	"github.com/sunny-ai/sunny-profile/internal/wakeword"
)

var (
	ttsEngineFlag string
	piperBin      string
	piperModel    string
	brainURL      string
	whisperHost   string
	whisperPort   int
	wakePhrase    string
	sessionID     string
	modeFlag      string
	querySeconds  int
)

func main() {
	root := &cobra.Command{
		Use:   "sunny-audio",
		Short: "Sunny Audio — single-device wake word + local playback",
		RunE:  run,
	}
	root.Flags().StringVar(&ttsEngineFlag, "tts", "edge", "TTS engine: edge|piper")
	root.Flags().StringVar(&piperBin, "piper-bin", "piper", "Piper binary path")
	root.Flags().StringVar(&piperModel, "piper-model", "", "Piper voice .onnx model")
	root.Flags().StringVar(&brainURL, "brain", "http://127.0.0.1:8765", "Sunny brain URL")
	root.Flags().StringVar(&whisperHost, "whisper-host", "promaxgb10-6116", "Whisper host")
	root.Flags().IntVar(&whisperPort, "whisper-port", 8768, "Whisper port")
	root.Flags().StringVar(&wakePhrase, "wake", "hey sunny", "Wake phrase")
	root.Flags().StringVar(&sessionID, "session", "desktop-audio", "Session id sent to brain")
	root.Flags().StringVar(&modeFlag, "mode", "general", "Conversation mode sent to brain")
	root.Flags().IntVar(&querySeconds, "query-seconds", 8, "Seconds to capture after wake")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	engine, err := makeTTSEngine()
	if err != nil {
		return err
	}
	player := audio.NewPlayer()
	client := &http.Client{Timeout: 20 * time.Second}

	if err := speak(ctx, engine, player, "Sunny is online. Say hey sunny to activate me."); err != nil {
		fmt.Println("startup voice warning:", err)
	}

	var busy atomic.Bool
	onWake := func(wakeCtx context.Context) {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		defer busy.Store(false)

		fmt.Println("Wake word activated")
		_ = speak(wakeCtx, engine, player, "Yes, I am listening.")

		text, err := wakeword.ListenAndTranscribe(wakeCtx, querySeconds, whisperHost, whisperPort)
		if err != nil || strings.TrimSpace(text) == "" {
			_ = speak(wakeCtx, engine, player, "I did not catch that. Please try again.")
			return
		}

		fmt.Printf("Heard: %s\n", text)
		reply, err := sendTranscript(wakeCtx, client, brainURL, sessionID, modeFlag, text)
		if err != nil {
			fmt.Println("brain error:", err)
			_ = speak(wakeCtx, engine, player, "I heard you, but I could not reach the brain service.")
			return
		}

		if strings.TrimSpace(reply) == "" {
			reply = "Got it."
		}
		if err := speak(wakeCtx, engine, player, reply); err != nil {
			fmt.Println("playback error:", err)
		}
	}

	detector := wakeword.New(wakePhrase, whisperHost, whisperPort, onWake)
	fmt.Println("Sunny Audio ready")
	fmt.Printf("  Brain:    %s\n", brainURL)
	fmt.Printf("  Whisper:  %s:%d\n", whisperHost, whisperPort)
	fmt.Printf("  Wake:     %q\n", wakePhrase)
	fmt.Printf("  Session:  %s\n", sessionID)
	fmt.Println("Listening for wake word...")
	return detector.Run(ctx)
}

func makeTTSEngine() (tts.Engine, error) {
	switch strings.ToLower(ttsEngineFlag) {
	case "piper":
		if piperModel == "" {
			return nil, fmt.Errorf("--piper-model required for piper engine")
		}
		return tts.NewPiperEngine(piperBin, piperModel), nil
	case "edge":
		return tts.NewEdgeTTSEngine("en-US-GuyNeural"), nil
	default:
		return nil, fmt.Errorf("unsupported --tts value %q (use edge|piper)", ttsEngineFlag)
	}
}

func speak(ctx context.Context, engine tts.Engine, player *audio.Player, text string) error {
	audioBytes, mime, err := engine.Synthesize(ctx, text)
	if err != nil {
		return err
	}
	return player.PlayBytes(ctx, audioBytes, mime)
}

func sendTranscript(ctx context.Context, httpClient *http.Client, baseURL, session, mode, text string) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"session_id": session,
		"speaker":    "me",
		"text":       text,
		"mode":       mode,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/transcript", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return parseReplyText(body), nil
}

func parseReplyText(body []byte) string {
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	keys := []string{"reply", "response", "assistant", "text", "message"}
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
