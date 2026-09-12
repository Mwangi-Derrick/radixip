"""Flask / Django WSGI helpers backed by the native RadixPolicy binding.

These are thin request-hook helpers, not full middleware classes, because Flask
and Django have different hook APIs.  Import the function that matches your
framework and call it early in the request lifecycle.

Example — Flask before_request::

    from flask import Flask, request, jsonify
    from radixip import RadixPolicy
    from radixip.middleware.wsgi import make_flask_hook

    app = Flask(__name__)
    policy = RadixPolicy.from_yaml("config/radixip.yaml")

    app.before_request(make_flask_hook(policy))

Example — Django middleware class::

    # settings.py
    MIDDLEWARE = [
        "radixip.middleware.wsgi.RadixIPDjangoMiddleware",
        ...
    ]

    # Pass the policy via Django settings if you cannot import it at class
    # definition time:
    RADIXIP_POLICY = RadixPolicy.from_yaml("config/radixip.yaml")
"""
from __future__ import annotations

from typing import Callable, Optional


# ---------------------------------------------------------------------------
# Flask
# ---------------------------------------------------------------------------

def make_flask_hook(policy, resolve_ip: Optional[Callable] = None) -> Callable:
    """Return a Flask ``before_request`` compatible hook.

    Parameters
    ----------
    policy:
        A ``RadixPolicy`` instance.
    resolve_ip:
        Optional callable ``(flask.Request) -> str | None``.  Defaults to
        ``flask.request.remote_addr``.

    Example::

        @app.before_request
        def radixip_gate():
            return make_flask_hook(policy)()
    """
    def _hook():
        # Import Flask here so this module can be imported without Flask
        # installed (e.g. in a Django-only project).
        from flask import request, jsonify, abort

        if resolve_ip:
            ip = resolve_ip(request)
        else:
            ip = request.remote_addr

        if not ip:
            return jsonify({"error": "invalid client IP"}), 400

        result = policy.check_request(
            ip,
            request.method,
            request.path,
        )
        decision = result["decision"]

        if decision == "allow":
            return None  # Flask: None means "continue"

        if decision == "limit":
            retry_after = str(result.get("retry_after_seconds") or 1)
            response = jsonify({"error": "rate limited"})
            response.status_code = 429
            response.headers["Retry-After"] = retry_after
            return response

        if decision in ("block", "auto_ban"):
            return jsonify({"error": "blocked"}), 403

        return jsonify({"error": "invalid client IP"}), 400

    return _hook


# ---------------------------------------------------------------------------
# Django
# ---------------------------------------------------------------------------

class RadixIPDjangoMiddleware:
    """Django WSGI middleware that gates every request with RadixPolicy.

    Add to ``settings.MIDDLEWARE`` *before* authentication middleware so that
    blocked / rate-limited clients are rejected early.

    Configuration is read from ``settings.RADIXIP_POLICY`` (a ``RadixPolicy``
    instance) or ``settings.RADIXIP_CONFIG_PATH`` (path to ``radixip.yaml``).

    Example settings.py::

        from radixip import RadixPolicy
        RADIXIP_POLICY = RadixPolicy.from_yaml("config/radixip.yaml")

        MIDDLEWARE = [
            "radixip.middleware.wsgi.RadixIPDjangoMiddleware",
            "django.middleware.security.SecurityMiddleware",
            ...
        ]
    """

    def __init__(self, get_response):
        from django.conf import settings as _settings
        self.get_response = get_response
        if hasattr(_settings, "RADIXIP_POLICY"):
            self.policy = _settings.RADIXIP_POLICY
        elif hasattr(_settings, "RADIXIP_CONFIG_PATH"):
            from radixip import RadixPolicy
            self.policy = RadixPolicy.from_yaml(_settings.RADIXIP_CONFIG_PATH)
        else:
            raise RuntimeError(
                "RadixIPDjangoMiddleware requires either settings.RADIXIP_POLICY "
                "or settings.RADIXIP_CONFIG_PATH to be defined."
            )
        self.resolve_ip = getattr(_settings, "RADIXIP_RESOLVE_IP", None)

    def __call__(self, request):
        from django.http import JsonResponse

        if self.resolve_ip:
            ip = self.resolve_ip(request)
        else:
            ip = request.META.get("REMOTE_ADDR")

        if not ip:
            return JsonResponse({"error": "invalid client IP"}, status=400)

        result = self.policy.check_request(ip, request.method, request.path)
        decision = result["decision"]

        if decision == "allow":
            return self.get_response(request)

        if decision == "limit":
            retry_after = str(result.get("retry_after_seconds") or 1)
            response = JsonResponse({"error": "rate limited"}, status=429)
            response["Retry-After"] = retry_after
            return response

        if decision in ("block", "auto_ban"):
            return JsonResponse({"error": "blocked"}, status=403)

        return JsonResponse({"error": "invalid client IP"}, status=400)
