"""Throwaway OpenID Connect provider for CI and e2e: generates an RSA key, publishes its JWKS and signs ID tokens.

The API trusts it only where docker-compose.idp.yml wires it in as the generic "oidc" provider. Never used in production.

  python3 scripts/testidp.py init      # writes .idp/private.pem and .idp/public/jwks.json
"""
import base64
import json
import os
import sys
import time

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".idp")
KID = "ci-key-1"
ISSUER = "http://idp"
AUDIENCE = "cortex-ci"
JWKS_URL = "http://idp/jwks.json"


def _b64(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def _int(n):
    return _b64(n.to_bytes((n.bit_length() + 7) // 8, "big"))


def init():
    os.makedirs(os.path.join(ROOT, "public"), exist_ok=True)
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    with open(os.path.join(ROOT, "private.pem"), "wb") as f:
        f.write(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    nums = key.public_key().public_numbers()
    jwks = {"keys": [{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": KID, "n": _int(nums.n), "e": _int(nums.e)}]}
    with open(os.path.join(ROOT, "public", "jwks.json"), "w") as f:
        json.dump(jwks, f)
    os.chmod(ROOT, 0o755)
    os.chmod(os.path.join(ROOT, "public"), 0o755)
    os.chmod(os.path.join(ROOT, "public", "jwks.json"), 0o644)


def id_token(sub, nonce, **overrides):
    with open(os.path.join(ROOT, "private.pem"), "rb") as f:
        key = serialization.load_pem_private_key(f.read(), None)
    now = int(time.time())
    claims = {"iss": ISSUER, "aud": AUDIENCE, "sub": sub, "iat": now, "exp": now + 600, "nonce": nonce}
    claims.update(overrides)
    head = _b64(json.dumps({"alg": "RS256", "typ": "JWT", "kid": KID}).encode())
    body = _b64(json.dumps(claims).encode())
    sig = key.sign(f"{head}.{body}".encode(), padding.PKCS1v15(), hashes.SHA256())
    return f"{head}.{body}.{_b64(sig)}"


if __name__ == "__main__":
    if sys.argv[1:] == ["init"]:
        init()
    else:
        sys.exit(__doc__)
