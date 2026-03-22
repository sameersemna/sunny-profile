#!/usr/bin/env python3
"""
Raspberry Pi Zero 2W — Sunny room mic
Records audio, detects wake word via Whisper, posts to Sunny brain.
Run: python3 pi_listener.py --brain http://macbook.local:8765
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile
import time
import wave
import struct
import urllib.request

def record_chunk(duration_sec=2, rate=16000, channels=1):
    """Record a chunk of audio, return raw PCM bytes."""
    tmp = tempfile.mktemp(suffix=".wav")
    cmd = ["arecord", "-r", str(rate), "-c", str(channels),
           "-f", "S16_LE", "-d", str(duration_sec), tmp]
    try:
        subprocess.run(cmd, capture_output=True, check=True, timeout=duration_sec+3)
        with open(tmp, "rb") as f:
            return f.read()
    except Exception as e:
        print(f"record error: {e}")
        return None
    finally:
        try: os.remove(tmp)
        except: pass

def whisper_transcribe(wav_bytes, whisper_host, whisper_port):
    """Send WAV to whisper.cpp server and get text."""
    import urllib.request, urllib.parse
    tmp = tempfile.mktemp(suffix=".wav")
    try:
        with open(tmp, "wb") as f:
            f.write(wav_bytes)

        boundary = "----SunnyBoundary"
        with open(tmp, "rb") as f:
            audio_data = f.read()

        body = (
            f"--{boundary}
"
            f"Content-Disposition: form-data; name=\"file\"; filename=\"audio.wav\"
"
            f"Content-Type: audio/wav

"
        ).encode() + audio_data + f"
--{boundary}--
".encode()

        url = f"http://{whisper_host}:{whisper_port}/inference"
        req = urllib.request.Request(url, data=body,
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            result = json.loads(resp.read())
            return result.get("text", "").strip()
    except Exception as e:
        return ""
    finally:
        try: os.remove(tmp)
        except: pass

def post_to_brain(brain_url, session_id, text, mode="general"):
    """Post a transcript to the Sunny brain."""
    payload = json.dumps({
        "session_id": session_id,
        "speaker": "me",
        "text": text,
        "mode": mode,
    }).encode()
    req = urllib.request.Request(
        brain_url + "/api/transcript", data=payload,
        headers={"Content-Type": "application/json"}
    )
    try:
        with urllib.request.urlopen(req, timeout=5):
            pass
        return True
    except Exception as e:
        print(f"brain post error: {e}")
        return False

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--brain", default="http://127.0.0.1:8765")
    ap.add_argument("--whisper-host", default="promaxgb10-6116")
    ap.add_argument("--whisper-port", type=int, default=8768)
    ap.add_argument("--mode", default="general")
    ap.add_argument("--wake-word", default="hey sunny")
    ap.add_argument("--session", default="pi-room")
    args = ap.parse_args()

    print(f"☀  Sunny Pi Listener")
    print(f"   Brain:    {args.brain}")
    print(f"   Whisper:  {args.whisper_host}:{args.whisper_port}")
    print(f"   Wake:     \"{args.wake_word}\"")
    print()

    in_session = False
    session_timeout = 0

    while True:
        wav = record_chunk(duration_sec=2)
        if not wav:
            time.sleep(0.5)
            continue

        text = whisper_transcribe(wav, args.whisper_host, args.whisper_port)
        if not text:
            continue

        low = text.lower()
        print(f"[heard] {text}")

        # Wake word detection
        if args.wake_word in low or "hey sonny" in low or "a sunny" in low:
            print("✨ Wake word! Starting 30s session...")
            in_session = True
            session_timeout = time.time() + 30
            continue

        # In active session: forward everything to brain
        if in_session:
            if time.time() < session_timeout:
                print(f"→ brain: {text}")
                post_to_brain(args.brain, args.session, text, args.mode)
            else:
                print("Session timeout")
                in_session = False

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nStopped")
