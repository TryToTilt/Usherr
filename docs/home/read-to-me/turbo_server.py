"""Chatterbox Turbo for Usherr's Read-to-Me.

Resemble AI's Chatterbox Turbo (chatterbox-tts, MIT) behind the same small
web API the Chatterbox TTS API offers and Usherr uses, so either one can be
running on port 4123:

  GET  /health            {"status", "model_loaded"}
  GET  /voices            {"voices": [{"name"}]}: the copied voices, files in /voices
  POST /voices            a form with voice_name and voice_file: a copied voice (in
                          place of one by that name)
  DELETE /voices/{name}   deletes it
  POST /v1/audio/speech   {"input", "voice"?, "temperature"?, "seed"?} -> WAV

Turbo reads one piece at a time (one graphics card). Its built-in voice is the
default; a copied voice is a short clean sample in /voices (name.wav, .mp3 or
.flac), read once and remembered.
"""
import io
import os
import re
import threading
import traceback

import torch
import torchaudio
import uvicorn
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import Response
from pydantic import BaseModel

VOICES = os.environ.get("VOICE_LIBRARY_DIR", "/voices")
DEVICE = os.environ.get("DEVICE", "cuda")
SPEED = os.environ.get("CB_SPEED", "off")  # "bf16": the token model in bfloat16

state = {"model": None, "default": None, "error": ""}
lock = threading.Lock()
voice_conds = {}  # sample file -> its conditionals


def load():
    try:
        from chatterbox.tts_turbo import ChatterboxTurboTTS

        m = ChatterboxTurboTTS.from_pretrained(device=DEVICE)
        if SPEED == "bf16":
            m.t3.to(dtype=torch.bfloat16)
        state["default"] = m.conds
        state["model"] = m
        print(f"turbo: ready on {DEVICE} (speed-up: {SPEED})", flush=True)
    except Exception as e:  # noqa: BLE001
        state["error"] = str(e)
        traceback.print_exc()


threading.Thread(target=load, daemon=True).start()
app = FastAPI(title="Chatterbox Turbo for Usherr")

SAFE = re.compile(r"^[A-Za-z0-9 _.-]{1,60}$")


def voice_files():
    out = {}
    try:
        for f in sorted(os.listdir(VOICES)):
            stem, ext = os.path.splitext(f)
            if ext.lower() in (".wav", ".mp3", ".flac") and SAFE.match(stem):
                out[stem] = os.path.join(VOICES, f)
    except OSError:
        pass
    return out


@app.get("/health")
def health():
    if state["model"] is not None:
        return {"status": "healthy", "model_loaded": True, "model": "turbo"}
    return {"status": "error" if state["error"] else "loading", "model_loaded": False, "model": "turbo", "error": state["error"]}


@app.get("/voices")
def voices():
    return {"voices": [{"name": n} for n in voice_files()]}


@app.post("/voices", status_code=201)
async def add_voice(voice_name: str = Form(...), voice_file: UploadFile = File(...), language: str = Form("en")):
    if not SAFE.match(voice_name) or voice_name == "default":
        raise HTTPException(400, "That name can't be used.")
    ext = os.path.splitext(voice_file.filename or "")[1].lower()
    if ext not in (".wav", ".mp3", ".flac"):
        raise HTTPException(400, "A voice is a WAV, MP3 or FLAC file.")
    data = await voice_file.read()
    if not data or len(data) > 25 << 20:
        raise HTTPException(400, "A voice is a short recording (up to 25 MB).")
    old = voice_files().get(voice_name)
    path = os.path.join(VOICES, voice_name + ext)
    with open(path, "wb") as f:
        f.write(data)
    with lock:
        voice_conds.pop(path, None)
        if old and old != path:
            voice_conds.pop(old, None)
            os.remove(old)
    return {"name": voice_name}


@app.delete("/voices/{name}")
def delete_voice(name: str):
    path = voice_files().get(name)
    if not path:
        raise HTTPException(404, "That voice isn't there.")
    with lock:
        voice_conds.pop(path, None)
        os.remove(path)
    return {"deleted": name}


class Speech(BaseModel):
    input: str
    voice: str | None = None
    temperature: float | None = None
    seed: int | None = None


@app.post("/v1/audio/speech")
def speech(req: Speech):
    m = state["model"]
    if m is None:
        raise HTTPException(503, "The model is still loading." if not state["error"] else state["error"])
    text = req.input.strip()
    if not text or len(text) > 1000:
        raise HTTPException(400, "The text is 1 to 1000 characters.")
    sample = None
    if req.voice and req.voice != "default":
        sample = voice_files().get(req.voice)
        if not sample:
            raise HTTPException(404, "That voice isn't there.")
    temperature = req.temperature if req.temperature is not None else 0.8
    if not 0.05 <= temperature <= 2.0:
        raise HTTPException(400, "The temperature is from 0.05 to 2.")
    with lock:
        if sample:
            if sample not in voice_conds:
                m.prepare_conditionals(sample)
                voice_conds[sample] = m.conds
            m.conds = voice_conds[sample]
        else:
            m.conds = state["default"]
        if req.seed is not None:
            torch.manual_seed(req.seed)
        with torch.inference_mode(), torch.autocast("cuda", dtype=torch.bfloat16, enabled=SPEED == "bf16"):
            wav = m.generate(text, temperature=temperature)
    buf = io.BytesIO()
    torchaudio.save(buf, wav.float().clamp(-1, 1).cpu(), m.sr, format="wav", encoding="PCM_S", bits_per_sample=16)  # 16-bit, like the Chatterbox TTS API
    return Response(buf.getvalue(), media_type="audio/wav")


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=int(os.environ.get("PORT", "4123")))
