import os
import sys

# Ensure radixip is in path if running from source
sys.path.append(os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', 'lib', 'python')))

from flask import Flask, request, jsonify
from radixip import RadixPolicy

app = Flask(__name__)

config_path = os.environ.get("RADIXIP_CONFIG", os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', 'config', 'radixip.yaml')))
policy = RadixPolicy.from_yaml(config_path)

@app.before_request
def radixip_middleware():
    xff = request.headers.get("X-Forwarded-For")
    ip = xff.split(",")[0].strip() if xff else request.remote_addr

    if not ip:
        return jsonify({"error": "invalid client IP"}), 400

    result = policy.check_ip(ip)
    decision = result["decision"]

    if decision == "allow":
        return None  # Continue
    elif decision == "limit":
        retry_after = str(result.get("retry_after_seconds") or 1)
        response = jsonify({"error": "rate limited"})
        response.status_code = 429
        response.headers["Retry-After"] = retry_after
        return response
    elif decision in ("block", "auto_ban"):
        return jsonify({"error": "blocked"}), 403
    
    return jsonify({"error": "invalid client IP"}), 400

@app.route("/health")
def health():
    return jsonify({"ok": True})

@app.route("/api/v1/public")
def public():
    return jsonify({"framework": "flask", "route": "public"})

@app.route("/api/v1/auth", methods=["GET"])
def auth_get():
    return jsonify({"framework": "flask", "route": "auth-get"})

@app.route("/api/v1/auth", methods=["POST"])
def auth_post():
    return jsonify({"framework": "flask", "route": "auth-post"})

if __name__ == "__main__":
    app.run(port=8094)
