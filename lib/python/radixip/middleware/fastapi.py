"""FastAPI middleware backed by the native RadixPolicy binding."""

from typing import Callable, Optional

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import JSONResponse, Response


class RadixIPMiddleware(BaseHTTPMiddleware):
    """Apply one shared RadixPolicy instance to every HTTP request.

    Pass ``resolve_ip`` when the application has a trusted proxy policy. The
    default uses the direct peer address and does not trust forwarded headers.
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
        return request.client.host if request.client else None

    async def dispatch(self, request: Request, call_next) -> Response:
        ip = self.resolve_ip(request)
        if not ip:
            return JSONResponse({"error": "invalid client IP"}, status_code=400)

        result = self.policy.check_ip(ip)
        decision = result["decision"]
        if decision == "allow":
            return await call_next(request)
        if decision == "limit":
            return JSONResponse(
                {"error": "rate limited"},
                status_code=429,
                headers={"Retry-After": str(result["retry_after_seconds"] or 1)},
            )
        if decision == "block":
            return JSONResponse({"error": "blocked"}, status_code=403)
        return JSONResponse({"error": "invalid client IP"}, status_code=400)
