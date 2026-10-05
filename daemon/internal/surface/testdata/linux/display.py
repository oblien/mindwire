"""Disposable Linux desktop: records only synthetic test-window input."""
import json
import threading
import time
import tkinter as tk
from http.server import BaseHTTPRequestHandler, HTTPServer

for attempt in range(100):
    try:
        root = tk.Tk()
        break
    except tk.TclError:
        time.sleep(0.05)
else:
    raise RuntimeError("Linux test display did not start")

root.title("Mindwire Linux desktop test")
root.geometry("1600x1000+0+0")
root.overrideredirect(True)
root.configure(background="#191919")
text = tk.Text(root, font=("monospace", 20), background="#252525", foreground="white")
text.place(x=100, y=100, width=1200, height=700)
text.focus_force()
for x, label, cursor in [(100, "Clickable", "hand2"), (500, "Busy", "watch"), (900, "Resize", "sb_h_double_arrow")]:
    target = tk.Label(root, text=label, cursor=cursor, font=("monospace", 20), background="#323232", foreground="white")
    target.place(x=x, y=840, width=280, height=100)
lock = threading.Lock()
state = {"fixture": "mindwire-linux-desktop-v1", "ready": False, "pointer": [0, 0], "presses": [], "releases": [],
         "pressPositions": [], "releasePositions": [], "keys": [], "text": ""}


def pointer(event):
    with lock:
        state["pointer"] = [event.x_root, event.y_root]
        if event.type == tk.EventType.ButtonPress:
            state["presses"].append(event.num)
            state["pressPositions"].append([event.num, event.x_root, event.y_root])
        elif event.type == tk.EventType.ButtonRelease:
            state["releases"].append(event.num)
            state["releasePositions"].append([event.num, event.x_root, event.y_root])


def key(event):
    with lock:
        state["keys"].append(event.keysym)


def update():
    with lock:
        state["ready"] = True
        state["text"] = text.get("1.0", "end-1c")
    root.after(10, update)


root.bind_all("<Motion>", pointer)
root.bind_all("<ButtonPress>", pointer)
root.bind_all("<ButtonRelease>", pointer)
root.bind_all("<KeyPress>", key)
root.after(100, update)


class StateHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        with lock:
            data = json.dumps(state).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *_):
        pass


server = HTTPServer(("0.0.0.0", 8799), StateHandler)
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    root.mainloop()
finally:
    server.shutdown()
