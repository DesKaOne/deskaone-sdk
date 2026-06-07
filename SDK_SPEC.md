# DesKaOne SDK Cross-Language Specification

## 1. Overview

DesKaOne SDK is a cross-language networking SDK for building TCP, proxy, HTTP, WebSocket, reconnecting WebSocket, storage, and database functionality with consistent behavior across supported languages.

This specification defines the expected behavior for the existing Go, Dart, and TypeScript SDKs and for future ports to Python, Rust, C, and other languages. Language APIs may use idiomatic names, types, errors, asynchronous models, and package layouts, but the observable behavior described in this document must remain consistent.

## 2. Supported Core Features

Ports should support the following core features unless a feature is explicitly documented as unavailable for that language or platform:

- `ProxyConfig`
- `ProxyPicker`
- `TCPClient`
- HTTP CONNECT proxy handler
- SOCKS4 / SOCKS4a proxy handler
- SOCKS5 proxy handler
- Auto TLS upgrade
- `HttpClient` over `TCPClient`
- `WebSocketClient` over `TCPClient`
- `ReconnectWebSocketClient`
- Utility helpers
- Optional storage/database modules

## 3. Proxy URL Format

Supported proxy URL formats:

- `http://host:port`
- `http://user:pass@host:port`
- `socks4://host:port`
- `socks4://user@host:port`
- `socks5://host:port`
- `socks5://user:pass@host:port`
- `host:port`
- `user:pass@host:port`

Default behavior:

- Missing scheme may use a configurable default proxy type.
- `http` and `https` schemes map to HTTP CONNECT proxy.
- `s4`, `sock4`, and `socks4` schemes map to SOCKS4.
- `s5`, `sock5`, and `socks5` schemes map to SOCKS5.
- Username and password values must be URL-decoded when parsed.
- `toString` / `toUrl` output must URL-encode credentials.
- Host must not be empty.
- Port must be in the inclusive range `1..65535`.

## 4. ProxyPicker Behavior

SDK ports should provide the following picker behaviors:

- Single proxy picker: always returns the configured proxy.
- Round-robin proxy picker: returns proxies in deterministic rotating order.
- Random proxy picker: returns one proxy selected randomly from the configured list.
- An empty proxy list must throw, return, or raise a `NoProxyAvailable`-style error using the language's idiomatic error mechanism.
- Pickers must not allow external mutation of their internal proxy list. Constructors and accessors should copy lists or otherwise protect internal state.

## 5. TCPClient Behavior

Connection matrix:

| Mode | Flow |
| --- | --- |
| Direct plain TCP | Socket/connect target -> `TcpConnection` |
| Direct TLS | Socket/connect target -> TLS upgrade -> `TcpConnection` |
| Proxy plain TCP | Socket/connect proxy -> proxy handshake -> `TcpConnection` |
| Proxy TLS | Socket/connect proxy -> proxy handshake -> TLS upgrade to target -> `TcpConnection` |

Requirements:

- Validate destination host and port before attempting a connection.
- Support connection timeout.
- Support local bind/source address where the language and runtime support it.
- Set `TCP_NODELAY` when possible.
- Destroy or close the socket on failure.
- Preserve leftover bytes after proxy handshakes.
- TLS server name / SNI defaults to the destination host but can be overridden.

## 6. HTTP CONNECT Handler

Request format:

```http
CONNECT host:port HTTP/1.1
Host: host:port
User-Agent: DesKaOne/...
Proxy-Connection: Keep-Alive
Proxy-Authorization: Basic ...
```

`Proxy-Authorization: Basic ...` is included only when proxy username/password credentials exist.

Requirements:

- Read the proxy response until `CRLF CRLF`.
- Max response header size defaults to 32 KiB.
- HTTP status `200` means success.
- Non-`200` responses must return a sanitized error.
- `Proxy-Authenticate` header should be surfaced if present.
- Authorization headers must be redacted in errors.
- Preserve leftover bytes read beyond the HTTP CONNECT response header.

## 7. SOCKS4 / SOCKS4a Handler

Requirements:

