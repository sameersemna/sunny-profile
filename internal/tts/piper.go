package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Engine is the TTS interface — returns WAV/MP3 bytes
type Engine interface {
	Synthesize(ctx context.Context, text string) ([]byte, string, error) // bytes, mimeType, err
}

// PiperEngine uses Piper TTS (local, fast, offline)
// Install: https://github.com/rhasspy/piper
// Models: https://huggingface.co/rhasspy/piper-voices
type PiperEngine struct {
	piperBin  string
	modelPath string
}

func NewPiperEngine(piperBin, modelPath string) *PiperEngine {
	if piperBin == "" { piperBin = "piper" }
	return &PiperEngine{piperBin: piperBin, modelPath: modelPath}
}

func (p *PiperEngine) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("sunny_%d.wav", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	cmd := exec.CommandContext(ctx, p.piperBin,
		"--model", p.modelPath,
		"--output_file", tmpFile,
	)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("piper: %w — %s", err, string(out))
	}
	data, err := os.ReadFile(tmpFile)
	return data, "audio/wav", err
}

// EdgeTTSEngine uses Microsoft Edge TTS via the edge-tts CLI (free, needs internet)
// Install: pip install edge-tts
type EdgeTTSEngine struct {
	voice string // e.g. "en-US-GuyNeural"
}

func NewEdgeTTSEngine(voice string) *EdgeTTSEngine {
	if voice == "" { voice = "en-US-GuyNeural" }
	return &EdgeTTSEngine{voice: voice}
}

func (e *EdgeTTSEngine) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("sunny_%d.mp3", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	cmd := exec.CommandContext(ctx, "edge-tts",
		"--voice", e.voice,
		"--text", text,
		"--write-media", tmpFile,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("edge-tts: %w — %s", err, string(out))
	}
	data, err := os.ReadFile(tmpFile)
	return data, "audio/mpeg", err
}

// TTSServer serves synthesized audio over HTTP so Sonos can play it
type TTSServer struct {
	engine   Engine
	dir      string
	baseURL  string
	mux      *http.ServeMux
}

func NewTTSServer(engine Engine, serveDir, baseURL string) *TTSServer {
	os.MkdirAll(serveDir, 0755)
	s := &TTSServer{engine: engine, dir: serveDir, baseURL: baseURL, mux: http.NewServeMux()}
	s.mux.HandleFunc("/tts/generate", s.generate)
	s.mux.Handle("/tts/audio/", http.StripPrefix("/tts/audio/", http.FileServer(http.Dir(serveDir))))
	return s
}

func (s *TTSServer) Handler() http.Handler { return s.mux }

func (s *TTSServer) generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "POST only", 405); return }
	var req struct{ Text string `json:"text"` }
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil || req.Text == "" {
		http.Error(w, "need json {text}", 400); return
	}

	audio, mime, err := s.engine.Synthesize(r.Context(), req.Text)
	if err != nil { http.Error(w, err.Error(), 500); return }

	ext := ".wav"
	if strings.Contains(mime, "mpeg") { ext = ".mp3" }
	fname := fmt.Sprintf("tts_%d%s", time.Now().UnixNano(), ext)
	os.WriteFile(filepath.Join(s.dir, fname), audio, 0644)
	cleanOldFiles(s.dir, 20)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"url":   s.baseURL + "/tts/audio/" + fname,
		"bytes": len(audio),
		"mime":  mime,
	})
}

func cleanOldFiles(dir string, keepLast int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keepLast { return }
	for _, e := range entries[:len(entries)-keepLast] {
		os.Remove(filepath.Join(dir, e.Name()))
	}
}
