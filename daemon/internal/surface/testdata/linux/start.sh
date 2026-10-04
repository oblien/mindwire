#!/bin/sh
set -eu
export DISPLAY=:99
Xtigervnc :99 -geometry 1600x1000 -depth 24 -SecurityTypes None \
    -AlwaysShared -rfbport 5900 -localhost no -nolisten tcp &
vnc_pid=$!
trap 'kill "$vnc_pid" 2>/dev/null || true' EXIT INT TERM
python3 /display.py
