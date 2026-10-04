FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    tigervnc-standalone-server python3-tk xfonts-base \
    && rm -rf /var/lib/apt/lists/*
COPY display.py /display.py
COPY start.sh /start.sh
EXPOSE 5900 8799
CMD ["/bin/sh", "/start.sh"]
