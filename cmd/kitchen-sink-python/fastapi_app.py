import os
import sys

# Ensure radixip is in path if running from source (fallback)
sys.path.append(os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', 'lib', 'python')))

from fastapi import FastAPI, Request
from radixip import RadixPolicy
from radixip.middleware import RadixIPMiddleware

app = FastAPI(title="RadixIP FastAPI Sink")

config_path = os.environ.get("RADIXIP_CONFIG", os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', 'config', 'radixip.yaml')))
policy = RadixPolicy.from_yaml(config_path)

def resolve_ip(request: Request) -> str | None:
    # Use X-Forwarded-For to allow the test runner to spoof IPs
    xff = request.headers.get("x-forwarded-for")
    if xff:
        return xff.split(",")[0].strip()
    return request.client.host if request.client else None

app.add_middleware(RadixIPMiddleware, policy=policy, resolve_ip=resolve_ip)

@app.get("/health")
async def health():
    return {"ok": True}

@app.get("/api/v1/public")
async def public():
    return {"framework": "fastapi", "route": "public"}

@app.get("/api/v1/auth")
async def auth_get():
    return {"framework": "fastapi", "route": "auth-get"}

@app.post("/api/v1/auth")
async def auth_post():
    return {"framework": "fastapi", "route": "auth-post"}
