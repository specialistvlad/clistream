"""Loopback UDP path for measuring recovery with delay, packet loss and NAT rebinding."""

import heapq
import selectors
import socket
import threading
import time


class NetworkPath:
    def __init__(self, server, *, drop_first_peer=False):
        self.drop_first_peer = drop_first_peer
        self.first_peer = None
        self.server = ("127.0.0.1", server)
        self.front = self.bound()
        self.address = f"127.0.0.1:{self.front.getsockname()[1]}"
        self.stop = threading.Event()
        self.rebind = threading.Event()
        self.rebound = threading.Event()
        self.dropped = 0
        self.thread = threading.Thread(target=self.run, daemon=True)

    @staticmethod
    def bound():
        s = socket.socket(type=socket.SOCK_DGRAM)
        s.bind(("127.0.0.1", 0))
        return s

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.stop.set()
        self.thread.join(3)
        self.front.close()

    def run(self):
        upstream = self.bound()
        events = []
        peer = None
        count = 0
        with selectors.DefaultSelector() as sel:
            sel.register(self.front, selectors.EVENT_READ)
            sel.register(upstream, selectors.EVENT_READ)
            try:
                while not self.stop.is_set():
                    if self.rebind.is_set():
                        new = self.bound()
                        sel.unregister(upstream)
                        upstream.close()
                        upstream = new
                        sel.register(upstream, selectors.EVENT_READ)
                        self.rebind.clear()
                        self.rebound.set()
                    for key, _ in sel.select(0.004):
                        down = key.fileobj is upstream
                        packet, addr = key.fileobj.recvfrom(65535)
                        if not down:
                            if self.first_peer is None:
                                self.first_peer = addr
                            if self.drop_first_peer and addr == self.first_peer:
                                self.dropped += 1
                                continue
                            peer = addr
                        count += 1
                        if count % 23 == 0:
                            self.dropped += 1
                            continue
                        delay = 0.012 if count % 4 else 0.022
                        heapq.heappush(
                            events,
                            (time.monotonic() + delay, count, down, packet, peer),
                        )
                    while events and events[0][0] <= time.monotonic():
                        _, _, down, packet, dst = heapq.heappop(events)
                        if down:
                            self.front.sendto(packet, dst)
                        else:
                            upstream.sendto(packet, self.server)
            finally:
                upstream.close()
