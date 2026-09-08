"""
Type stubs for the RadixIP native extension (radixip.radixip).

These definitions mirror the PyO3 classes exported from ``src/lib.rs``.
IDEs and mypy use this file for autocompletion and type-checking; the
actual implementation is in the compiled .so / .pyd extension module.
"""
from __future__ import annotations

from typing import Dict, Optional


class RadixEngine:
    """High-performance IP radix-tree engine.

    Example::

        from radixip import RadixEngine

        engine = RadixEngine()
        engine.insert("10.0.0.0/8", {"value": "blocked", "attributes": {"asn": "AS1234"}})

        match = engine.lookup("10.1.2.3")
        if match:
            print(match["value"])   # "blocked"
    """

    def __init__(self, variant: Optional[str] = None) -> None:
        """Create a new engine.

        Parameters
        ----------
        variant:
            One of ``"standard"``, ``"concurrent"``, ``"lockfree"``,
            ``"adaptive"``.  Defaults to the memory-efficient variant.
        """
        ...

    def insert(self, subnet: str, metadata: Dict[str, object]) -> None:
        """Insert a CIDR prefix with associated metadata dict.

        Parameters
        ----------
        subnet:
            CIDR notation, e.g. ``"10.0.0.0/8"``.
        metadata:
            Must contain at least ``{"value": str, "attributes": dict}``.
        """
        ...

    def lookup(self, ip: str) -> Optional[Dict[str, object]]:
        """Longest-prefix match.

        Returns a metadata dict on match, or ``None``.
        """
        ...

    def remove(self, subnet: str) -> bool:
        """Remove a prefix.  Returns ``True`` if the entry existed."""
        ...

    def contains(self, ip: str) -> bool:
        """Returns ``True`` if any stored prefix covers the given IP."""
        ...

    def clear(self) -> None:
        """Remove all entries."""
        ...

    def __len__(self) -> int:
        """Number of stored prefixes."""
        ...

    def stats(self) -> Dict[str, int]:
        """Engine performance statistics.

        Returns a dict with keys: ``size``, ``inserts``, ``lookups``,
        ``hits``, ``misses``, ``removals``.
        """
        ...

    def __repr__(self) -> str: ...


class RadixPolicy:
    """Composite policy handle: blocklist + token-bucket rate limiter.

    Load once at application startup; reuse for every request.

    Example::

        from radixip import RadixPolicy

        policy = RadixPolicy.from_yaml("config/radixip.yaml")

        result = policy.check_ip("203.0.113.5")
        if result["decision"] == "allow":
            ...  # pass the request through
    """

    @staticmethod
    def from_yaml(path: str) -> "RadixPolicy":
        """Load the shared RadixIP YAML policy configuration.

        Parameters
        ----------
        path:
            File-system path to ``radixip.yaml``.

        Raises
        ------
        ValueError
            If the file cannot be read or the schema is invalid.
        """
        ...

    def check_ip(self, ip: str) -> Dict[str, object]:
        """Evaluate one already-extracted client IP.

        Returns
        -------
        dict with keys:

        * ``"decision"`` — one of ``"allow"``, ``"block"``, ``"limit"``,
          ``"auto_ban"``, ``"bad_request"``
        * ``"retry_after_seconds"`` — ``int``, meaningful when decision is
          ``"limit"``

        Raises
        ------
        ValueError
            If ``ip`` is not a valid IPv4 or IPv6 address.
        """
        ...


def version() -> str:
    """Return the RadixIP library semantic version string."""
    ...
