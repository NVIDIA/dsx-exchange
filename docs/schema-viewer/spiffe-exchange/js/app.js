
    const schema = {
  "asyncapi": "3.1.0",
  "info": {
    "title": "SPIRE SPIFFE Exchange - Public Keysets",
    "version": "1.0.0",
    "description": "AsyncAPI specification for publishing JWK (JSON Web Key) public keys on the\nSPIFFE/SPIRE exchange topic. One JWK per message. Used to distribute public\nkeys for a given tenant and key identifier so consumers can verify JWS or\nuse keys for encryption.\n\n**Topic format:** `spiffe-exchange/v1/pub-keysets/tenant/{tenant_domain}/kid/{kid}`\n\nPayloads conform to RFC 7517 (JSON Web Key). Only public key material is\npublished on this channel.\n"
  },
  "servers": {
    "production": {
      "host": "broker.example.com",
      "protocol": "mqtt",
      "description": "MQTT broker for SPIFFE exchange public key distribution"
    }
  },
  "channels": {
    "pubKeysets": {
      "address": "spiffe-exchange/v1/pub-keysets/tenant/{tenant_domain}/kid/{kid}",
      "parameters": {
        "tenant_domain": {
          "description": "Tenant domain identifier (e.g. tenant namespace or domain name)."
        },
        "kid": {
          "description": "Key ID (kid) for this key; aligns with JWS/JWE header kid."
        }
      },
      "messages": {
        "jwk": {
          "name": "JwkMessage",
          "title": "JWK (RFC 7517)",
          "contentType": "application/json",
          "payload": {
            "type": "object",
            "required": [
              "kty"
            ],
            "description": "Single JSON Web Key per RFC 7517. Only public key parameters are included on this channel. Key type (kty) determines which additional members are present.\n",
            "properties": {
              "kty": {
                "type": "string",
                "description": "Key type (e.g. RSA, EC, OKP).",
                "enum": [
                  "RSA",
                  "EC",
                  "OKP"
                ],
                "x-parser-schema-id": "<anonymous-schema-3>"
              },
              "use": {
                "type": "string",
                "description": "Public key use (sig, enc, or omitted).",
                "enum": [
                  "sig",
                  "enc"
                ],
                "x-parser-schema-id": "<anonymous-schema-4>"
              },
              "key_ops": {
                "type": "array",
                "items": {
                  "type": "string",
                  "x-parser-schema-id": "<anonymous-schema-6>"
                },
                "description": "Key operations (e.g. verify, encrypt).",
                "x-parser-schema-id": "<anonymous-schema-5>"
              },
              "alg": {
                "type": "string",
                "description": "Algorithm (e.g. ES256, RS256, EdDSA).",
                "x-parser-schema-id": "<anonymous-schema-7>"
              },
              "kid": {
                "type": "string",
                "description": "Key ID; should match the topic kid when present.",
                "x-parser-schema-id": "<anonymous-schema-8>"
              },
              "n": {
                "type": "string",
                "description": "RSA modulus (Base64url).",
                "x-parser-schema-id": "<anonymous-schema-9>"
              },
              "e": {
                "type": "string",
                "description": "RSA public exponent (Base64url).",
                "x-parser-schema-id": "<anonymous-schema-10>"
              },
              "crv": {
                "type": "string",
                "description": "Elliptic curve (e.g. P-256, P-384).",
                "x-parser-schema-id": "<anonymous-schema-11>"
              },
              "x": {
                "type": "string",
                "description": "EC x coordinate (Base64url).",
                "x-parser-schema-id": "<anonymous-schema-12>"
              },
              "y": {
                "type": "string",
                "description": "EC y coordinate (Base64url).",
                "x-parser-schema-id": "<anonymous-schema-13>"
              }
            },
            "x-parser-schema-id": "Jwk"
          },
          "x-parser-unique-object-id": "jwk"
        }
      },
      "x-parser-unique-object-id": "pubKeysets"
    }
  },
  "operations": {
    "publishPubKeyset": {
      "action": "send",
      "channel": "$ref:$.channels.pubKeysets",
      "messages": [
        "$ref:$.channels.pubKeysets.messages.jwk"
      ],
      "description": "Publish one JWK for the given tenant and kid. Publishers (e.g. SPIRE) use this to advertise a public key for verification or encryption.\n",
      "x-parser-unique-object-id": "publishPubKeyset"
    },
    "subscribePubKeyset": {
      "action": "receive",
      "channel": "$ref:$.channels.pubKeysets",
      "messages": [
        "$ref:$.channels.pubKeysets.messages.jwk"
      ],
      "description": "Subscribe to public key updates for a tenant and kid. Each message carries one JWK. Consumers use the key to verify signatures or encrypt.\n",
      "x-parser-unique-object-id": "subscribePubKeyset"
    }
  },
  "components": {
    "messages": {
      "JwkMessage": "$ref:$.channels.pubKeysets.messages.jwk"
    },
    "schemas": {
      "Jwk": "$ref:$.channels.pubKeysets.messages.jwk.payload"
    }
  },
  "x-parser-spec-parsed": true,
  "x-parser-api-version": 3,
  "x-parser-spec-stringified": true
};
    const config = {"show":{"sidebar":true},"sidebar":{"showOperations":"byDefault"}};
    const appRoot = document.getElementById('root');
    AsyncApiStandalone.render(
        { schema, config, }, appRoot
    );
  