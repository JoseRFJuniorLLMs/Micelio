"""TCP client for connecting to Micelio nodes.

Uses the same length-prefixed binary framing as the Go implementation:
4 bytes big-endian uint32 length + JSON payload bytes.
"""

import logging
import socket
import struct
import threading
import time
from typing import Optional

from micelio.protocol import Message, decode_message


# Maximum message size (1 MB, same as Go handler)
MAX_MESSAGE_SIZE = 1 << 20

# Default read timeout in seconds
DEFAULT_TIMEOUT = 30.0

# Default reconnection settings
DEFAULT_MAX_RETRIES = 0  # 0 = infinite retries
DEFAULT_RECONNECT_BASE_WAIT = 1.0  # seconds
DEFAULT_RECONNECT_MAX_WAIT = 30.0  # seconds
DEFAULT_HEALTH_CHECK_INTERVAL = 15.0  # seconds

logger = logging.getLogger("micelio.client")


class MicelioClient:
    """TCP client for the AIP protocol.

    Connects to a Micelio node and exchanges length-prefixed JSON messages.
    Wire-compatible with the Go network handler.

    Supports auto-reconnection with exponential backoff and periodic
    health checks (ping/pong).
    """

    def __init__(
        self,
        timeout: float = DEFAULT_TIMEOUT,
        auto_reconnect: bool = False,
        max_retries: int = DEFAULT_MAX_RETRIES,
        reconnect_base_wait: float = DEFAULT_RECONNECT_BASE_WAIT,
        reconnect_max_wait: float = DEFAULT_RECONNECT_MAX_WAIT,
        health_check_interval: float = DEFAULT_HEALTH_CHECK_INTERVAL,
    ):
        self._sock: Optional[socket.socket] = None
        self._timeout = timeout
        self._host: Optional[str] = None
        self._port: Optional[int] = None
        self._lock = threading.Lock()

        # Reconnection settings.
        self._auto_reconnect = auto_reconnect
        self._max_retries = max_retries
        self._reconnect_base_wait = reconnect_base_wait
        self._reconnect_max_wait = reconnect_max_wait
        self._health_check_interval = health_check_interval
        self._reconnecting = False
        self._closed = False  # True after explicit close()

        # Health check thread.
        self._health_thread: Optional[threading.Thread] = None
        self._health_stop = threading.Event()

    def connect(self, host: str, port: int) -> None:
        """Connect to a Micelio node via TCP."""
        self._host = host
        self._port = port
        self._closed = False
        self._connect_once()

        if self._auto_reconnect and self._health_check_interval > 0:
            self._start_health_check()

    def _connect_once(self) -> None:
        """Establish a single TCP connection."""
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(self._timeout)
        sock.connect((self._host, self._port))
        with self._lock:
            self._sock = sock
        logger.info("connected to %s:%d", self._host, self._port)

    def reconnect(self) -> bool:
        """Attempt to reconnect with exponential backoff.

        Returns True if reconnection succeeded, False otherwise.
        """
        if self._host is None or self._port is None:
            logger.error("cannot reconnect: no previous connection")
            return False

        if self._closed:
            return False

        # Prevent multiple concurrent reconnection attempts.
        with self._lock:
            if self._reconnecting:
                return False
            self._reconnecting = True

        try:
            return self._reconnect_loop()
        finally:
            with self._lock:
                self._reconnecting = False

    def _reconnect_loop(self) -> bool:
        attempt = 0
        while not self._closed:
            if self._max_retries > 0 and attempt >= self._max_retries:
                logger.error(
                    "reconnection failed after %d attempts", attempt
                )
                return False

            # Exponential backoff: base * 2^attempt, capped at max.
            delay = min(
                self._reconnect_base_wait * (2 ** attempt),
                self._reconnect_max_wait,
            )
            logger.info(
                "reconnecting to %s:%d (attempt %d, backoff %.1fs)",
                self._host, self._port, attempt + 1, delay,
            )
            time.sleep(delay)

            if self._closed:
                return False

            try:
                # Close old socket if still open.
                with self._lock:
                    if self._sock is not None:
                        try:
                            self._sock.close()
                        except OSError:
                            pass
                        self._sock = None

                self._connect_once()
                logger.info("reconnected to %s:%d", self._host, self._port)
                return True
            except (OSError, ConnectionError) as exc:
                logger.warning("reconnect attempt %d failed: %s", attempt + 1, exc)
                attempt += 1

        return False

    def _start_health_check(self) -> None:
        """Start a background thread that periodically checks the connection."""
        self._health_stop.clear()
        self._health_thread = threading.Thread(
            target=self._health_check_loop,
            daemon=True,
            name="micelio-health-check",
        )
        self._health_thread.start()

    def _health_check_loop(self) -> None:
        while not self._health_stop.is_set():
            self._health_stop.wait(self._health_check_interval)
            if self._health_stop.is_set() or self._closed:
                return

            if not self.ping():
                logger.warning("health check failed, attempting reconnect")
                self.reconnect()

    def ping(self) -> bool:
        """Check if the connection is still alive.

        Sends a zero-length probe using socket-level keepalive. Returns
        True if the connection appears healthy, False otherwise.
        """
        with self._lock:
            if self._sock is None:
                return False
            try:
                # Use a non-destructive check: MSG_PEEK with a tiny recv to
                # detect if the remote end closed the connection.
                self._sock.setblocking(False)
                try:
                    data = self._sock.recv(1, socket.MSG_PEEK)
                    # If recv returns b'' the remote closed the connection.
                    if data == b"":
                        return False
                except BlockingIOError:
                    # No data available -- connection is still alive.
                    pass
                except (OSError, ConnectionError):
                    return False
                finally:
                    self._sock.settimeout(self._timeout)
                return True
            except Exception:
                return False

    def send_message(self, msg: Message) -> None:
        """Send a message using length-prefixed binary framing.

        Format: 4 bytes big-endian uint32 (length) + JSON bytes.
        Matches the Go SendDirect method.

        If auto_reconnect is enabled and the send fails due to a connection
        error, attempts to reconnect and retry once.
        """
        try:
            self._send_message_once(msg)
        except (OSError, ConnectionError) as exc:
            if self._auto_reconnect:
                logger.warning("send failed (%s), reconnecting", exc)
                if self.reconnect():
                    self._send_message_once(msg)
                else:
                    raise ConnectionError("send failed and reconnect unsuccessful") from exc
            else:
                raise

    def _send_message_once(self, msg: Message) -> None:
        with self._lock:
            if self._sock is None:
                raise RuntimeError("Not connected")
            sock = self._sock

        data = msg.encode()
        length = len(data)
        if length > MAX_MESSAGE_SIZE:
            raise ValueError(f"Message too large: {length} bytes (max {MAX_MESSAGE_SIZE})")

        # Write 4-byte big-endian length header
        header = struct.pack(">I", length)
        sock.sendall(header)
        sock.sendall(data)

    def receive_message(self) -> Message:
        """Receive a length-prefixed message.

        Reads the 4-byte header, then the JSON payload.
        Matches the Go stream handler reading logic.

        If auto_reconnect is enabled and reading fails due to a connection
        error, attempts to reconnect (but does NOT retry the receive since
        the original message is lost).
        """
        try:
            return self._receive_message_once()
        except (OSError, ConnectionError) as exc:
            if self._auto_reconnect:
                logger.warning("receive failed (%s), reconnecting", exc)
                self.reconnect()
            raise

    def _receive_message_once(self) -> Message:
        with self._lock:
            if self._sock is None:
                raise RuntimeError("Not connected")
            sock = self._sock

        # Read 4-byte length header
        header = self._recv_exact(sock, 4)
        length = struct.unpack(">I", header)[0]

        if length > MAX_MESSAGE_SIZE:
            raise ValueError(f"Message too large: {length} bytes (max {MAX_MESSAGE_SIZE})")

        # Read payload
        data = self._recv_exact(sock, length)
        return decode_message(data)

    @staticmethod
    def _recv_exact(sock: socket.socket, n: int) -> bytes:
        """Read exactly n bytes from the socket."""
        buf = bytearray()
        while len(buf) < n:
            chunk = sock.recv(n - len(buf))
            if not chunk:
                raise ConnectionError("Connection closed by remote")
            buf.extend(chunk)
        return bytes(buf)

    def close(self) -> None:
        """Close the TCP connection and stop health checks."""
        self._closed = True

        # Stop health check thread.
        self._health_stop.set()
        if self._health_thread is not None:
            self._health_thread.join(timeout=5)
            self._health_thread = None

        with self._lock:
            if self._sock is not None:
                try:
                    self._sock.close()
                except OSError:
                    pass
                self._sock = None

    @property
    def connected(self) -> bool:
        """Return True if connected."""
        with self._lock:
            return self._sock is not None

    def __enter__(self) -> "MicelioClient":
        return self

    def __exit__(self, *args) -> None:
        self.close()

    def __del__(self) -> None:
        self.close()
