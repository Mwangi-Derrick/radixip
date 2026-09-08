"""RadixIP: high-performance IP radix-tree middleware for Python.

Import the native classes directly::

    from radixip import RadixEngine, RadixPolicy, version

Or use the framework middleware::

    from radixip.middleware import RadixIPMiddleware
"""
# The native extension is built with PyO3/maturin and compiled into
# radixip.radixip (or radixip.<platform>.so). Import it here so callers
# can use `from radixip import RadixEngine` without knowing the internal layout.
try:
    from .radixip import RadixEngine, RadixPolicy, version  # type: ignore[import]
except ImportError:
    # Development fallback: if the native module hasn't been built yet, let
    # the ImportError surface with a helpful message rather than a cryptic one.
    raise ImportError(
        "RadixIP native extension not found. "
        "Run `maturin develop` or `pip install -e .` to build from source."
    ) from None

__all__ = ["RadixEngine", "RadixPolicy", "version"]
