# Chatterbox TTS API as built from its source (tagged read-to-me-chatterbox-base), plus two things:
#
# 1. Its watermark library (perth) needs pkg_resources, which setuptools 81 and newer no longer have,
#    so the model fails to start ("'NoneType' object is not callable"). Older setuptools brings it back.
# 2. Usherr's speed-up (usherr_speed.py): the text-to-speech-token model works in bfloat16, about
#    550 MB less GPU memory for the same speed and sound (measured 2026-10-26). CB_SPEED in
#    docker-compose.yml chooses it (off, autocast, bf16).
#    It also lets Usherr choose a take: a request with an X-Usherr-Seed header is read with that
#    seed, so the same text comes out the same way again.
#
# To rebuild from scratch, see the top of docker-compose.yml (the chatterbox service).
FROM read-to-me-chatterbox-base:latest
RUN uv pip install "setuptools<81"
COPY usherr_speed.py /app/usherr_speed.py
RUN sed -i '1i import usherr_speed  # Usherr: bfloat16 speed-up' /app/main.py
