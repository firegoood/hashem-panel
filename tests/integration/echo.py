#!/usr/bin/env python3
import socket
import sys
import threading

label = sys.argv[1].encode() + b':'
ports = [int(x) for x in sys.argv[2:]]

def tcp(port):
    s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(('0.0.0.0', port)); s.listen(1024)
    def client(c):
        with c:
            while data := c.recv(65536): c.sendall(label + data)
    while True:
        c, _ = s.accept(); threading.Thread(target=client, args=(c,), daemon=True).start()

def udp(port):
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.bind(('0.0.0.0', port))
    while True:
        data, addr = s.recvfrom(65536); s.sendto(label + data, addr)

for port in ports:
    threading.Thread(target=tcp, args=(port,), daemon=True).start()
    threading.Thread(target=udp, args=(port,), daemon=True).start()
threading.Event().wait()
