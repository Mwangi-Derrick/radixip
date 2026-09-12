"""FastAPI / Starlette middleware backed by the native RadixPolicy binding."""

from typing import Callable, Optional

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import JSONResponse, Response


class RadixIPMiddleware(BaseHTTPMiddleware):
    """Apply one shared RadixPolicy instance to every HTTP request.

    Parameters
    ----------
    app:
        The ASGI application to wrap.
    policy:
        A ``RadixPolicy`` instance (from ``RadixPolicy.from_yaml(...)``).
        Create it once at application startup, **not** inside the middleware
        constructor, to avoid rebuilding the policy on every worker fork.
    resolve_ip:
        Optional callable ``(request: Request) -> str | None``.  Use this to
        implement trusted-proxy rules.  The default reads the direct peer
        address from ``request.client.host``, which is correct when the
        application sits behind an authenticated ingress that strips and
        re-sets ``X-Forwarded-For``.

    Example::

        from fastapi import FastAPI
        from radixip import RadixPolicy
        from radixip.middleware import RadixIPMiddleware

        app = FastAPI()
        policy = RadixPolicy.from_yaml("config/radixip.yaml")
        app.add_middleware(RadixIPMiddleware, policy=policy)

    Trusted-proxy example::

        def resolve_ip(request: Request) -> str | None:
            # Only safe after the ingress proxy has been authenticated.
            xff = request.headers.get("x-forwarded-for", "")
            return xff.split(",")[0].strip() or None

        app.add_middleware(RadixIPMiddleware, policy=policy, resolve_ip=resolve_ip)
    """

    def __init__(
        self,
        app,
        policy,
        resolve_ip: Optional[Callable[[Request], Optional[str]]] = None,
    ):
        super().__init__(app)
        self.policy = policy
        self.resolve_ip = resolve_ip or self._peer_ip

    @staticmethod
    def _peer_ip(request: Request) -> Optional[str]:
        """Return the direct peer address, or None if unavailable."""
        return request.client.host if request.client else None

    async def dispatch(self, request: Request, call_next: Callable) -> Response:
        ip = self.resolve_ip(request)
        if not ip:
            return JSONResponse({"error": "invalid client IP"}, status_code=400)

        result = self.policy.check_request(ip, request.method, request.url.path)
        decision = result["decision"]

        if decision == "allow":
            return await call_next(request)

        if decision == "limit":
            retry_after = str(result.get("retry_after_seconds") or 1)
            return JSONResponse(
                {"error": "rate limited"},
                status_code=429,
                headers={"Retry-After": retry_after},
            )

        if decision in ("block", "auto_ban"):
            # auto-banned clients use the same 403 path as static blocklist hits.
            return JSONResponse({"error": "blocked"}, status_code=403)

        # bad_request or unknown decision
        return JSONResponse({"error": "invalid client IP"}, status_code=400)