- Use SOCKS4 IPv4 mode when the target host is an IPv4 address.
- Use SOCKS4a domain mode when the target host is not an IPv4 address.
- Proxy username maps to SOCKS4 `userid`.
- Null bytes in username or host are invalid.
- Request command is `CONNECT`.
- Response must be exactly 8 bytes.
- Response `VN` must be `0x00`.
- Response `CD` value `0x5A` means success.

Reply error mapping:

| Code | Meaning |
| --- | --- |
| `0x5B` | request rejected or failed |
| `0x5C` | identd missing |
| `0x5D` | identd could not confirm userid |

## 8. SOCKS5 Handler

Requirements:

- Implement method negotiation.
- Support no-auth method `0x00`.
- Support username/password method `0x02`.
- Reject unsupported methods and method `0xFF`.
- Username and password lengths must each be `<=255` bytes.
- CONNECT request uses domain address type `ATYP 0x03` by default for consistency.
- Validate response version, reply code, reserved byte, and bound address type.

Reply error mapping:

| Code | Meaning |
| --- | --- |
| `0x01` | general SOCKS server failure |
| `0x02` | connection not allowed by ruleset |
| `0x03` | network unreachable |
| `0x04` | host unreachable |
| `0x05` | connection refused |
| `0x06` | TTL expired |
| `0x07` | command not supported |
| `0x08` | address type not supported |

## 9. HttpClient Behavior

Requirements:

- Must use `TCPClient` internally.
- Must not depend on a built-in high-level HTTP client for core behavior.
- Support `http` and `https` schemes.
- Support proxy routing through `TCPClient`.
- Use HTTP/1.1.
- Send `Connection: close` by default.
- Send `Accept-Encoding: identity` by default unless decompression is implemented.
- Validate headers to prevent CRLF injection.

Supported request operations:

- `GET`
- `POST`
- `PUT`
- `PATCH`
- `DELETE`
- Custom request
- JSON body helper

Response/body handling requirements:

- Support `Content-Length`.
- Support `Transfer-Encoding: chunked`.
- Support body-until-close responses.
- Support redirects with `maxRedirects` default `5`.
- Max header size defaults to 32 KiB.
- Max body size defaults to 10 MiB.

Response must expose:

- URI
- HTTP version
- Status code
- Reason phrase
- Headers
- Body bytes
- Body/text helper
- JSON helper if the language supports it

## 10. WebSocketClient Behavior

Requirements:

- Must use `TCPClient` directly, not `HttpClient`.
- Support `ws` and `wss` schemes.
- Support direct, proxy, and TLS connections.
- Send an HTTP Upgrade handshake.
- Generate `Sec-WebSocket-Key` using 16 bytes from a secure random source.
- Validate `Sec-WebSocket-Accept` as `base64(sha1(key + GUID))`.
- Use GUID `258EAFA5-E914-47DA-95CA-C5AB0DC85B11`.
- Do not request `permessage-deflate` unless compression is implemented.
- Reject RSV bits if compression is not implemented.
- Client frames must be masked.
- Decode server frames.
- Auto-pong defaults to `true`.
- `maxPayloadSize` defaults to 16 MiB.

Supported opcodes / frame types:

- Continuation
- Text
- Binary
- Close
- Ping
- Pong

Control frame requirements:

- Control frames must not be fragmented.
- Control frame payload length must be `<=125` bytes.

API should expose:

- `sendText`
- `sendJson`
- `sendBinary`
- `ping`
- `pong`
- `close`
- `destroy`
- `readyState`
- `protocol`
- `closeCode`
- `closeReason`
- Message stream / event API

## 11. ReconnectWebSocketClient Behavior

Requirements:

- Wraps `WebSocketClient`.
- Automatically reconnects on unexpected close/error.
- Manual stop/close must not reconnect.
- `reconnectDelay` defaults to 2 seconds.
- `maxReconnects` value `-1` means unlimited reconnect attempts.

API should expose:

- `run`
- `stop`
- `close`
- `sendText`
- `sendJson`
- `sendBinary`
- `current`
- `isRunning`

Callbacks/events:

- `onConnect`
- `onMessage`
- `onError`
- `onDisconnect`

## 12. Error Handling Convention

