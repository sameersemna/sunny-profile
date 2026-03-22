# Sunny Audio Client — Single Device Setup Guide

## Architecture

```
HP laptop (single audio client)
  └── Sunny listens for "Hey Sunny" on HP mic
       → Whisper STT on promaxgb10-6116
       → transcript posted to Sunny brain
       → TTS generated locally (Piper or Edge-TTS)
       → audio played through OS default output device
          (speaker / earphones / bluetooth headset)
```

This is now the recommended setup. Input and output happen on the same machine.

---

## Step 1 — Install TTS

Option A — Edge TTS (recommended, free, high quality, needs internet):

```bash
pip3 install edge-tts --break-system-packages
edge-tts --list-voices | grep "en-US"
edge-tts --voice en-US-GuyNeural --text "Hello" --write-media /tmp/test.mp3
```

Option B — Piper TTS (offline, local):

```bash
wget https://github.com/rhasspy/piper/releases/latest/download/piper_linux_x86_64.tar.gz
tar -xzf piper_linux_x86_64.tar.gz
sudo cp piper/piper /usr/local/bin/

wget https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx
wget https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx.json
```

---

## Step 2 — Install local audio player tools

At least one of these must be installed so Sunny can play audio through your OS default output route:

```bash
sudo apt install ffmpeg mpv mpg123 vlc pulseaudio-utils alsa-utils
```

Sunny tries available players in this order:

- MP3: ffplay, mpv, mpg123, cvlc
- WAV: paplay, aplay, ffplay, mpv, cvlc

---

## Step 3 — Build and run

```bash
cd sunny-profile/
make build

# Edge TTS mode
./bin/sunny-audio --tts edge --brain http://127.0.0.1:8765 --whisper-host promaxgb10-6116

# Piper mode
./bin/sunny-audio --tts piper --piper-model ./en_US-lessac-medium.onnx --brain http://127.0.0.1:8765 --whisper-host promaxgb10-6116
```

When running, Sunny announces it is online. Say "Hey Sunny", then speak your query.

---

## Step 4 — Behavior

- Wake phrase: hey sunny
- After wake, Sunny captures a short query from the same mic
- Transcript is posted to the brain at /api/transcript
- If the API returns a reply-like text field, Sunny speaks it back
- If reply text is missing, Sunny confirms with "Got it."

Useful flags:

```bash
./bin/sunny-audio --wake "hey sunny" --query-seconds 10 --session hp-main --mode general
```

---

## Privacy note

- Microphone capture is local to your HP device
- STT runs on your Whisper host
- TTS is local with Piper or cloud-backed text-only with Edge-TTS
- Audio playback uses your current OS default output device
