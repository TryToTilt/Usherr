"""Usherr's speed-up for Chatterbox: the text-to-speech-token model (T3, the
slow part, one token at a time) works in bfloat16, which the RTX 3070 does
natively. The voice decoder (S3Gen) is left as it is, so the sound doesn't
change.

The mode is CB_SPEED in the environment (or /app/usherr_mode, for trying
things without recreating the container):
  off       as Chatterbox ships
  autocast  T3's maths in bfloat16, its weights kept as they are
  bf16      T3's weights in bfloat16 as well (about half its memory)

It also gives the API a seed: a request with an X-Usherr-Seed header is read
with that seed, so the same text in the same voice comes out the same way
again (a remake, an A/B). Usherr sends one piece at a time, so the seed set
for a request is the one its reading uses.
"""
import os

def _mode():
    try:
        with open("/app/usherr_mode") as f:
            return f.read().strip()
    except OSError:
        return os.environ.get("CB_SPEED", "off").strip()

MODE = _mode()

if MODE in ("autocast", "bf16"):
    import torch
    from chatterbox.models.t3 import t3 as _t3

    _inference = _t3.T3.inference
    _cast = {"done": False}

    def inference(self, *args, **kwargs):
        if MODE == "bf16" and not _cast["done"]:
            self.to(dtype=torch.bfloat16)
            _cast["done"] = True
        with torch.autocast("cuda", dtype=torch.bfloat16):
            return _inference(self, *args, **kwargs)

    _t3.T3.inference = inference
    print(f"usherr: Chatterbox speed-up on ({MODE})", flush=True)
else:
    print("usherr: Chatterbox speed-up off", flush=True)


# The seed (X-Usherr-Seed): the API is a FastAPI app, made after this is imported.
try:
    import fastapi

    _fastapi_init = fastapi.FastAPI.__init__

    def _init_with_seed(self, *args, **kwargs):
        _fastapi_init(self, *args, **kwargs)

        @self.middleware("http")
        async def usherr_seed(request, call_next):
            seed = request.headers.get("x-usherr-seed", "")
            if seed.isdigit():
                import torch

                torch.manual_seed(int(seed))
            return await call_next(request)

    fastapi.FastAPI.__init__ = _init_with_seed
    print("usherr: seeds on (X-Usherr-Seed)", flush=True)
except Exception as e:  # noqa: BLE001
    print(f"usherr: no seeds ({e})", flush=True)
