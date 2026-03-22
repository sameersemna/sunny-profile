package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Player plays audio using the system's current default output device.
type Player struct{}

func NewPlayer() *Player { return &Player{} }

// PlayBytes writes audio data to a temporary file and plays it with best-effort fallbacks.
func (p *Player) PlayBytes(ctx context.Context, data []byte, mime string) error {
	ext := ".wav"
	if strings.Contains(strings.ToLower(mime), "mpeg") {
		ext = ".mp3"
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("sunny_play_%d%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	return p.PlayFile(ctx, tmpFile, mime)
}

// PlayFile plays an audio file through the OS-selected output route.
func (p *Player) PlayFile(ctx context.Context, filePath, mime string) error {
	var candidates [][]string
	mime = strings.ToLower(mime)

	if strings.Contains(mime, "mpeg") {
		candidates = [][]string{
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "error", filePath},
			{"mpv", "--no-video", "--really-quiet", filePath},
			{"mpg123", "-q", filePath},
			{"cvlc", "--play-and-exit", "--intf", "dummy", filePath},
		}
	} else {
		candidates = [][]string{
			{"paplay", filePath},
			{"aplay", filePath},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "error", filePath},
			{"mpv", "--no-video", "--really-quiet", filePath},
			{"cvlc", "--play-and-exit", "--intf", "dummy", filePath},
		}
	}

	var attempts []string
	for _, c := range candidates {
		if len(c) == 0 {
			continue
		}
		bin := c[0]
		if _, err := exec.LookPath(bin); err != nil {
			attempts = append(attempts, fmt.Sprintf("%s:not-found", bin))
			continue
		}
		cmd := exec.CommandContext(ctx, bin, c[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v (%s)", bin, err, strings.TrimSpace(string(out))))
			continue
		}
		return nil
	}

	return fmt.Errorf("no working audio player found (%s)", strings.Join(attempts, "; "))
}