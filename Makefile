# ── sunny-profile Makefile ─────────────────────────────────────
.PHONY: all build profile sonos audio clean

BIN := ./bin

all: build

build: profile sonos audio

profile:
	@mkdir -p $(BIN)
	CGO_ENABLED=1 go build -ldflags="-s -w" -o $(BIN)/sunny-profile ./cmd/sunny-profile

sonos:
	@mkdir -p $(BIN)
	CGO_ENABLED=1 go build -ldflags="-s -w" -o $(BIN)/sunny-sonos   ./cmd/sunny-sonos

audio:
	@mkdir -p $(BIN)
	CGO_ENABLED=1 go build -ldflags="-s -w" -o $(BIN)/sunny-audio   ./cmd/sunny-audio

clean:
	rm -rf $(BIN)

# Ingest your profile
ingest:
	$(BIN)/sunny-profile ingest --profile profile.yaml

# Search test
search:
	$(BIN)/sunny-profile search "startup experience machine learning"

# Show stats
stats:
	$(BIN)/sunny-profile stats

# Start Sonos daemon (auto-discover Sonos)
run-sonos:
	$(BIN)/sunny-sonos --tts edge --whisper-host promaxgb10-6116

# Start Sonos daemon with known IP
run-sonos-ip:
	$(BIN)/sunny-sonos --sonos-ip $(SONOS_IP) --tts edge

# Start single-device audio client (HP mic + OS default speaker)
run-audio:
	$(BIN)/sunny-audio --tts edge --whisper-host promaxgb10-6116

# Install edge-tts (Python TTS for Sonos)
install-edge-tts:
	pip3 install edge-tts --break-system-packages

# Install Piper TTS (optional, higher quality, offline)
install-piper:
	@echo "Download Piper from: https://github.com/rhasspy/piper/releases"
	@echo "And a voice model from: https://huggingface.co/rhasspy/piper-voices"
	@echo "Recommended voice: en_US-lessac-medium"
