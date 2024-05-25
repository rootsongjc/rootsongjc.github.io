import jwt
import requests
from jwt.algorithms import RSAAlgorithm
import argparse
import json
import base64

def base64url_decode(input):
    rem = len(input) % 4
    if rem > 0:
        input += '=' * (4 - rem)
    return base64.urlsafe_b64decode(input)

def get_signing_key(jwk_url, kid):
    jwks = requests.get(jwk_url).json()
    for jwk in jwks['keys']:
        if jwk['kid'] == kid:
            return RSAAlgorithm.from_jwk(jwk)
    raise Exception('Public key not found.')

def validate_token(token, audience, issuer, jwk_url):
    headers = jwt.get_unverified_header(token)
    kid = headers['kid']
    signing_key = get_signing_key(jwk_url, kid)

    decoded_token = jwt.decode(
        token,
        signing_key,
        algorithms=["RS256"],
        audience=audience,
        issuer=issuer
    )
    return decoded_token

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description='Validate a JWT token.')
    parser.add_argument('token', type=str, help='The JWT token to validate')
    args = parser.parse_args()

    token = args.token

    # 解析 token 的 payload 部分，提取 audience 和 issuer
    header, payload, signature = token.split('.')
    decoded_payload = base64url_decode(payload)
    payload_json = json.loads(decoded_payload)

    audience = payload_json['aud']
    issuer = payload_json['iss']
    jwk_url = f"{issuer}.well-known/jwks.json"

    try:
        decoded = validate_token(token, audience, issuer, jwk_url)
        print("Token is valid. Decoded payload:")
        for key, value in decoded.items():
            print(f"{key}: {value}")
    except Exception as e:
        print(f"Token validation failed: {e}")