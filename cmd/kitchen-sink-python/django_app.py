import os
import sys
import json

# Ensure radixip is in path if running from source
sys.path.append(os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', 'lib', 'python')))

from django.conf import settings
from django.core.wsgi import get_wsgi_application
from django.http import JsonResponse
from django.urls import path

from radixip import RadixPolicy

config_path = os.environ.get("RADIXIP_CONFIG", os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', 'config', 'radixip.yaml')))
policy = RadixPolicy.from_yaml(config_path)

def radixip_middleware(get_response):
    def middleware(request):
        xff = request.META.get('HTTP_X_FORWARDED_FOR')
        ip = xff.split(',')[0].strip() if xff else request.META.get('REMOTE_ADDR')

        if not ip:
            return JsonResponse({"error": "invalid client IP"}, status=400)

        result = policy.check_request(ip, request.method, request.path)
        decision = result["decision"]

        if decision == "allow":
            return get_response(request)
        elif decision == "limit":
            retry_after = str(result.get("retry_after_seconds") or 1)
            response = JsonResponse({"error": "rate limited"}, status=429)
            response['Retry-After'] = retry_after
            return response
        elif decision in ("block", "auto_ban"):
            return JsonResponse({"error": "blocked"}, status=403)
        
        return JsonResponse({"error": "invalid client IP"}, status=400)
    return middleware

# Views
def health(request):
    return JsonResponse({"ok": True})

def public(request):
    return JsonResponse({"framework": "django", "route": "public"})

def auth(request):
    if request.method == "POST":
        return JsonResponse({"framework": "django", "route": "auth-post"})
    return JsonResponse({"framework": "django", "route": "auth-get"})

# Configure Django settings
settings.configure(
    DEBUG=False,
    ROOT_URLCONF=__name__,
    ALLOWED_HOSTS=['*'],
    MIDDLEWARE=[
        f'{__name__}.radixip_middleware',
    ],
)

urlpatterns = [
    path('health', health),
    path('api/v1/public', public),
    path('api/v1/auth', auth),
]

application = get_wsgi_application()

if __name__ == "__main__":
    from django.core.management import execute_from_command_line
    execute_from_command_line(["django_app.py", "runserver", "0.0.0.0:8095"])