- Each language should use idiomatic errors or exceptions.
- Error messages should be clear and stable enough for debugging.
- Sensitive values must be redacted in errors, logs, debug output, and string representations.

Sensitive values include:

- Proxy credentials
- `Authorization`
- `Proxy-Authorization`
- Tokens
- API keys
- Database passwords
- Storage encryption keys

## 13. Timeout Convention

Ports should document and consistently apply timeout options for:

- TCP connect timeout
- Proxy handshake timeout
- TLS handshake timeout
- HTTP response read timeout
- WebSocket handshake timeout
- Ping interval
- Reconnect delay

## 14. Security Rules

- Never hardcode proxy credentials, API keys, tokens, database passwords, or storage keys.
- Examples must use environment variables for secrets.
- Debug logs must not print secrets.
- Header values must reject CRLF injection.
- SecureStorage dev key must not be used in production.
- Debug JSON dump exposes plaintext and must be disabled in production.

## 15. SecureStorage Optional Module

Expected behavior:

- File-based encrypted key-value storage.
- JSON map as plaintext before encryption.
- AES-CBC + PKCS7 or stronger encryption.
- Must include HMAC or AEAD integrity check.
- Verify integrity before decrypting.
- Atomic write.
- Backup recovery.
- Debug dump only when explicitly enabled.
- Key must be 16, 24, or 32 bytes, or an equivalent supported size for the selected cryptographic primitive.

## 16. Database Optional Module

Expected behavior:

- Simple abstraction, not an ORM.
- Supported dialects:
  - SQLite
  - PostgreSQL
- API:
  - `open`
  - `close`
  - `execute`
  - `query`
  - `queryOne`
  - `transaction`
- Tests must not require live PostgreSQL unless explicitly marked as integration tests.

## 17. Test Matrix

| Area | Required coverage |
| --- | --- |
| ProxyConfig parse | Valid formats, invalid hosts, invalid ports, credential encoding/decoding |
| ProxyPicker | Single, round-robin, random, empty list error, mutation protection |
| HTTP CONNECT parser | Success, non-200, auth, max header size, leftover bytes |
| SOCKS4 request/reply | IPv4 mode, SOCKS4a mode, userid, reply error mapping |
| SOCKS5 negotiation/reply | No-auth, username/password, unsupported method, reply error mapping |
| TCP direct plain | Direct TCP connection flow and timeout handling |
| TCP direct TLS | TLS upgrade, SNI default, SNI override |
| TCP proxy plain | Proxy connect and handshake without TLS upgrade |
| TCP proxy TLS | Proxy connect, handshake, TLS upgrade to target |
| HttpClient content-length | Fixed-size response body parsing and max body limits |
| HttpClient chunked | Chunked response parsing and malformed chunk handling |
| HttpClient redirect | Redirect following and `maxRedirects` enforcement |
| WebSocket accept hash | `Sec-WebSocket-Accept` validation |
| WebSocket frame encode/decode | Masking, payload lengths, opcodes, RSV rejection |
| WebSocket close handshake | Close frame handling and close metadata |
| Reconnect behavior | Unexpected close reconnect, manual stop does not reconnect |
| SecureStorage corruption recovery | Integrity failure, backup recovery, no plaintext debug by default |
| SQLite transaction rollback | Transaction rollback on failure |

## 18. Language Port Checklist

- [ ] Create package scaffold
- [ ] Port `ProxyConfig`
- [ ] Port `ProxyPicker`
- [ ] Port `TCPClient`
- [ ] Port HTTP/SOCKS handlers
- [ ] Port `HttpClient`
- [ ] Port `WebSocketClient`
- [ ] Port `ReconnectWebSocketClient`
- [ ] Port utilities
- [ ] Add examples
- [ ] Add tests
- [ ] Add README English
- [ ] Add README_ID Indonesian
- [ ] Add CI
- [ ] Add LICENSE
- [ ] Add CHANGELOG
- [ ] Run format/lint/test

## 19. Current Implementations

- Go: https://github.com/DesKaOne/deskaone-sdk
- Dart: https://github.com/DesKaOne/deskaone-sdk-dart
- TypeScript: https://github.com/DesKaOne/deskaone-sdk-ts
