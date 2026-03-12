"""Basic mDNS discovery for finding Micelio peers on the local network.

Uses the zeroconf library (optional dependency).
Service name matches Go: "_aip-network._tcp.local."
"""

import time
from dataclasses import dataclass
from typing import List, Optional

try:
    from zeroconf import ServiceBrowser, ServiceInfo, Zeroconf

    _HAS_ZEROCONF = True
except ImportError:
    _HAS_ZEROCONF = False


# mDNS service type (matches Go mDNS discovery)
SERVICE_TYPE = "_aip-network._tcp.local."


@dataclass
class Peer:
    """A discovered peer on the local network."""

    host: str
    port: int
    name: str
    properties: dict


class _Listener:
    """Internal zeroconf listener that collects discovered services."""

    def __init__(self):
        self.peers: List[Peer] = []

    def add_service(self, zc: "Zeroconf", type_: str, name: str) -> None:
        info = zc.get_service_info(type_, name)
        if info is None:
            return
        addresses = info.parsed_addresses()
        if not addresses:
            return
        props = {}
        if info.properties:
            for k, v in info.properties.items():
                key = k.decode("utf-8") if isinstance(k, bytes) else k
                val = v.decode("utf-8") if isinstance(v, bytes) else v
                props[key] = val
        self.peers.append(
            Peer(
                host=addresses[0],
                port=info.port,
                name=name,
                properties=props,
            )
        )

    def remove_service(self, zc: "Zeroconf", type_: str, name: str) -> None:
        pass

    def update_service(self, zc: "Zeroconf", type_: str, name: str) -> None:
        pass


def discover_peers(
    service_type: str = SERVICE_TYPE,
    timeout: float = 5.0,
) -> List[Peer]:
    """Discover Micelio peers on the local network via mDNS.

    Args:
        service_type: mDNS service type to browse for.
        timeout: How long to wait for responses (seconds).

    Returns:
        List of discovered peers.

    Raises:
        ImportError: If zeroconf is not installed.
    """
    if not _HAS_ZEROCONF:
        raise ImportError(
            "zeroconf is required for mDNS discovery. "
            "Install with: pip install micelio[discovery]"
        )

    zc = Zeroconf()
    listener = _Listener()
    try:
        browser = ServiceBrowser(zc, service_type, listener)
        time.sleep(timeout)
    finally:
        zc.close()

    return listener.peers
