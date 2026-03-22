package wakeword

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Detector listens for a wake word and fires a callback
type Detector struct {
	wakeWord    string
	onWake      func(ctx context.Context)
	whisperHost string
	whisperPort int
}

// New creates a wake word detector
func New(wakeWord, whisperHost string, whisperPort int, onWake func(ctx context.Context)) *Detector {
	if wakeWord == "" {
		wakeWord = "hey sunny"
	}
	return &Detector{
		wakeWord:    strings.ToLower(wakeWord),
		onWake:      onWake,
		whisperHost: whisperHost,
		whisperPort: whisperPort,
	}
}

// Run starts continuous wake word detection
func (d *Detector) Run(ctx context.Context) error {
	fmt.Printf("Wake word detector active — say \"%s\"\n", d.wakeWord)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			text, err := d.listenOnce(ctx, 2)
			if err != nil {
				continue
			}
			if d.matches(text) {
				fmt.Printf("Wake word detected: %q\n", text)
				go d.onWake(ctx)
			}
		}
	}
}

func (d *Detector) listenOnce(ctx context.Context, durationSec int) (string, error) {
	return ListenAndTranscribe(ctx, durationSec, d.whisperHost, d.whisperPort)
}

// ListenAndTranscribe records one chunk from the default microphone and returns Whisper text.
func ListenAndTranscribe(ctx context.Context, durationSec int, whisperHost string, whisperPort int) (string, error) {
	tmpFile := fmt.Sprintf("/tmp/sunny_wake_%d.wav", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	var cmd *exec.Cmd
	if commandExists("parec") {
		// PulseAudio/PipeWire
		cmd = exec.CommandContext(ctx, "bash", "-c",
			fmt.Sprintf("parec --rate=16000 --channels=1 --format=s16le 2>/dev/null | dd bs=%d count=1 of=%s 2>/dev/null",
				16000*2*durationSec, tmpFile))
	} else {
		// ALSA
		cmd = exec.CommandContext(ctx, "arecord",
			"-r", "16000", "-c", "1", "-f", "S16_LE",
			"-d", fmt.Sprintf("%d", durationSec), tmpFile)
	}
	if err := cmd.Run(); err != nil {
		return "", err
	}

	info, err := os.Stat(tmpFile)
	if err != nil || info.Size() < 1000 {
		return "", fmt.Errorf("no audio captured")
	}

	return transcribeFile(ctx, whisperHost, whisperPort, tmpFile)
}

func (d *Detector) transcribeFile(ctx context.Context, filePath string) (string, error) {
	return transcribeFile(ctx, d.whisperHost, d.whisperPort, filePath)
}

func transcribeFile(ctx context.Context, whisperHost string, whisperPort int, filePath string) (string, error) {
	out, err := exec.CommandContext(ctx, "curl", "-s",
		"-F", fmt.Sprintf("file=@%s;type=audio/wav", filePath),
		"-F", "language=en",
		"-F", "response_format=json",
		fmt.Sprintf("http://%s:%d/inference", whisperHost, whisperPort),
	).Output()
	if err != nil {
		return "", err
	}
	var resp struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(out, &resp) == nil {
		return strings.TrimSpace(resp.Text), nil
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *Detector) matches(text string) bool {
	low := strings.ToLower(text)
	variants := []string{d.wakeWord, "hey sonny", "a sunny", "ay sunny", "hey son"}
	for _, v := range variants {
		if strings.Contains(low, v) {
			return true
		}
	}
	return false
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
