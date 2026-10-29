# Chatterbox Turbo for Usherr's Read-to-Me (turbo_server.py): Resemble AI's chatterbox-tts
# package (MIT), which brings Turbo, on PyTorch's own CUDA image. Its torch matches the one
# chatterbox-tts asks for, so it isn't downloaded twice.
FROM pytorch/pytorch:2.6.0-cuda12.4-cudnn9-runtime
# perth (the watermark library) needs pkg_resources: setuptools before 81 has it.
RUN pip install --no-cache-dir "chatterbox-tts==0.1.7" "setuptools<81" "fastapi==0.115.6" "uvicorn==0.34.0"
WORKDIR /app
COPY turbo_server.py /app/turbo_server.py
EXPOSE 4123
CMD ["python", "/app/turbo_server.py"]
