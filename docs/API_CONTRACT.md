# API Contract 방향

이 문서는 터널별 Connector 모델의 API 방향이다. 구현된 경로와 정확한 필드는 [api/openapi.json](../api/openapi.json)에 정의한다. 일부 폐기·설정 적용·stream 경로는 아직 구현하지 않았다.

새 관리형 Connector는 재사용 가능한 실행 토큰을 bearer credential로 사용한다. 이 토큰은 초기 등록과 컨테이너 재생성 후 재연결에 사용하며, 교체하거나 Connector를 삭제할 때까지 유효하다. 기존 PoC의 일회용 등록 토큰과 30일 API credential도 지원한다. 등록 응답은 201이다. 서비스 등록 전에는 `services`가 없거나 비어 있고 Connector는 설정을 주기적으로 조회한다. 서비스 등록 후에는 각 서비스의 `service_id`, `target`, `tunnel`을 담은 `services` 배열을 반환한다.

## 1. 채널과 인증

| 경로 | 용도 | 인증 |
| --- | --- | --- |
| /api/v1/* | 관리자 UI/API | 관리자 세션 또는 관리자 API 토큰 |
| /connector/v1/enrollment | Connector 등록·재연결 | 재사용 가능한 실행 토큰 + HTTPS |
| /connector/v1/* | heartbeat·설정·터널 제어 | Connector별 실행 토큰 또는 기존 자격 증명 |

Connector는 항상 연결을 먼저 시작한다. 공인 Outlet은 외부 HTTP/HTTPS와 등록 endpoint를 제외하고 Connector 관리 포트를 직접 공개하지 않는다.

모든 공개 주소는 관리 호스트와 같은 사이트(same-site)이므로 `SameSite=Strict` 쿠키만으로는 터널로 연결한 앱이 관리 API에 보내는 폼 요청을 막지 못한다. 그래서 `GET`, `HEAD`, `OPTIONS`가 아닌 브라우저 요청은 `Sec-Fetch-Site`가 `same-origin`이나 `none`이어야 하고, 이 헤더가 없으면 `Origin`의 호스트가 `Host`와 같아야 한다. 어긋나면 `403 cross_origin_request`다. 두 헤더가 모두 없는 요청(Connector, CLI)은 그대로 처리한다. 공개 경로로 들어온 응답에는 `Strict-Transport-Security: max-age=31536000`을 붙인다.

## 2. 관리자 API

### Outlet과 Connector

~~~
GET    /api/v1/outlet
PUT    /api/v1/outlet/tunnel
POST   /api/v1/outlet/tunnel/check
GET    /api/v1/diagnostics/trace
GET    /api/v1/connectors
GET    /api/v1/connectors/:id/usage
POST   /api/v1/connectors
GET    /api/v1/connectors/:id
POST   /api/v1/connectors/:id/enrollment-tokens
POST   /api/v1/connectors/:id/revoke
~~~

`GET /api/v1/diagnostics/trace`는 관리자 세션 인증이 필요하다. Outlet 호스트명과 설정된 `region`, 요청 peer 주소, 전달 헤더, `CF-Ray` 및 colo 접미사를 반환한다. `region`은 자동 위치 판정이 아니라 `outlet.json`에 설정한 설명용 값이다. 전달 헤더와 `CF-Ray`는 직접 연결에서 위조될 수 있으므로 접근 제어나 인증 판단에 사용하지 않는다.

`GET /api/v1/outlet`과 이 응답을 돌려주는 변경 API(`PUT /api/v1/outlet`, `POST /api/v1/outlet/domains` 등)의 본문에는 Outlet 소프트웨어 버전 `version`(예: `"0.1.0"`)이 들어 있다. 인증이 필요 없는 `GET /api/v1/setup/status`는 버전을 반환하지 않는다. 릴리스 노트와 대조해 패치되지 않은 버전을 찾는 데 쓰일 수 있기 때문이다.

Outlet 응답의 `tunnel`은 Connector가 데이터 터널에 접속하는 `address`, 직접 지정하지 않았을 때의 `default_address`, 직접 지정 여부 `custom`을 담는다. 주소는 관리 화면 설정, `outlet.json`의 `tunnel_remote_address`, `관리주소:터널수신포트` 순서로 정한다. 관리 주소(`admin_hostname`)는 대표 도메인을 처음 등록할 때 만든 8글자 무작위 이름과 대표 도메인을 합친 값이며, 그 전에 설정한 Outlet은 `meerkat`을 유지한다. `PUT /api/v1/outlet/tunnel`에 `{"address":"tunnel.example.com:2333"}`을 보내 지정하고, 빈 문자열을 보내면 기본값으로 되돌린다. 기본값이 없으면(대표 도메인과 `tunnel_remote_address`가 모두 없음) 비울 수 없으며 `409 tunnel_default_unavailable`을 반환한다. 포트를 생략하면 터널 수신 포트를 붙이며 형식이 잘못되었거나 루프백 주소이면 `400 invalid_tunnel_address`를 반환한다. 주소가 바뀌면 관리형 Connector의 모든 서비스 설정에 반영하고 revision을 올린다. 대표 도메인을 바꿔 기본 주소가 바뀔 때와 Outlet이 시작할 때도 같다. 한 Connector의 서비스는 항상 같은 터널 주소를 쓰므로 서비스를 추가할 때도 기존 서비스를 현재 주소로 맞춘다.

Outlet 응답의 `modes`는 도메인마다 DNS 방식을 담는다. `wildcard`는 `*` 레코드 하나, `hosts`는 관리 주소와 공개 주소마다 레코드를 만드는 방식이며 기본값은 `wildcard`다. `PUT /api/v1/outlet`과 `POST /api/v1/outlet/domains`에 `mode`를 함께 보낼 수 있고(대표 도메인을 바꿀 때 생략하면 기존 방식을 유지), `PUT /api/v1/outlet/domains/{domain}/mode`에 `{"mode":"hosts"}`를 보내 바꾼다. 잘못된 값은 `400 invalid_domain_mode`, 등록되지 않은 도메인은 `404 domain_not_found`다. 방식은 DNS 안내와 확인 대상만 바꾸며 라우팅과 인증서 발급은 같다. `POST /api/v1/outlet/domains/{domain}/check-dns`는 대표 도메인이면 관리 주소, `wildcard`면 임의의 하위 주소 하나, `hosts`면 그 도메인의 공개 주소마다, 그리고 그 도메인에 속한 터널 주소를 조회한다. 루트는 터널 주소일 때만 조회한다.

`POST /api/v1/outlet/tunnel/check`는 `{"address":"..."}`의 주소를, 비어 있으면 현재 터널 주소를 Outlet에서 DNS 조회한 뒤 조회된 IP마다(최대 8개) TCP 연결을 시도해 `reachable`과 IP별 `results`(`latency_ms`, `error`: `connection_refused`, `timeout`, `unreachable`, `connection_failed`, `address_not_allowed`)를 반환한다. 루프백, 링크 로컬, 멀티캐스트 주소는 Connector가 접속할 수 없는 주소이므로 연결하지 않고 `address_not_allowed`로 표시한다. DNS 조회 실패는 `error: dns_lookup_failed`로 표시한다. 응답한 서버가 이 Outlet인지와 Connector 쪽 방화벽은 확인하지 않는다. 터널 주소가 없으면 `409 tunnel_address_unset`을 반환한다.

`POST /api/v1/connectors`는 `name`과 선택 항목 `description`(관리자 메모, 줄바꿈을 제외한 제어 문자 없이 최대 500자)을 받는다. `PATCH /api/v1/connectors/{id}`에 `{"description":"..."}`나 `{"display_name":"..."}`(관리 화면에 보일 이름, 64자까지, 비우면 `name`을 보여줌)를 보내 바꾼다. 둘 다 없으면 `400 invalid_request`, 형식이 잘못되면 `400 invalid_description`/`invalid_display_name`, 없는 Connector면 `404 connector_not_found`다. `name`은 Connector 등록과 설치 명령·컨테이너 이름에 쓰이므로 바꿀 수 없다. 메모는 관리 화면에만 쓰이고 Connector 설정에는 포함되지 않는다. `GET /api/v1/connectors`의 각 Connector는 마지막 heartbeat가 알린 Connector 버전을 `version`(예: `0.5.2`)으로 담으며, heartbeat를 받기 전에는 없다.

Connector 폐기는 관련 Service·Route의 새 연결을 먼저 차단한 뒤 identity를 폐기한다.

`GET /api/v1/connectors/:id/usage`는 해당 터널의 시간별 HTTP 요청 수, 추정 요청 바이트, 응답 본문 바이트, HTTP 5xx 응답 수와 서비스별 합계를 반환한다. `hours` 쿼리는 `24`(기본), `168`, `720`을 지원한다. `service_id`를 지정하면 시간별 데이터만 해당 서비스로 필터링하고, 서비스별 합계 목록은 선택 메뉴를 위해 전체 서비스를 반환한다. Caddy의 호스트별 집계 메트릭을 Outlet이 15초마다 읽고 SQLite에 차이를 저장한다. 개별 요청의 URL, IP, 헤더는 저장하지 않는다. 집계는 기능 적용 이후의 공개 경로부터 시작하며, Caddy가 재시작되어 수집되지 못한 요청과 삭제된 공개 경로의 기록은 포함되지 않는다.

### Service와 Route

~~~
GET    /api/v1/connectors/:id/services
POST   /api/v1/connectors/:id/services
PATCH  /api/v1/connectors/:id/services/:service_id
POST   /api/v1/services/:id/revoke
GET    /api/v1/services/:id/routes
POST   /api/v1/services/:id/routes
DELETE /api/v1/services/:id/routes/:route_id
PUT    /api/v1/addresses/:hostname
~~~

Service 생성은 외부 공개를 뜻하지 않는다. Route를 활성화해야 외부 endpoint가 생긴다.
서비스의 앱 호스트와 포트는 `PATCH /api/v1/connectors/:id/services/:service_id`에 `{"target":{"host":"app","port":3000}}`을 보내 수정한다. 한 터널에 여러 서비스를 등록할 수 있지만, 같은 터널 안에서 대상 호스트(대소문자 구분 없음)와 포트의 조합은 고유해야 한다. 생성·수정 시 중복이면 `409 service_target_taken`을 반환한다. 같은 앱의 공개 주소가 여러 개 필요하면 하나의 서비스에 Route를 여러 개 추가한다. 서비스 추가·수정·삭제 시 설정 revision을 올린다. Connector는 새 설정을 받아 Rathole에 반영하고, Outlet은 변경된 서비스의 probe 결과가 확인될 때까지 그 공개 경로만 보류한다. 변경되지 않은 서비스의 상태와 공개 경로는 유지한다.

**공유 주소.** 같은 hostname을 여러 서비스(다른 터널 포함)가 함께 쓸 수 있다.

- 등록: `POST /api/v1/services/:id/routes`는 다른 서비스가 쓰는 hostname이면 `409 hostname_taken`이다. `{"hostname": "…", "shared": true}`로 보내면 그 주소의 마지막 대상으로 추가한다. 한 서비스는 한 주소에 한 번만 등록된다.
- Route 응답: `priority`(장애 조치 순서, 작을수록 먼저), `address_mode`(`failover`, `balance`), `address_sticky`, `address_backends`(그 주소를 쓰는 서비스 수)가 들어 있다.
- 정책: `PUT /api/v1/addresses/:hostname`에 `{"mode": "failover"|"balance", "sticky": bool, "order": ["<route id>", …], "current": {"mode", "sticky", "order"}}`를 보낸다. `order`는 그 주소의 Route를 모두 한 번씩 담아야 한다. `current`는 호출자가 읽은 상태이며, 주면 그 사이 다른 관리자가 바꾼 경우 `409 address_changed`로 거부한다. 성공하면 `204`와 `address.update` 감사 기록이고, 값이 맞지 않으면 `400 invalid_address_policy`, 주소가 없으면 `404 address_not_found`다. `sticky`는 `balance`에서만 의미가 있다.
- 선택: 공개 요청은 터널이 연결되고 서비스가 probe에 응답하는 대상만 받는다. `failover`는 그중 순서가 가장 앞선 대상을 고른다. `balance`는 차례대로 고르고, `sticky`이면 클라이언트 IP별 rendezvous hashing으로 대상을 고정한다. 정상 대상이 없으면 `404`다.
- 재시도: 대상에 닿지 못하면 본문 없는 요청에 한해 다음 대상으로 재시도한다. 연결에 요청을 쓰지 못했으면(전송 계층의 내부 재시도 포함) 모든 메서드를, 쓴 뒤 응답 전에 끊겼거나 시간이 초과되면 GET, HEAD, OPTIONS만 재시도한다. 대상이 응답한 뒤(업그레이드 포함)에는 재시도하지 않는다. 다음 대상은 재시도 시점의 라우팅과 상태로 다시 고르며, 한 요청은 대상마다 한 번만 시도한다.
- 상태: 공유 주소의 대상은 Outlet probe와 Connector의 상태 보고가 모두 정상일 때(관리 화면의 서비스 상태와 같다) 요청을 받는다. 응답 전에 실패한 대상은 다른 정상 대상이 있으면 10초 동안 제외하고, 응답하면 바로 복귀한다.
- 사용량: Caddy는 호스트 단위로 집계하므로 공유 주소의 사용량은 순서가 가장 앞선 Route에 기록한다. 증가분의 기준값은 호스트 단위로 두어, 순서가 바뀌어도 이전 사용량을 다시 세지 않는다.
- 삭제: 터널을 삭제해 주소에 남은 Route가 없으면 주소 정책도 지운다.
- 삭제: 마지막 Route를 지우면 주소 정책도 지운다.

### 예시: Service 생성

~~~
{
  "name": "example-web",
  "protocol": "http",
  "target": {
    "host": "app",
    "port": 3000
  },
  "health_check": {
    "type": "tcp",
    "timeout_ms": 2000
  }
}
~~~

target.host는 기본적으로 같은 Compose 네트워크의 서비스 이름 또는 loopback만 허용한다. 임의의 외부 주소는 MVP에서 거부한다.

### 예시: HTTP Route

~~~
{
  "service_id": "service_1",
  "kind": "http",
  "hostname": "example.com",
  "access_policy_id": "public-web"
}
~~~

### 예시: TCP Route

~~~
{
  "service_id": "service_db",
  "kind": "tcp",
  "listen_port": 15432,
  "access_policy_id": "office-only"
}
~~~

### 로그인, Telegram 알림, 감사 로그, 요청 제한

~~~
POST   /api/v1/session
POST   /api/v1/session/second-factor
GET    /api/v1/alerts
PUT    /api/v1/alerts/telegram
POST   /api/v1/alerts/telegram/test
DELETE /api/v1/alerts/telegram
PUT    /api/v1/alerts/rules
PUT    /api/v1/security/second-factor
GET    /api/v1/security/rate-limit
PUT    /api/v1/security/rate-limit
GET    /api/v1/security/connection-limits
PUT    /api/v1/security/connection-limits
GET    /api/v1/security/tunnel-guard
PUT    /api/v1/security/tunnel-guard
GET    /api/v1/security/block-rules
PUT    /api/v1/security/block-rules
GET    /api/v1/security/client-bans?page=&per_page=&q=
PUT    /api/v1/security/client-bans
POST   /api/v1/security/client-bans/release
GET    /api/v1/security/sso
PUT    /api/v1/security/sso
DELETE /api/v1/security/sso
POST   /api/v1/security/sso/test
GET    /api/v1/session/sso/start
GET    /api/v1/session/sso/callback
GET    /api/v1/audit
GET    /api/v1/audit/export
~~~

오류 응답은 항상 `{"code": "...", "request_id": "..."}`이다. 아래 경로는 `GET /api/v1/alerts`부터 모두 관리자 세션이 필요하다.

**Telegram 설정.** Outlet마다 봇 하나를 쓴다. `GET /api/v1/alerts`는 다음을 반환한다. 봇 토큰은 어떤 응답에도 나오지 않고 마지막 네 글자 `token_hint`만 나온다.

~~~
{
  "telegram": {"configured": true, "enabled": true, "chat_id": "-1001234567890", "token_hint": "b-cd", "verified_at": "2026-10-26T14:00:00Z"},
  "rules": {
    "tunnel_down": {"enabled": true, "after_minutes": 5},
    "service_down": {"enabled": true, "after_minutes": 5},
    "route_error": {"enabled": true},
    "dns_failure": {"enabled": true},
    "error_spike": {"enabled": true, "threshold_percent": 5, "min_requests": 50},
    "login": {"enabled": true},
    "rate_limit": {"enabled": true, "min_blocked": 500},
    "latency": {"enabled": true, "increase_percent": 50, "min_requests": 100},
    "outlet_restart": {"enabled": true},
    "disk_low": {"enabled": true, "min_free_percent": 10},
    "memory_low": {"enabled": true, "min_free_percent": 10},
    "tunnel_unstable": {"enabled": true, "min_reconnects": 3}
  },
  "events": [{"id": 12, "kind": "tunnel_down", "subject": "home-server", "message": "Tunnel home-server disconnected (5 min)",
              "opened_at": "2026-10-26T14:00:00Z", "resolved_at": null, "sent": true, "error": null}],
  "login_second_factor": false
}
~~~

- `PUT /api/v1/alerts/telegram` 본문은 `{"bot_token"?: string, "chat_id": string, "enabled": bool}`이다. `bot_token`을 생략하거나 비우면 저장된 토큰을 유지한다. 토큰 형식(`123456789:AA…`)이 틀리거나 저장된 토큰도 없으면 `400 invalid_telegram_token`, 채팅 ID가 숫자(`-100…` 포함)나 `@채널이름`이 아니면 `400 invalid_telegram_chat`이다. 토큰이나 채팅 ID를 바꾸면 `verified_at`이 지워지고, 확인된 봇을 전제로 하는 로그인 보안 코드도 함께 꺼진다. 응답은 `GET`과 같다.
- `POST /api/v1/alerts/telegram/test`는 저장된 설정으로 테스트 메시지를 보낸다. 성공하면 `verified_at`을 기록하고 `GET` 형태를 반환한다. 설정이 없으면 `409 telegram_not_configured`, Telegram이 거부하거나 닿지 않으면 `502`와 `telegram_unauthorized`(토큰이 틀림), `telegram_chat_not_found`(채팅 ID가 틀렸거나 봇이 시작·초대되지 않음), `telegram_unreachable`(네트워크·시간 초과), `telegram_failed` 중 하나를 반환한다. 메시지를 보내는 동안 다른 요청이 토큰이나 채팅을 바꾸었으면 테스트한 설정이 저장된 설정이 아니므로 `409 telegram_changed`를 반환하고 확인으로 치지 않는다.
- `DELETE /api/v1/alerts/telegram`은 토큰과 채팅 ID를 지우고 알림과 로그인 보안 코드를 끄며 열려 있던 알림 이벤트를 조용히 닫는다. 응답은 `GET`과 같다.
- `PUT /api/v1/alerts/rules`는 규칙을 담은 객체로 설정을 통째로 바꾼다. 처음부터 있던 여섯 규칙(`tunnel_down`, `route_error`, `dns_failure`, `error_spike`, `login`, `rate_limit`)은 모두 담아야 하며 키가 빠지면 `400 invalid_request`다. 나중에 추가된 규칙(`service_down`, `latency`, `outlet_restart`, `disk_low`, `memory_low`, `tunnel_unstable`)은 키가 빠지거나 `null`이면 저장된 값을 유지하므로 이전 버전의 화면이나 스크립트도 그대로 저장할 수 있다. 담긴 규칙은 통째로 받으므로 그 안에서 빠진 필드는 저장된 값이 아니라 0으로 본다. 범위는 `after_minutes`(`tunnel_down`, `service_down`) 1–1440, `threshold_percent` 0.1–100, `min_requests`(`error_spike`, `latency`) 1–1,000,000, `min_blocked` 1–1,000,000, `increase_percent` 10–1000, `min_free_percent`(`disk_low`, `memory_low`) 1–50, `min_reconnects` 2–100이며 벗어나면 `400 invalid_alert_rules`다. 응답은 `GET`과 같다.
- `PUT /api/v1/security/second-factor`는 `{"enabled": bool}`을 받아 `{"login_second_factor": bool}`을 반환한다. 켜려면 Telegram이 설정되고 테스트 메시지로 확인되어 있어야 하며 아니면 `409 telegram_not_verified`다.

**알림 규칙과 이벤트.** 알림은 Telegram이 설정되고 `enabled`일 때만 평가한다. 이벤트의 `kind`는 규칙 키와 같은 `tunnel_down`, `service_down`, `tunnel_unstable`, `route_error`, `dns_failure`, `error_spike`, `latency`, `rate_limit`, `disk_low`, `memory_low`, `outlet_restart`, `login`이다. 같은 대상의 알림은 열려 있는 동안 다시 보내지 않고, 풀리면 복구 메시지(`tunnel_down`, `service_down`, `route_error`, `dns_failure`, `disk_low`, `memory_low`)를 한 번 보내며 `resolved_at`을 채운다. 같은 대상은 1시간 안에 다시 열지 않는다. `tunnel_unstable`, `error_spike`, `latency`, `rate_limit`, `outlet_restart`, `login`은 즉시 닫히는 한 번짜리 이벤트(`resolved_at`이 `opened_at`과 같음)이다. `subject`는 터널 이름, `터널 / 서비스` 이름, 공개 주소, 도메인, 시간 구간(RFC 3339), 관리자 이메일 등 대상을 나타내는 짧은 문자열이다. 전송에 실패하면 `sent`가 `false`, `error`에 위 오류 코드가 남는다. 열려 있는 이벤트(`tunnel_down`, `service_down`, `route_error`, `dns_failure`, `disk_low`, `memory_low`)는 한 시간 동안 1분마다 다시 시도하고, 한 번도 알리지 못한 이벤트는 복구 메시지도 보내지 않는다. 최근 50개만 `events`에 담기며 오래된 닫힌 이벤트는 200개를 넘으면 삭제한다.

- `tunnel_down`: 등록 완료된 터널이 `after_minutes` 이상 `connected`가 아니면 알린다.
- Outlet이 재시작하면 장애가 시작된 시각을 잊으므로 `tunnel_down`과 `service_down`은 대기 시간을 다시 잰다. 그동안에도 장애가 이어지면 열려 있던 이벤트는 닫지 않고 다시 보내지도 않는다.
- `service_down`: 연결된 터널의 켜진 서비스가 `after_minutes` 이상 `connected`가 아니면(`error`: Connector가 앱에 닿지 못함, `connecting`: Outlet이 터널 너머로 보낸 확인 요청이 응답을 받지 못하거나 5xx를 받음, `unknown`: 아직 확인 전) 알린다. 메시지의 `Problem` 줄이 어느 쪽인지 밝힌다. 터널이 `connected`가 아닌 동안에는 `tunnel_down`의 몫이므로 그 서비스의 이벤트를 열지도 닫지도 않고, 터널이 돌아오면 `after_minutes`를 처음부터 다시 잰다. 서비스를 삭제하거나 규칙을 끄면 복구 메시지 없이 닫는다.
- `tunnel_unstable`: 터널이 응답 없이 지나간 뒤 다시 연결되었거나 터널 프로세스가 재시작한 횟수가 한 시간 안에 `min_reconnects` 이상이면 알린다. 재연결은 15초와 그 Connector 하트비트 간격의 2.5배 중 긴 시간 동안 하트비트가 없었던 경우다. 터널 대부분이 30초 안에 함께 재연결한 것은 Outlet 쪽 사정(Caddy 재시작, 호스트 네트워크)으로 보고 세지 않으며, 규칙이 꺼져 있던 동안의 기록도 세지 않는다. 터널마다 한 시간에 한 번만 보내는 한 번짜리 이벤트다.
- `route_error`: 5분마다 공개 주소와 관리 주소를 Caddy(`MEERKAT_CADDY_METRICS`의 호스트, 443 포트)를 거쳐 TLS 핸드셰이크로 점검한다. 핸드셰이크나 인증서 검증이 실패하거나, 인증서 남은 기간이 14일과 수명의 6분의 1 중 짧은 쪽보다 적으면 실패로 친다. 연속 2회 실패하면 알리고 점검을 통과하면 복구 메시지를 보낸다. Caddy는 인증서를 핸드셰이크 때 발급·갱신(on-demand)하므로, Caddy가 응답한 채 알린 주소는 실패한 ACME 검증이 쌓이지 않도록(Let's Encrypt는 이름당 시간당 5회 제한) 30분마다만 다시 점검한다. 같은 이유로 이 점검은 아직 아무도 방문하지 않은 주소의 인증서 발급도 시작한다. 한 번의 점검에서 Caddy가 어떤 주소에도 응답하지 않았고(연결 실패 또는 핸드셰이크 무응답) 그중 하나가 연속 2회 그랬다면, 주소별 이벤트 대신 Caddy 주소를 `subject`로 한 이벤트 하나로 알리고 주소별 이벤트는 그대로 둔다. 각 점검은 가장 오래전에 확인한 주소부터 20초 안에서 진행한다. 공개 주소에 `last_error`가 생겨도 알린다.
- `dns_failure`: 15분마다 도메인별 필수 이름(DNS 연결 확인과 같은 목록)을 조회해 실패하면 알린다.
- `error_spike`: 직전 UTC 시간대와, 30분이 지난 현재 시간대의 5xx 비율이 `threshold_percent` 이상이고 요청이 `min_requests` 이상이면 시간대마다 한 번 알린다. 시간대의 첫 몇 분(정각에 도는 작업 등)만으로 판단하지 않기 위해서다.
- `latency`: Outlet 처리 시간과 서비스 응답 시간을 종류마다 따로, 직전 UTC 시간대와 30분이 지난 현재 시간대에 대해 점검한다. 그 시간대의 p95가 직전 7일의 p95보다 `increase_percent` 이상 늘었고, 시간대의 요청이 `min_requests` 이상, 직전 7일의 요청이 그 10배 이상이며, 늘어난 시간이 절대값으로 Outlet 처리 20 ms, 서비스 응답 100 ms 이상이면 종류마다 시간대에 한 번 알린다. 한 번짜리 이벤트다.
- `login`: 관리자 로그인이 성공할 때마다 이메일, 접속 주소(또는 로컬 관리 포트), 브라우저·OS 요약을 보낸다. 비동기로 보내므로 Telegram 장애가 로그인을 막거나 늦추지 않는다.
- `rate_limit`: 최근 10분 동안 거부한 요청이 `min_blocked` 이상이면 작동한 제한 종류와 상위 IP·서브넷·엔드포인트를 알린다. 계속되는 동안 30분에 한 번만 보낸다.
- `disk_low`: Outlet 데이터 디스크의 여유 공간이 `min_free_percent` 미만이면 알리고, `min_free_percent` + 2 이상으로 돌아오면 복구 메시지를 보낸다.
- `memory_low`: 1분마다 잰 여유 메모리가 세 번 연속(약 2분) `min_free_percent` 미만이면 알린다. 컨테이너에 메모리 제한이 있으면 그 제한을, 없으면 호스트의 메모리를 기준으로 하며, `min_free_percent` + 2 이상으로 돌아오면 복구 메시지를 보낸다.
- `outlet_restart`: 직전 실행이 정상적으로 끝나지 않은 채(충돌, OOM 종료, 정전 등) Outlet이 시작되면 첫 알림 점검에서 한 번 알린다. 데이터 디렉터리의 `running.json`으로 판단하며 정상 종료(SIGTERM 등) 때 지운다. Telegram이 아직 설정되지 않았으면 시작 후 1시간까지 기다렸다 보내고 그 뒤에는 버린다. 재시작이 반복되어도 1시간에 한 번만 보내는 한 번짜리 이벤트다.

Telegram 메시지는 HTML(`parse_mode`)이며 이모지와 굵은 제목 한 줄, `이름: 값` 줄 두세 개, 마지막 줄의 관리 화면 링크(`https://관리주소/connectors/ID`, `/domains/도메인`, `/observe`, `/`)로 통일한다. 모든 동적 값은 이스케이프하고 식별자는 `<code>`로 감싼다.

**환경 변수의 텔레그램.** `MEERKAT_TELEGRAM_BOT_TOKEN`과 `MEERKAT_TELEGRAM_CHAT_ID`(그리고 `MEERKAT_TELEGRAM_ENABLED`, 기본 `true`)를 주면 관리 화면에서 봇을 저장하지 않았을 때 그 값을 쓴다. `GET /api/v1/alerts`의 `telegram.source`가 `environment`이고, 테스트 메시지로 확인한 상태는 환경 변수의 봇·채팅이 그대로일 때만 유지된다. 관리 화면에서 저장하면(`source: "database"`) 그 값이 우선하고, `DELETE /api/v1/alerts/telegram`은 저장한 값을 지워 환경 변수의 봇으로 돌아간다. `telegram.environment`는 환경 변수에 봇이 있는지를 뜻한다.

**로그인 보안 코드.** `login_second_factor`가 켜져 있으면 인터넷(공개 주소)에서 온 `POST /api/v1/session`은 이메일과 비밀번호가 맞아도 세션을 만들지 않는다. 대신 6자리 코드를 Telegram으로 보내고 `202 {"second_factor": "telegram", "challenge": "<id>"}`를 반환한다. 코드는 해시만 메모리에 두며 5분 동안 유효하고 5번까지 입력할 수 있다. Telegram 전송에 실패하면 `502 telegram_unreachable`이다. 이어서 `POST /api/v1/session/second-factor`에 `{"challenge": "...", "code": "123456"}`을 보내면 기존 로그인과 같은 `201`과 세션 쿠키를 받는다. 코드가 틀렸거나 만료·모르는 challenge이면 모두 같은 `401 invalid_second_factor`이고, 다섯 번째 오답은 challenge를 폐기하며 `429 too_many_attempts`다. 두 로그인 경로 모두 클라이언트 주소(IPv6는 /56 단위)당 분당 10회로 제한하며 넘으면 `429 rate_limited`와 `Retry-After: 60`을 반환한다. 인터넷에서 온 비밀번호 확인은 주소와 관계없이 15분 동안 오답 30회, 동시에 4건까지만 받고, 넘으면 `429 rate_limited`와 남은 시간을 담은 `Retry-After`를 반환한다. 세션 쿠키는 `__Host-meerkat_session`(HttpOnly·Secure·SameSite=Strict, `Path=/`)이다. `__Host-` 접두사 때문에 브라우저는 Domain 속성이 붙은 쿠키를 이 이름으로 받지 않으므로, 같은 대표 도메인의 다른 호스트(터널로 공개한 앱 등)가 관리 화면 쿠키를 덮어쓰거나 끼워 넣을 수 없다. 공개 주소에서 로그인에 성공한 브라우저는 신뢰 기기 쿠키(`__Host-meerkat_device`, 90일, `Path=/`, HttpOnly·Secure·SameSite=Strict, Outlet만 아는 키로 서명)를 받고, 이 쿠키가 있으면 누구나 채울 수 있는 오답 30회 제한을 받지 않는다. 주소당 제한과 동시 4건 제한, 보안 코드는 그대로 적용되며 쿠키 자체는 아무 권한도 주지 않는다. 로컬 관리 포트는 이 공용 제한을 받지 않아 SSH로는 항상 로그인할 수 있다. 로그아웃은 데이터베이스에서 세션을 폐기한 뒤 메모리 캐시에서 지우며, 폐기 전에 시작된 조회가 세션을 다시 캐시하지 못한다. 등록된 관리자가 없는 이메일도 같은 비밀번호 해시 계산을 거치므로 응답 시간으로 관리자 이메일을 알아낼 수 없다.

SSH 터널로 접속하는 **로컬 관리 포트**의 로그인은 코드 없이 세션을 만든다. 운영자가 서버에 SSH로 들어갈 수 있으면 언제든 복구할 수 있다는 뜻이다. 요청이 공개 경로를 거쳤는지는 헤더가 아니라 Outlet이 `PublicHandler` 안에서만 요청 컨텍스트에 남기는 표시로 판단하므로 클라이언트가 위조할 수 없다. 접속 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목만 쓰고, 로컬 포트에서는 어떤 헤더도 믿지 않는다.

**감사 로그.** `GET /api/v1/audit?limit=50&offset=0&action=<접두사>&q=<텍스트>`는 최신순으로 반환한다. `limit`은 1–200(기본 50), `offset`은 0 이상이며 어긋나면 `400 invalid_audit_query`다. `action`은 동작 이름의 앞부분(`connector.` 등), `q`는 `actor_email`, `resource_id`, `detail`을 대소문자 구분 없이 부분 일치로 찾는다(128자까지). `GET /api/v1/audit/export?action=&q=&utc_offset=`는 같은 조건의 모든 항목을 같은 순서로 CSV로 내려받는다(아래 **CSV 내려받기**). 열은 `id`, `at`, `action`, `actor_email`, `resource_type`, `resource_id`, `source`, `ip`, `user_agent`, `detail`(JSON)이고, 시작할 때의 마지막 항목까지만 담아 그사이 추가된 항목 때문에 쪽이 어긋나지 않는다.

~~~
{"events": [{"id": "m_…", "created_at": "2026-10-26T14:00:00.000Z", "actor_email": "admin@example.com",
             "action": "connector.create", "resource_type": "connector", "resource_id": "m_…",
             "source": "public", "ip": "203.0.113.7", "user_agent": "Mozilla/5.0 …", "detail": {"name": "home-server"}}],
 "total": 1}
~~~

`source`는 `public`(인터넷 경로) 또는 `local`(로컬 관리 포트)이고, `ip`는 `public`일 때만 Caddy가 전달한 접속 주소이며 그 밖에는 `null`이다. `actor_email`은 세션의 관리자이며, 관리자 계정이 생기기 전 설정 토큰으로 한 작업과 알 수 없는 시도에서는 `null`(로그인 실패는 시도한 이메일)이다. `detail`은 비밀이 아닌 필드만 담는다. 토큰, 비밀번호, 인증 코드, 봇 토큰은 기록하지 않는다. 기록은 요청을 기다리게 하지 않는다. 항목은 작업 시각과 ID를 붙여 메모리 큐에 넣고 백그라운드에서 묶어 쓴다. 데이터베이스가 받지 못하면 메모리에 두고 다시 시도하며, 메모리 한도를 넘으면 데이터 디렉터리의 `spool/audit/`에 파일로 옮겼다가 쓰기 작업이 한가할 때 다시 쓴다. 같은 ID는 한 번만 저장되므로 다시 써도 중복되지 않는다. 큐와 파일 한도까지 모두 넘으면 그 항목은 버리고 개수를 로그에 남기지만 요청은 그대로 처리한다. 종료할 때는 남은 항목을 쓰거나 파일로 옮긴 뒤 데이터베이스를 닫는다. 목록은 작업 시각 순서다.

감사 로그는 추가만 가능하다. SQLite 트리거가 `audit_events`의 모든 `UPDATE`와 `DELETE`(`REPLACE` 포함)를 `audit log is append-only`로 중단하고, 삭제·수정·정리 API와 보관 기간은 없다. 데이터베이스 파일에 직접 접근할 수 있는 사람은 트리거를 지울 수 있으므로 파일 권한(0600)과 서버 접근 통제가 전제다.

인증 없이 들어온 입력은 영구 로그를 채우지 않게 걸러 낸다. 실패한 로그인의 이메일은 올바른 이메일 형식일 때만 남기고, User-Agent의 제어 문자는 지우며, 모르는 challenge로 보낸 2단계 인증 요청과 토큰 없는 Connector 등록 시도는 기록하지 않는다. `connector.enroll`은 실행 토큰을 처음 쓴 등록만 남기고, 재시작 때마다 하는 재등록은 남기지 않는다.

기록하는 동작 이름은 다음과 같다.

| 분류 | 동작 |
| --- | --- |
| 세션 | `session.login`, `session.login_failed`, `session.second_factor`, `session.second_factor_failed`, `session.logout`, `setup.admin_create` |
| 터널 | `connector.create`, `connector.delete`, `connector.describe`, `connector.rename`, `connector.token_issue`, `connector.token_reset`, `connector.enroll`, `connector.enroll_failed` |
| 서비스·공개 주소 | `service.create`, `service.update`, `service.delete`, `route.create`, `route.delete` |
| 도메인·터널 주소 | `domain.set`, `domain.add`, `domain.remove`, `domain.mode`, `tunnel.update` |
| 알림·보안 | `telegram.update`, `telegram.test`, `telegram.delete`, `alert_rules.update`, `second_factor.update`, `security.rate_limit`, `security.connection_limits`, `security.tunnel_guard`, `security.block_rules`, `security.client_bans`, `security.client_ban_release`, `security.sso`, `security.sso_reset` |

**요청 제한(rate limit).** 공개 경로(`PublicHandler`)에서 프록시하기 전에, 데이터베이스를 조회하기 전에 토큰 버킷으로 제한한다. `GET`과 `PUT /api/v1/security/rate-limit`은 같은 객체를 주고받으며 `PUT`은 모든 키를 담아야 하고 범위를 벗어나면 `400 invalid_rate_limit`이다. 설정은 재시작 없이 바로 적용한다.

~~~
{
  "enabled": true,
  "per_ip":      {"rps": 300,  "burst": 1000},
  "per_subnet":  {"rps": 1500, "burst": 5000},
  "per_address": {"rps": 0,    "burst": 0},
  "endpoints": [{"host": "k7m2x9qa.example.com", "path_prefix": "/api/v1/session", "rps": 3, "burst": 30, "per": "ip"}]
}
~~~

- `per_ip`는 클라이언트 IP 하나, `per_subnet`은 IPv4 /24 또는 IPv6 /56, `per_address`는 공개 호스트 이름 하나(모든 클라이언트 합계)를 제한한다. `rps`와 `burst`가 모두 0이면 끄는 것이고, 켜려면 `rps` 1–10000, `burst`는 `rps` 이상 100000 이하여야 한다.
- `endpoints`는 최대 50개이며 `host`(호스트 이름 또는 모든 호스트를 뜻하는 `*`)와 `/`로 시작하는 `path_prefix`가 같은 요청에 적용된다. 가장 긴 접두사가 이기고, 길이가 같으면 이름이 있는 호스트가 `*`보다 우선한다. 경로는 `//`, `.`, `..`을 정리하고 소문자로 바꿔 비교한다. `per`가 `total`이면 모든 클라이언트를 합쳐, `ip`이면 클라이언트 IP마다 제한한다. 같은 `host`와 `path_prefix`는 한 번만 쓸 수 있다.
- 저장된 설정이 없으면 위 값이 기본이며 관리 호스트의 로그인 경로 규칙도 목록에 포함된다. 대표 도메인을 바꾸면 저장된 규칙의 관리 호스트도 새 이름으로 바뀐다.
- 요청은 해당하는 모든 버킷에 토큰이 있어야 통과하고, 하나라도 비어 있으면 거부한다. 거부된 요청은 다른 버킷의 토큰을 쓰지 않는다. 거부는 `429 Too Many Requests`, `Retry-After: 1`, 본문 `Too many requests (ip limit)` 형태이며 `ip`, `subnet`, `address`, `endpoint` 중 어느 제한인지 알려 준다. 프록시하지 않는다.
- 접속 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목이다. 없으면 IP·서브넷·IP별 엔드포인트 제한은 건너뛴다(모든 클라이언트가 한 버킷을 나누는 일을 피한다). Caddy가 내부 주소(Host `outlet:8080`, `X-Forwarded-For` 없음)로 보내는 `/.well-known/meerkat/cert-ask` 확인은 제한하지 않는다. 클라이언트가 Caddy를 거쳐 같은 Host를 보내면 `X-Forwarded-For`가 붙으므로 인증서 확인으로 취급하지 않는다. ACME HTTP-01 응답(`/.well-known/acme-challenge/`)은 Caddy가 직접 처리하므로 Outlet에 도달한 같은 경로는 일반 요청이다. 로컬 관리 포트는 이 제한을 거치지 않는다.
- 버킷은 10분 동안 쓰지 않으면 지우고 전체 약 10만 개를 넘으면 가장 오래 쓰지 않은 것부터 지운다.

**연결 제한(connection limits).** 요청 하나나 클라이언트 하나가 붙잡을 수 있는 자원을 제한한다. `GET`과 `PUT /api/v1/security/connection-limits`는 같은 객체를 주고받으며 `PUT`은 모든 키를 담아야 하고 범위를 벗어나면 `400 invalid_connection_limits`이다. 설정은 재시작 없이 바로 적용하고, 값이 0이면 그 제한은 꺼진다. 아래가 기본값이다.

~~~
{
  "max_upload_mb": 100,
  "per_ip_concurrent": 512,
  "total_concurrent": 4096,
  "idle_timeout_seconds": 30,
  "min_upload_kb_per_second": 1,
  "hosts": [{"host": "files.example.com", "max_upload_mb": 4096}]
}
~~~

- `max_upload_mb`(0–102400, MiB)는 서비스로 전달하는 요청 본문의 상한이다. `hosts`(최대 50개, 호스트 이름 하나와 그 상한, 0이면 제한 없음)에 있는 호스트는 그 값을 따른다. `Content-Length`가 상한을 넘으면 본문을 받기 전에 `413`과 `Connection: close`로 거부하고, 길이를 미리 알 수 없는 본문은 상한을 넘는 순간 끊고 `413`을 반환한다.
- `per_ip_concurrent`(0–1000000)는 클라이언트 주소(IPv6는 /56 단위)당 처리 중인 공개 요청 수다. 관리 호스트와 커넥터 API를 포함한 모든 공개 요청에 적용하며, 넘으면 `429`, `Retry-After: 1`, 본문 `Too many requests in progress from this address`다.
- `total_concurrent`(0–1000000)는 모든 클라이언트를 합쳐 서비스로 전달 중인 요청 수다. 처리 중인 요청 하나에 Outlet 메모리 약 170KB가 들므로 기본값 4096이면 최대 약 700MB다. 넘으면 `503`, `Retry-After: 5`다. 관리 호스트와 커넥터 API 요청은 세지 않으므로 프록시가 가득 차도 관리 화면과 터널 상태 보고는 계속 동작한다.
- `idle_timeout_seconds`(0 또는 5–3600)는 클라이언트가 보내는 본문 읽기와 클라이언트로의 응답 쓰기 각각에 주는 시간이다. 그동안 진행이 없으면 연결을 끊고, 업로드 중이었다면 `408`을 반환한다. 서비스의 응답을 기다리는 시간, 스트림 응답 중간의 쉼, keep-alive 연결의 다음 요청 대기, 업그레이드(WebSocket) 이후의 연결은 포함하지 않는다.
- `min_upload_kb_per_second`(0–100000)는 요청이 시작된 뒤 평균 업로드 속도의 하한이다. 처음 유휴 시간(유휴 제한이 꺼져 있으면 30초)을 유예한 뒤 이보다 느리면 `408`로 끊는다. 응답(다운로드)에는 적용하지 않는다.
- 본문을 끝까지 받지 않고 답한 요청(404, 429, 413, 서비스의 조기 응답 등)은 남은 본문을 기다려 비우지 않고, 응답 뒤 연결을 닫는다. 본문 일부만 보내고 멈춘 클라이언트가 제한에 잡히지 않은 채 연결을 붙잡지 못하게 하기 위해서다.
- 동시 요청 제한으로 거부한 요청은 요청 제한 거부와 함께 `rate_limited`로 집계하며 `rate_limit` 알림에는 `concurrent`(IP별)와 `capacity`(전체)로 나타난다.
- Caddy는 16KB를 넘는 요청 헤더를 거부하고 헤더를 10초 안에 받아야 한다. Caddy 자체의 본문·응답 유휴 제한은 1시간으로 두어 Outlet 설정이 먼저 적용된다.

**터널 포트 보호(tunnel guard).** rathole 서버(터널 포트, 기본 2333)는 연결한 누구에게나 토큰을 확인하기 전에 Noise 핸드셰이크(X25519)를 해 주고 자체 제한이 없어, 익명 연결이 몰리면 rathole이 마비되어 모든 터널이 끊긴다. 그래서 Outlet이 `outlet.json`의 `tunnel_listen_address`(기본 `0.0.0.0:2333`)를 직접 수신하고, 통과시킨 연결만 `tunnel_internal_address`의 rathole로 전달한다. 생략하면 기본은 수신 포트 + 1의 `127.0.0.1`이다(수신 포트가 65535면 − 1). 수신 포트 2333이면 `127.0.0.1:2334`, 2334이면 `127.0.0.1:2335`다. 내부 주소는 루프백 IP 리터럴이어야 하고 수신 포트와 달라야 하며, 직접 적은 값이 어긋나면 Outlet이 시작하지 않는다. 생성하는 `server.toml`의 `bind_addr`는 내부 주소다. rathole은 Outlet과 같은 네트워크 네임스페이스(`network_mode: service:outlet`)에서 실행하므로 Compose 파일은 그대로다. 수신 포트를 잡지 못하면(업그레이드 직후 이전 rathole이 아직 포트를 쥐고 있는 경우) 경고를 남기고 1초부터 30초까지 간격을 늘려 다시 시도하며, Outlet은 멈추지 않는다. 통과시킨 연결은 내부 주소로 연결하고(제한 5초) 양방향으로 복사한다. 한쪽의 데이터 끝은 `CloseWrite`로 반대쪽에 전달(half-close)하고, 오류가 나면 양쪽을 닫는다. rathole 쪽이 먼저 끝나면(닫거나 half-close) 클라이언트에게 마무리할 시간 5초를 주고, 그때까지 끝내지 않으면(클라이언트 쪽 읽기 기한) 양쪽을 닫는다. 그 밖의 방향이나 살아 있는 연결에는 시간 제한을 두지 않는다. TCP keepalive를 켠다. Outlet을 종료하면 수신을 멈추고 전달 중인 연결을 모두 닫는다.

`GET /api/v1/security/tunnel-guard`(관리자)는 설정, Outlet이 시작한 뒤의 통계, 알고 있는 Connector 주소를 반환한다.

~~~
{
  "settings": {"mode": "limited", "allowed": ["203.0.113.0/24"]},
  "stats": {"active": 3, "accepted": 120, "refused": {"rate": 0, "busy": 0, "idle": 2, "strict": 0}},
  "connectors": [{"client": "203.0.113.7", "last_seen": "2026-10-27T09:00:00Z"}]
}
~~~

- `PUT`은 `{"mode", "allowed"}`를 모두 담아야 하고 같은 객체를 반환한다. `mode`는 `limited`(기본)나 `strict`이며 `allowed`는 IP 주소나 CIDR 범위 최대 50개다. 저장할 때 소문자로 바꾸고, 범위의 호스트 비트는 지우고, 단일 주소(`/32`, `/128`)는 주소로, IPv4-mapped IPv6는 IPv4로 정리하며 중복은 한 번만 남긴다(입력 순서는 유지한다). 형식이 맞지 않거나 보낸 항목이 50개를 넘으면 `400 invalid_tunnel_guard`다. 설정은 재시작 없이 새 연결부터 바로 적용하며 감사 로그 `security.tunnel_guard`에 `{mode, allowed}`를 남긴다.
- 클라이언트의 단위는 IPv4 주소 하나나 IPv6 /64다. `connectors[].client`는 IPv4 주소 또는 `2001:db8:0:9::/64` 같은 접두사이고 `last_seen`은 RFC 3339(UTC)이며 최근에 본 것이 앞이다.
- **아는 클라이언트**는 인증된 Connector API 요청(`/connector/v1/enrollment` 성공, `/connector/v1/config`, 자격 증명이 확인된 `/connector/v1/heartbeat`)을 최근 15분 안에 보낸 주소와 `allowed`의 주소·범위다. 인증에 실패한 요청은 주소를 알게 하지 않는다. 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목이고, 로컬 관리 포트는 TCP 상대 주소다. 아는 주소 표는 4096개로 제한하며 가장 오래전에 본 것부터 지운다. 아는 클라이언트는 한도 없이 통과시키되 클라이언트당 동시 65536개의 안전 상한만 두고, 넘으면 `busy`로 거부한다. Outlet이 한 Connector에 열 수 있는 연결(동시 요청 4096개 기본값과 유휴 keep-alive 연결, 서비스마다 미리 만드는 약 9개의 채널)보다 충분히 크게 잡은 값이며 조정하는 값이 아니다.
- **모르는 클라이언트**는 `limited`에서 클라이언트당 분당 새 연결 120개(토큰 버킷, 버스트 120)와 동시 32개, 모두 합쳐 동시 256개와 초당 새 연결 50개(버스트 100)까지 받는다. 어느 한도든 넘으면 연결을 즉시 닫는다(RST). 연결한 뒤 10초 안에 핸드셰이크의 첫 메시지 전체를 보내야 한다. 메시지는 길이(2바이트, 리틀 엔디언)와 그 길이만큼의 본문이며(Noise_NK의 첫 메시지는 본문 48바이트), 길이는 1–1024여야 한다. 길이가 범위 밖이면 즉시 닫고(RST) 어느 거부 항목에도 세지 않는다. 10초 안에 다 받지 못하면 `idle`로 거부하며(RST), 한 바이트나 일부만 보내고 멈춘 경우도 같다. 어느 쪽이든 rathole에 연결하지 않는다. rathole 서버는 연결마다 핸드셰이크를 accept 루프 안에서 차례로 처리하고 중간에 멈춘 연결을 최대 5초 기다리므로, 메시지를 다 받기 전에 넘기면 연결 몇 개로 모든 터널의 새 연결을 막을 수 있다. 메시지 전체를 받은 뒤에 rathole에 연결해 길이와 본문을 한 번에 넘기고, 그 뒤의 바이트는 그대로 중계한다. 아무것도 보내지 않고 닫는 연결(`nc -vz`)과 메시지를 다 보내기 전에 스스로 닫는 연결은 거부로 세지 않는다.
- `strict`에서는 모르는 클라이언트를 바로 거부한다. 아는 Connector와 `allowed`만 rathole에 닿는다. 아는 주소는 인증된 Connector API 요청에서만 알아내므로, 터널 연결이 그 요청과 다른 주소에서 나가는 Connector는 모르는 클라이언트다. Outlet 도메인(Connector API를 받는 관리 호스트 이름)이 Cloudflare 같은 프록시 뒤에 있으면 Outlet은 프록시의 주소를 보고, Connector가 여러 주소로 나가면(예: 한쪽은 IPv6, 다른 쪽은 IPv4) 터널 연결의 주소를 본 적이 없을 수 있다. 이런 Connector는 `limited`에서 모르는 클라이언트의 한도를 받고 `strict`에서 거부되므로, 그 범위를 `allowed`에 넣거나 관리 호스트 이름을 프록시 없이(DNS-only) 두어야 한다.
- 한도는 동시 연결(클라이언트, 전체), 속도(클라이언트, 전체) 순으로 확인한다. 거부된 연결은 동시 연결 수도 속도 토큰도 쓰지 않는다.
- `stats.active`는 rathole로 전달 중인 연결 수, `accepted`는 통과해 rathole에 연결을 시도한 수다(모르는 클라이언트는 첫 메시지를 다 받은 뒤에 센다). `refused`는 `rate`(속도), `busy`(동시 연결), `idle`(첫 메시지를 10초 안에 다 보내지 않음), `strict`(엄격 모드)로 나눈다.
- SYN flood는 커널의 SYN 쿠키에, 회선을 채우는 공격은 호스팅 업체의 방어에 맡긴다.

**차단 규칙(block rules).** 공개 주소(서비스로 연결된 호스트)로 온 요청의 경로가 규칙의 조건 중 하나라도 일치하면 서비스로 보내기 전에 `403 Forbidden`(본문 `Forbidden`)으로 거부한다. 관리 호스트와 대표 도메인의 커넥터 API에는 적용하지 않는다. 공개 주소로 온 요청의 경로가 앱에 보내는 인코딩된 형태로 8192바이트를 넘으면 차단 규칙과 접근 정책을 확인하기 전에 `414`로 거부한다(쿼리 문자열은 세지 않는다). 조건은 경로 전체와 비교하므로, nginx·Apache 기본값처럼 길이를 묶어 정규식 비교 시간을 제한한다. `GET /api/v1/security/block-rules`는 `{"rules": [...], "hits": {"<id>": 12}, "addresses": ["app.example.com"]}`를 반환한다. `hits`는 Outlet 시작 이후 규칙별로 차단한 요청 수, `addresses`는 규칙을 한정할 수 있는 공개 주소 목록이다. `PUT`은 `{"rules": [...]}`만 받고(본문 256KiB 이하) 같은 형태로 답하며, 형식이 맞지 않으면 `400 invalid_block_rules`다. 설정은 재시작 없이 바로 적용한다.

~~~
{"rules": [{"id": "scanners", "name": "Vulnerability scanners", "enabled": true, "ban": false, "hosts": ["inc.example.com"],
            "conditions": [{"operator": "matches", "value": "\\.(php|env)$"}, {"operator": "starts_with", "value": "/cgi-bin/"}]}]}
~~~

- 규칙은 최대 50개, 규칙마다 조건 1–50개다. `name`은 1–64자, `id`는 영문·숫자·`-`·`_` 32자 이하이며 비워 보내면 서버가 정한다. `hosts`는 호스트 이름 또는 `*.example.com` 형태의 와일드카드이며(규칙마다 200개까지), `*.example.com`은 그 아래 모든 깊이의 이름에 맞고 `example.com` 자체에는 맞지 않는다. 비어 있으면 모든 공개 주소에 적용한다.
- `operator`는 `equals`(같음), `starts_with`(다음으로 시작), `ends_with`(다음으로 끝남), `contains`(포함), `matches`(정규식과 일치)다. 값은 512바이트 이하이고 제어 문자를 쓸 수 없다. `equals`와 `starts_with`는 `/`로 시작해야 한다. `matches`는 RE2 문법이며 대소문자를 구분하지 않는다(전후방 탐색과 역참조는 지원하지 않는다). 저장할 때 값의 앞뒤 공백을 지우고, 정규식이 아닌 값은 소문자로 바꾸고 `\`를 `/`로 읽으며, `equals`는 요청 경로와 같은 방식으로 정리하고, 나머지는 `/` 앞 조각(`ends_with`는 값 끝 조각도)을 요청 경로처럼 다듬고, 같은 조건은 하나만 남긴다.
- 요청 경로는 디코딩된 경로에서 `\`를 `/`로 읽고, 각 부분의 `;` 뒤와 NUL 뒤를 버리고, `//`·`.`·`..`을 정리하고, 소문자로 바꾸고, 각 조각 끝의 점과 공백을 지운 뒤(`/admin./x`는 `/admin/x`. 점과 공백뿐인 조각은 그대로) 비교한다(끝의 `/`는 없다). 모든 연산자는 끝에 `/`를 붙인 경로로도 비교하므로 `/cgi-bin/`과 `^/cgi-bin/`은 `/cgi-bin`에도 맞는다. 순서상 처음 맞는 규칙이 횟수를 센다.
- `/.well-known` 아래 경로는 어떤 규칙으로도 차단하지 않는다.
- 저장된 설정이 없으면 네 규칙이 기본이다. 스캐너만 찾는 경로로 클라이언트를 차단하는 `honeypot`과 루트의 숨김 파일(`/.` 시작)을 막는 `hidden-files`만 켜져 있고, 취약점 스캐너 경로(`.php` 등)를 막는 `scanners`와 사이트가 쓰지 않는 경로의 예시인 `unused-paths`는 실제 앱(예: Nextcloud의 `.php`)을 깨뜨릴 수 있어 꺼져 있다.
- 감사 로그에는 켜진 규칙과 꺼진 규칙의 이름만 남긴다.

**클라이언트 차단(허니팟).** 규칙의 `ban`(불리언, 기본 `false`, 빠뜨리면 `false`)이 `true`이면 일치한 요청을 `403`으로 거부하는 데서 그치지 않고 그 클라이언트를 차단한다. 차단된 클라이언트의 요청은 `minutes`분 동안 서비스로 연결된 모든 공개 주소에서 `403`(본문 `Forbidden`)이다. 관리 호스트, 대표 도메인의 커넥터 API, `/.well-known/` 아래 경로(차단 규칙과 같은 비교)에는 적용하지 않는다.
- 판정 순서: 공개 주소 찾기, 경로 길이(`414`) 확인 다음, 접근 정책보다 먼저 차단 목록을 확인하고, 그다음 차단 규칙이다. 차단 규칙은 `ban` 규칙을 순서대로 먼저, 나머지를 그다음 확인하며 처음 일치한 규칙이 `hits`를 센다. 그러므로 `ban` 규칙은 목록에서 앞선 넓은 규칙(예: 숨김 파일)보다 먼저 일치한다. 이미 차단된 클라이언트는 규칙까지 가지 않으므로 다시 차단하거나 시간을 늘리지 않는다.
- 클라이언트는 Caddy가 넘긴 주소(`X-Forwarded-For`의 마지막 항목)다. IPv4는 주소 하나, IPv6는 /64 단위다. 주소가 없거나 읽을 수 없는 요청은 규칙에 일치해도 `403`만 받고 차단하지 않는다.
- 차단 목록은 메모리에만 두므로 재시작하면 비워진다. 최대 65536건이며 가득 차면 가장 오래된 차단부터 지운다. 끝난 차단은 조회할 때, 그리고 30초마다 지운다. 차단 없이 지나가는 요청은 원자적 카운터 하나만 읽는다.
- 차단마다 `client`(IPv4 주소 또는 `2001:db8:1:2::/64` 같은 /64 접두사), `rule`(규칙 ID), 차단을 부른 요청의 `host`와 `path`(512바이트까지), `since`, `until`, `hits`(그 뒤 거부한 요청 수)를 둔다. 차단을 부른 요청 자체는 `hits`에 세지 않는다.
- 설정 `client_bans`는 `{"minutes": 60, "exempt": []}`이다. `minutes`는 1–10080(기본 60)이고 새 차단부터 적용하며, 이미 있는 차단은 `until`을 유지한다. `exempt`는 차단하지 않을 IP나 CIDR 범위(IPv4·IPv6) 50개까지다. 범위는 네트워크 주소로(`203.0.113.9/24`는 `203.0.113.0/24`), 주소 하나는 길이 없이(`203.0.113.5/32`와 `::ffff:203.0.113.5`는 `203.0.113.5`) 소문자로 정규화하고 중복을 없앤다. 제외된 클라이언트는 규칙에 일치해도 `403`만 받고, 같은 /64의 다른 주소가 받은 차단에도 걸리지 않는다. 저장하면 제외 범위가 통째로 덮는 차단은 바로 해제한다.

~~~
GET    /api/v1/security/client-bans?page=&per_page=&q=
{"settings": {"minutes": 60, "exempt": []}, "total": 1,
 "bans": [{"client": "203.0.113.9", "rule": "honeypot", "host": "app.example.com", "path": "/.env",
           "since": "2026-10-27T09:00:00Z", "until": "2026-10-27T10:00:00Z", "hits": 3}]}
~~~

- `GET`은 차단을 최근 것부터 반환한다. `total`은 `q`에 맞는, 아직 끝나지 않은 차단 수다. `q`는 `client`, `host`, `path`에서 대소문자 없이 찾는다(128바이트 이하). `page`(1부터)와 `per_page`(1–200, 기본 20)가 맞지 않으면 `400 invalid_ban_query`다.
- `PUT`은 `{"minutes", "exempt"}`를 모두 받아 저장하고 바로 적용하며 `GET`의 첫 쪽으로 답한다. 맞지 않는 값은 `400 invalid_client_bans`다(`security.client_bans`에 `{"minutes", "exempt"}`를 남긴다).
- `POST .../release`는 `{"client": "<목록의 client>"}`로 차단 하나를, `{"all": true}`로 모든 차단을 해제하고 `GET`의 첫 쪽으로 답한다. 둘 중 하나만 보내야 하며 아니면 `400 invalid_request`다. `client`는 IPv4 주소나 IPv6 /64 접두사(IPv6 주소를 보내면 그 /64)이고, 차단이 없으면 `404 ban_not_found`다(`security.client_ban_release`에 `{"client"|"all", "count"}`를 남긴다).
- 기본 `honeypot` 규칙은 스캐너만 찾는 경로를 건다: `/.env`, `/.git/`, `/.aws/`, `/.ssh/`, `/.svn/`, `/.hg/`, `/.docker/`, `/.kube/`, `/.htpasswd`, `/.ds_store`로 시작하는 경로, 깊이와 관계없이 `/.env` 또는 `/.env.<이름>`으로 끝나는 경로와 `/.git/config`·`/.git/head`로 끝나는 경로, `/wp-config.php`를 포함한 경로(`.bak`·`.old`·`~` 사본 포함). 마이그레이션 15는 저장된 규칙이 있고 `honeypot` ID가 없으며 50개 미만이면 이 규칙을 맨 앞에 넣는다. 저장된 규칙이 없으면 기본값이 적용되고, 한 번 적용한 뒤에는 지워도 다시 넣지 않는다.
- 브라우저가 보낸 요청(`Sec-Fetch-Site`나 `Referer`가 있는 요청)은 거부(`blocked`)만 하고 차단하지 않는다. 브라우저가 함정 경로를 요청하는 것은 채팅에 붙은 링크를 누르거나 주소를 직접 입력했을 때, 또는 다른 사이트가 이미지로 심었을 때뿐이며, 이때 차단하면 스캐너가 아니라 그 사람(과 같은 주소 뒤의 모두)을 막게 된다. 최근 브라우저는 모든 요청에 `Sec-Fetch-Site`를 보내고 스캐너는 두 헤더를 보내지 않는다. 위조해도 거부는 그대로다.
- 그 밖의 도구(curl 등)로 함정 경로를 요청하면 차단되므로, 관리자나 사무실처럼 직접 쓰는 주소는 `exempt`에 넣어 둘 수 있다.

**SSO(OpenID Connect).** 관리 콘솔에 OpenID Connect 공급자(Google Workspace, Microsoft Entra ID, Okta, Keycloak, Authentik 등)로 로그인한다. 공개 관리 호스트에서만 동작하고, SSH로 여는 로컬 관리 포트는 항상 비밀번호를 쓴다. 첫 관리자가 만들어지기 전에는 동작하지 않는다.

~~~
{"enabled": true, "label": "Google", "issuer": "https://accounts.google.com", "client_id": "…", "client_secret": "…",
 "scopes": ["openid", "email", "profile"], "groups_claim": "groups",
 "allowed_users": ["admin@example.com", "@example.com"], "allowed_groups": ["meerkat-admins"], "password_login": true}
~~~

- `GET /api/v1/security/sso`는 위 객체에서 `client_secret` 대신 `client_secret_set`, `client_secret_hint`(끝 4자)와, 공급자에 등록할 `redirect_uri`(`https://<관리 호스트>/api/v1/session/sso/callback`) 및 `post_logout_redirect_uri`(`https://<관리 호스트>/`)를 반환한다. `PUT`은 `client_secret`을 빼거나 비우면 저장된 값을 유지한다. 형식이 맞지 않으면 `400 invalid_sso`다. `POST /api/v1/security/sso/test`에 `{"issuer": "…"}`를 보내면 discovery 문서를 받아 확인하며, 실패하면 `502 provider_unreachable`이다. 공급자와의 통신(discovery, JWKS, 토큰 교환)은 https 리디렉션만 따르고 최대 3번까지만 따른다. 발급자 주소의 호스트는 제한하지 않으므로, 이 API와 discovery는 관리자가 지정한 주소로 Outlet이 직접 요청을 보내는 기능이다.
- `issuer`는 https URL이어야 하고 끝의 `/`는 지운다. `scopes`에는 `openid`가 항상 들어간다. 켜려면 `issuer`, `client_id`와 함께 `allowed_users` 또는 `allowed_groups`가 하나 이상 있어야 한다. 목록이 비어 있으면 공급자의 모든 계정이 들어올 수 있기 때문이다.
- 허용 판단: 채워진 목록은 모두 충족해야 한다(AND). `allowed_users`가 있으면 ID 토큰의 `email`(소문자)이 그 목록에 있거나 `@도메인` 항목과 도메인이 같아야 하고, `allowed_groups`가 있으면 `groups_claim` 클레임(문자열 또는 문자열 목록)에 그 목록의 그룹이 하나 이상 있어야 한다. 목록 안에서는 하나만 맞으면 된다. ID 토큰의 `email_verified`가 `true`(또는 문자열 `"true"`)여야 하며, 없거나 다른 값이면 거부한다. `email`이 없거나 ASCII가 아닌 문자가 들어 있어도 거부한다. Google은 ID 토큰에 그룹을 넣지 않으므로 사용자나 도메인으로 제한해야 하고, Entra ID는 테넌트 ID가 들어간 issuer(`/common`이 아닌 것)를 써야 한다.
- 흐름: 로그인 화면의 SSO 버튼은 `GET /api/v1/session/sso/start?return=/ko/`로 이동한다. Outlet은 state, nonce, PKCE(S256) 값을 만들어 state 해시를 메모리에 10분간 두고, state를 `__Host-meerkat_sso` 쿠키(HttpOnly, Secure, SameSite=Lax, `Path=/`)에 넣은 뒤 공급자로 보낸다. 진행 중인 로그인은 전체 8192건, 클라이언트 주소(IPv6는 /56 단위)당 3건까지 둔다. 한 주소에서 네 번째 로그인을 시작하면 그 주소의 가장 오래된 요청을 대신하고, 전체가 가득 차면 다른 사람의 진행 중인 로그인을 밀어내지 않고 새 요청을 `sso_busy`로 거부한다. 콜백은 쿠키와 `state`가 같고 한 번도 쓰이지 않았을 때만 code를 교환하고, ID 토큰의 서명(공급자 JWKS)·issuer·audience·만료·nonce를 확인한다.
- 관리자는 공급자 계정(`iss`, `sub`)에 묶인다. 처음 로그인할 때 같은 이메일의 관리자가 있으면 그 관리자에 공급자 계정을 연결하고, 없으면 새로 만든다. 이미 같은 공급자의 다른 계정에 묶인 관리자의 이메일로 로그인하면 `not_allowed`로 거부하고 `session.login_failed`에 `identity_mismatch`를 남긴다. 공급자에서 이메일이 바뀌어도 묶인 관리자로 로그인한다.
- 허용된 이메일에 관리자가 없으면 비밀번호 없는 관리자를 만든다. 이 관리자는 비밀번호로 로그인할 수 없고, 비밀번호 확인은 모르는 이메일과 같은 시간이 걸린다. 성공하면 비밀번호 로그인과 같은 세션·신뢰 기기 쿠키를 주고 `session.login`(`"method": "sso"`)을 남긴다. 텔레그램 보안 코드는 묻지 않는다(공급자의 다단계 인증을 쓴다). 세션 쿠키가 SameSite=Strict라서 리디렉션 대신 곧바로 `return` 경로로 이동하는 작은 페이지를 반환한다.
- 실패하면 `return` 경로에 `?sso_error=<code>`를 붙여 `303`으로 돌려보낸다. `code`는 `sso_unavailable`, `provider_unreachable`, `invalid_state`, `provider_denied`, `token_failed`, `not_allowed`, `sso_busy`이다. 공급자 discovery에 실패하면 10초 동안은 다시 요청하지 않고 `provider_unreachable`을 반환하며, 동시에 들어온 discovery는 한 번만 보낸다. 공급자 오류, 토큰 오류, 허용되지 않은 계정은 `session.login_failed`(`"method": "sso"`)로 남긴다.
- SSO로 로그인하면 ID 토큰을 `meerkat_sso_id_token` 쿠키(HttpOnly, Secure, SameSite=Strict, `Path=/api/v1/session`)에 둔다. 공개 주소에서 이 세션을 로그아웃하면(`DELETE /api/v1/session`) 공급자 discovery에 `end_session_endpoint`가 있을 때 `200 {"logout_url": "…"}`를 반환하고, 관리 화면은 그 주소(`client_id`, `id_token_hint`, `post_logout_redirect_uri` 포함)로 이동해 공급자 세션까지 끝낸다. 그 밖의 로그아웃은 기존처럼 `204`다.
- `password_login`이 `false`이면 공개 주소의 `POST /api/v1/session`은 비밀번호를 확인하지 않고 `403 password_login_disabled`다. SSO를 끄면 `password_login`은 항상 `true`가 된다.
- 설정은 환경 변수(`MEERKAT_OIDC_*`, `MEERKAT_ADMIN_EMAILS`, `MEERKAT_ADMIN_GROUPS`, `MEERKAT_PASSWORD_LOGIN`)로도 줄 수 있다. 환경 변수에 SSO가 있으면 그 값이 관리 화면에서 저장한 값보다 우선하고, `PUT`과 `DELETE /api/v1/security/sso`는 `409 sso_from_environment`로 거부한다. 잘못 저장해 모든 관리자가 로그인하지 못하는 일을 막기 위해서다. 환경 변수가 없으면 `PUT`으로 저장하고 `DELETE`로 저장한 값을 지운다(`security.sso_reset`). `GET`은 `source`(`database`, `environment`, `default`)와 환경 변수에 설정이 있는지를 뜻하는 `environment`를 함께 반환한다. 환경 변수로 SSO가 켜져 있으면 공개 주소에서는 관리자가 없어도 초기 설정이 끝난 것으로 보고(`setup_complete: true`), 허용된 사람이 처음 로그인할 때 관리자가 된다. 공개 주소에서 `admin.token`으로 하는 초기 설정(`POST /api/v1/setup/admin`)은 `409 setup_complete`로 거부한다. 로컬 관리 포트는 관리자가 생기기 전까지 `setup_complete: false`이고 `admin.token`으로 첫 관리자를 만들 수 있다. 도메인이 없으면 공개 주소가 없어 SSO로 로그인할 수 없기 때문이다. 대표 도메인과 관리 주소는 `MEERKAT_DOMAIN`, `MEERKAT_ADMIN_HOSTNAME` 환경 변수로도 줄 수 있다. Outlet이 시작할 때 적용하며, 이때 `GET /api/v1/outlet`의 `domain_source`는 `environment`이고 `PUT /api/v1/outlet`으로 다른 도메인을 지정하면 `409 domain_from_environment`다. 도메인 추가와 DNS 레코드 방식 변경은 그대로 할 수 있다. 관리 화면에서 정한 대표 도메인은 `domain_source: "database"`다. 방식을 정하지 않은 첫 대표 도메인은 개별 호스트(`hosts`) 방식이다.
- `GET /api/v1/setup/status`는 `{"setup_complete": bool, "sign_in": {"password": bool, "sso": bool, "recovery": bool}}`를 반환해 로그인 화면이 SSO 버튼과 비밀번호 양식을 고르게 한다. 누구나 읽을 수 있으므로 SSO 공급자 이름(`label`)은 담지 않으며, 버튼은 "SSO로 로그인"으로 표시한다. 로컬 관리 포트에서는 항상 비밀번호만이고 `recovery`가 `true`다.
- 계정 복구: 로컬 관리 포트의 `POST /api/v1/setup/recover`는 `Authorization: Bearer <admin.token>`과 `{"email", "password"}`(12자 이상)를 받아 그 이메일의 관리자 비밀번호를 설정하고, 관리자가 없으면 만든다. SSO로 생성되어 비밀번호가 없는 관리자가 공급자 장애 시 접속하는 경로다. 공급자 계정 연결은 유지한다. 성공하면 `200 {"status": "recovered"}`와 `setup.admin_recover` 감사 기록, 토큰·이메일·비밀번호가 맞지 않으면 `401 invalid_setup_credential`, 주소당 분당 10회를 넘으면 `429 rate_limited`다. 공개 주소에서는 `404`다.
- 터널 서비스 포트는 Outlet 컨테이너의 루프백(`127.0.0.1`)에서만 수신한다. 로컬 관리 포트(8443)는 컨테이너의 모든 인터페이스에서 수신하므로, Outlet을 다른 스택과 공유하는 Docker 네트워크에 연결하지 않는다.

### 접근 보호

공개 주소나 그 아래 경로마다 로그인 요구, 우회, 차단을 정한다. 관리자 SSO와 별개의 OpenID Connect 클라이언트("접근 보호 SSO")로 로그인하며, 관리자가 아닌 사람도 허용할 수 있다. 로그인할 수 없는 클라이언트는 접근 토큰을 보내고, 정책은 토큰을 조건으로 둘 수 있다.

~~~
GET    /api/v1/security/access-provider
PUT    /api/v1/security/access-provider
POST   /api/v1/security/access-provider/test
POST   /api/v1/security/access/sessions/revoke
GET    /api/v1/security/access-policies
PUT    /api/v1/security/access-policies
GET    /api/v1/security/access-tokens
POST   /api/v1/security/access-tokens
PUT    /api/v1/security/access-tokens/:id
DELETE /api/v1/security/access-tokens/:id
GET    /api/v1/access/start?host=<주소>&return=<경로>&bind=<해시>
GET    /api/v1/access/callback
GET    https://<보호 주소>/.meerkat/access/finish?ticket=<티켓>
GET    https://<보호 주소>/.meerkat/access/logout
POST   https://<보호 주소>/.meerkat/access/logout
~~~

**공급자 설정**
- `GET`은 `{"enabled", "issuer", "client_id", "client_secret_set", "client_secret_hint", "scopes", "groups_claim", "session_hours", "redirect_uri", "available"}`를 반환한다. `redirect_uri`는 `https://<관리 호스트>/api/v1/access/callback` 하나이며, 모든 보호 대상 주소가 이 URI를 쓴다.
- `PUT`은 `client_secret`을 비우면 저장된 값을 유지한다. 켜려면 https 발급자와 클라이언트 ID가 필요하다. `session_hours`는 1~720(기본 24)이다. 맞지 않으면 `400 invalid_access_provider`이다. 저장하면 바로 적용하고 `security.access_provider`를 남긴다.
- `test`는 discovery 문서를 확인한다(`400 invalid_issuer`, `502 provider_unreachable`).
- `sessions/revoke`는 세션 서명 키를 바꿔 모든 보호 주소의 세션을 끝낸다(`204`, `security.access_sessions_revoke`).

**정책**
- `GET`/`PUT /api/v1/security/access-policies`는 `{"policies": [...], "addresses": [공개 주소]}`이다(`GET`은 저장된 정책이 손상되었으면 `"damaged": true`를 더한다). 정책은 `{"id", "name", "enabled", "action", "hosts", "all_paths", "conditions", "methods", "require_token", "tokens", "allow_everyone", "allowed_users", "allowed_groups", "allow_admins"}`이다.
- 정책은 목록 순서대로 확인하며, 요청에 처음 일치하는 활성화된 정책 하나가 그 요청을 정한다. 순서를 바꾸려면 목록을 그 순서로 저장한다.
- `action`은 `allow`(로그인 후 허용 대상만, 생략 시 기본), `bypass`(로그인 없이 누구나), `block`(모두 `403`)이다. `bypass`·`block`은 허용 대상을 두지 않으며 저장할 때 비운다.
- `hosts`는 하나 이상의 주소 또는 `*.도메인`이다.
- `all_paths`가 `false`이면 `conditions`가 필요하다. 조건은 차단 규칙과 같은 연산자, 값 정리, 경로 비교를 쓴다. 요청 경로는 한 번 디코딩하고 `;` 매개변수를 뺀 형태로 비교하므로, 정규식이 아닌 값에 `%XX` 인코딩이나 `;`가 있으면 `400 invalid_access_policies`이다.
- `methods`가 있으면 그 메서드의 요청에만 적용되고, 다른 메서드의 요청은 아래 정책이 정한다. 값은 `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`이며(대소문자 무관, 이 순서로 저장), `GET`은 `HEAD`도 포함한다. 요청의 메서드는 대소문자 구분 없이 비교한다(`post`도 `POST`로 보는 앱이 있다). 비거나 생략하면 모든 메서드다. 그 밖의 값은 `400 invalid_access_policies`이다.
- `require_token`이 `true`이면 그 요청에 유효한 접근 토큰(아래 **접근 토큰**)을 보낸 요청에만 적용되고, 다른 요청은 아래 정책이 정한다. `tokens`(토큰 ID)가 있으면 그 토큰만, 비면 모든 토큰을 받는다. `require_token`이 `false`이면 `tokens`는 비운다. 없는 토큰을 지정하면 `400 invalid_access_policies`이다. 동작과 함께 쓰면 `bypass`는 토큰만으로 통과, `allow`는 토큰과 허용 대상의 세션이 모두 필요, `block`은 그 토큰을 보낸 요청을 거부한다. 토큰 없는 요청을 받을 정책이 아래에 없으면 그런 요청은 보호 없이 전달되므로, 콘솔은 그 정책에 표시한다. 다른 출처의 브라우저가 부르는 경로는 preflight `OPTIONS`가 토큰 없이 오므로 그 `OPTIONS`도 열어 두어야 한다.
- 경로 조건은 Outlet에 도달한 요청에만 적용된다. 단일 페이지 앱은 화면을 브라우저 안에서 바꾸므로, 앱 전체를 보호하고 외부 클라이언트가 부르는 경로만 우회한다.
- `bypass` 조건은 경로를 여는 만큼 넓히지 않고 그대로 비교한다. 값은 대소문자를 포함해 입력한 그대로 저장하며, `\`, 빈 조각, `.`·`..` 조각, 끝나는 곳이 확실한 조각 끝의 점이나 공백이 있으면 `400 invalid_access_policies`이다. 정규식은 대소문자를 구분한다. 요청은 꼭 필요한 바이트만 대문자 `%XX`로 인코딩해 온 경로(`%2F`, `%7E`, `%2A`, 소문자 hex 등이 없음. `!`, `'`, `(`, `)`, `*`, `[`, `]` 같은 문자는 그대로 와야 한다)이고, 디코딩 뒤 `%XX`·`\`·`;`·제어 문자·UTF-8이 아닌 바이트가 없으며, 빈·`.`·`..` 조각이나 점·공백으로 끝나는 조각이 없을 때만 디코딩한 경로 그대로(끝에 `/`를 붙이지 않고, 대소문자 구분) 비교한다. 그 밖의 요청은 우회 정책에 일치하지 않는다. 쿼리 문자열은 비교하지 않으므로, 쿼리로 다른 동작을 고르는 앱은 그 경로를 우회하지 않는다.
- 앱마다 다르게 읽을 수 있는 경로는 로그인 요구·차단 정책의 조건에는 모두 일치하고 우회 정책의 조건에는 일치하지 않는 것으로 본다. 곧 인코딩되어 온 `.`·`..`(`%2e%2e`, `a%2f..`)이나 `\` 옆의 `.`·`..`, `.`·`..`이 아니면서 점과 공백으로만 된 조각, UTF-8이 아니거나 제어 문자(NUL 포함)가 든 경로다. 디코딩 뒤에도 `%XX`가 남은 경로는 한 번 더 디코딩한 경로로도 비교해 둘 중 하나가 일치하면 일치로 보며, 그 경로에 앞의 경우(UTF-8이 아니거나 제어 문자 포함)가 있으면 모호한 경로로 본다.
- 허용 판단(`allow`): `allow_everyone`이면 접근 보호 SSO로 로그인한 모든 사용자를 들이며, 이때 사용자·그룹·관리자 허용은 비운다. 아니면 사용자(이메일 또는 `@도메인`)와 그룹은 둘 다 지정하면 둘 다 맞아야 한다. `allow_admins`는 접근 보호 SSO가 인증한 이메일이 Meerkat 관리자 이메일과 같으면 추가로 허용한다(관리자 SSO의 발급자·subject와는 대조하지 않는다). 관리자인지는 이메일마다 30초 동안 기억하므로, 관리자 추가는 그 안에 반영된다. 아무도 허용하지 않는 `allow` 정책은 `400 invalid_access_policies`이다.
- 저장하면 바로 적용하고 `security.access_policies`에 순서대로 정책마다 이름, 활성화, 동작, 주소, 메서드, 토큰 조건(`require_token`, `tokens`), 허용 대상을 남긴다. 동시에 저장해도 마지막으로 저장된 정책이 적용된다.
- 저장된 정책이 검사를 통과하지 못하면(손상, 업그레이드로 바뀐 검사) Outlet은 그대로 시작하고, 활성화된 `allow`·`block` 정책이 저장 순서대로 지정한 주소의 전체 경로와 모든 메서드에, 토큰 조건과 관계없이 적용되어, `allow`는 저장된 허용 대상(이메일은 소문자로)만 들이고 `block`과 토큰이 필요하던 `allow`는 아무도 들이지 않는다. 그래서 주소마다 처음 해당하는 정책 하나만 적용되며, 같은 주소의 두 번째 `allow` 정책만 허용하던 사람은 고칠 때까지 들어가지 못한다. 경로를 열기만 하던 `bypass`는 빠지며, 그 경로는 아래 정책이 정한다. 유효한 주소가 없는 정책, ID가 없는 정책, 정책 목록이 없거나(`null`, `{}`) 읽을 수 없으면 모든 공개 주소를 닫는다. `GET`은 읽은 정책을 빈 목록은 `[]`로, 올바르지 않거나 겹치는 ID는 새 ID로 바꿔 `"damaged": true`와 함께 보여 주며, 고쳐 다시 저장하면 풀린다.

**요청 처리**
- 공개 요청에 처음 일치한 정책이 `bypass`면 그대로, `block`이면 `403`, `allow`면 그 정책이 허용하는 세션이 있어야 앱으로 전달한다. 일치하는 정책이 없으면 그대로 전달한다.
- 세션 쿠키는 `__Host-meerkat_access`(HttpOnly, Secure, SameSite=Lax)이다. 이메일, 정책에 쓰인 그룹, 주소, 만료를 Outlet 키로 HMAC 서명하며, 발급된 주소에서만 유효하다.
- 세션이 없을 때 브라우저 탐색(GET/HEAD, `Sec-Fetch-Mode: navigate` 또는 HTML 요청)은 `302`로 관리 호스트의 `access/start`로 보낸다. 그 밖의 요청은 `401`이다.
- 세션은 있지만 정책이 허용하지 않으면 `403`이며, 이 페이지의 **다시 로그인** 버튼은 `POST /.meerkat/access/logout?return=<경로>`로 로그아웃한 뒤 그 경로로 돌아가 다시 로그인하게 한다(나중에 정책에 추가된 그룹은 다시 로그인해야 적용된다). 관리자 허용 확인 중 DB 오류는 `503`이다.
- 공급자가 꺼져 있으면 `allow` 정책에 걸린 요청은 기존 세션이 있어도 `503`이고(그 정책이 받지 않는 토큰을 보내고 세션이 없는 요청은 아래 **접근 토큰**의 `401`·`403`), 시작과 `finish`도 `503`이다. 토큰이 필요한 `bypass` 정책은 공급자와 무관하게 통과한다.
- Outlet이 보여 주는 페이지(`401`, `403`, `503`, 로그아웃 등)는 모두 `Cache-Control: no-store`다.
- 앱에는 `X-Meerkat-Access-Email`로 이메일을 전달한다. 클라이언트가 보낸 헤더 중 이름이 대소문자, `_`·`-` 구분 없이 `X-Meerkat-Access`로 시작하는 것은 모두 지운다(`X_Meerkat_Access_Email`을 같은 헤더로 읽는 앱 서버가 있다).
- `__Host-meerkat_access`로 시작하는 쿠키 이름은 Outlet 전용이다. 요청에서는 이름의 앞뒤 공백과 대소문자에 관계없이 지워 앱에 전달하지 않고, 앱 응답이 이 이름으로 설정하는 `Set-Cookie`도 지운다.

정책이 판정한 요청의 지표와 기록은 아래 **관찰**에 있다.

**접근 토큰**
- 스크립트, CI, 다른 서비스처럼 로그인할 수 없는 클라이언트는 `X-Meerkat-Access-Token: <값>` 헤더를 보낸다. 값이 맞고 만료되지 않았으며 이용 범위 안이면 그 요청에 유효한 토큰이고, 그 토큰을 받는 정책(`require_token`)에 해당한다. 값의 앞뒤 공백은 무시한다. 유효하지 않은 토큰은 보내지 않은 것처럼 정책을 고른다.
- 처음 일치한 정책이 토큰을 요구하지 않는 `allow`·`block`인데 토큰을 보냈으면, 세션으로 통과하더라도 클라이언트 주소마다 분당 10건까지 `access.token_failed`로 남기며, 사유(`invalid`, `expired`, `out_of_scope`, 유효하지만 그 정책이 받지 않으면 `not_allowed`)와 값이 가리킨 토큰의 ID를 기록한다(값은 남기지 않는다). 그 `allow` 정책에 맞는 세션이 없으면 로그인으로 보내지 않고, 값이 맞지 않거나 만료된 경우 `401`, 그 밖에는 `403`이다. IPv4-mapped IPv6 대역은 IPv4 대역으로 저장한다. 저장된 토큰 중 ID가 같은 토큰이 있으면 어느 것도 유효하지 않다(새 토큰은 겹치지 않는 ID를 받는다).
- 이용 범위: 토큰은 `hosts`(주소 또는 `*.도메인`, 1~100개)에서만, `ranges`(CIDR 또는 단일 주소, 0~50개, 비면 모든 IP)가 있으면 그 대역의 클라이언트에서만 쓸 수 있다. 클라이언트 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목이므로, Caddy 앞에 CDN이나 다른 프록시가 있으면 그 프록시의 주소다. 정책이 더 넓게 허용해도 이 범위를 넘지 않는다.
- 토큰을 요구하는 정책으로 통과한 요청은 앱에 토큰 이름을 `X-Meerkat-Access-Service`로 전달한다. 토큰 헤더는 앱에 전달하지 않는다.
- `GET`은 `{"tokens": [{"id", "name", "hosts", "ranges", "created_at", "expires_at", "last_used_at", "policies"}], "addresses": [공개 주소]}`를 만든 순서로 반환한다. `expires_at`이 `""`이면 만료 없음, `last_used_at`이 `""`이면 사용 기록 없음이며, `policies`는 그 토큰을 지정했거나, 모든 토큰을 받으면서 주소가 겹치는 정책(`{"id", "name"}`)이다. 값은 저장하지 않고 SHA-256 해시만 둔다. 마지막 사용 시각은 토큰마다 1초 단위로 기록해 30초마다 저장한다.
- `POST`는 `{"name", "hosts", "ranges", "expires_days"}`(이름 1~64자, 0이면 만료 없음, 최대 3650일)를 받아 `201 {"token", "value"}`로 답한다. `value`(`<id>.<비밀값>`)는 이때만 보여 준다. 토큰은 100개까지이며, 맞지 않으면 `400 invalid_access_token`이다(`security.access_token_create`).
- `PUT`은 `{"name", "hosts", "ranges"}`로 이름과 이용 범위를 바꾼다. 값과 만료는 그대로다(`security.access_token_update`, 없으면 `404 access_token_not_found`).
- `DELETE`는 `204`다. 정책이 `tokens`에 지정한 토큰은 `409 access_token_in_use`(모든 토큰을 받는 정책은 막지 않는다), 없으면 `404 access_token_not_found`이다(`security.access_token_delete`). 삭제하거나 만료되면 바로 통과하지 못한다.

**로그인 흐름**
- 로그인은 시작한 브라우저에 묶인다. 보호 주소는 바인딩 쿠키 `__Host-meerkat_access_bind`를, 관리 호스트는 state 쿠키 `__Host-meerkat_access_state`를 둔다(둘 다 Lax, 10분). 브라우저마다 하나씩이며 이후 로그인에서도 같은 값을 쓰므로, 여러 탭에서 동시에 로그인해도 서로 끊지 않고 쿠키가 쌓이지 않는다.
- 보호 주소는 바인딩 쿠키의 해시(소문자 hex 64자)를 `bind`로 `start`에 넘긴다. `callback`은 state 쿠키가, 티켓은 바인딩 쿠키가 로그인을 시작한 브라우저의 것이어야 받으므로, 다른 사람의 로그인으로 받은 링크로는 로그인되지 않는다(로그인 CSRF 방지).
- `start`는 공개 관리 호스트에서, 정책이 보호하는 주소와 올바른 `bind`가 있을 때만 동작한다(그 밖은 `404`). nonce와 PKCE로 공급자에 보낸다.
- 한 사무실이 주소 하나를 공유하는 경우를 위해 `start`는 클라이언트 주소마다 분당 120회(`429`), 진행 중 로그인은 주소마다 512개, 전체 32768개까지 받고, 넘으면 진행 중인 로그인을 버리지 않고 새 시작을 `503`으로 거절한다(둘 다 `Retry-After`와 원래 주소 링크). 관리자 SSO와 따로 센다.
- `callback`은 ID 토큰을 검증한다. `email_verified`가 `true`이고 ASCII 이메일이어야 한다. 그 주소의 정책 중 하나라도 허용해야 1분짜리 일회용 티켓을 발급하고, 보호 주소의 `/.meerkat/access/finish`로 보낸다(`access.login`에 발급자와 subject를 남기고, 실패는 클라이언트 주소마다 분당 10건까지 `access.login_failed`로 남긴다).
- 세션에는 그 주소의 정책이 지정한 그룹만 담는다. 쿠키 크기(3800바이트)를 넘으면 같은 정책들을 충족하는 더 적은 그룹만 남긴다(여러 정책을 충족하는 그룹부터).
- `finish`는 그 주소에 발급된 티켓만 받아 세션 쿠키로 바꾸고 원래 경로로 보낸다.
- `logout`은 같은 주소에서 보낸 `POST`일 때만 세션 쿠키를 지우고, `return`이 있으면 그 경로로 보낸다(`303`). `GET`이나 다른 사이트(같은 도메인의 다른 주소 포함)에서 온 요청은 지우지 않고 확인 버튼만 보여 준다.
- `/.meerkat/access/` 아래 경로는 Outlet이 처리하며, 기본 차단 규칙보다 먼저 확인한다.

### 관찰

콘솔의 **관찰** 페이지는 접근 보호의 지표, 요청 기록과 그 통계, 접근 기록, 실패 요청, 감사 로그(위)를 모아 보여 준다.

~~~
GET    /api/v1/observe/access-metrics?unit=hour|day|month|year&from=<RFC3339>&to=<RFC3339>&tz=<IANA>
GET    /api/v1/observe/latency?unit=hour|day|month|year&from=<RFC3339>&to=<RFC3339>&tz=<IANA>
GET    /api/v1/observe/request-log?page=&per_page=&class=&host=&path=&source=&q=
PUT    /api/v1/observe/request-log
GET    /api/v1/observe/request-log/export?class=&host=&path=&source=&q=&utc_offset=
GET    /api/v1/observe/request-log/top?by=endpoint|client&period=1h|24h|7d|30d|all&sort=&host=&q=&page=&per_page=&format=csv&utc_offset=
GET    /api/v1/observe/access-log?page=&per_page=&decision=&policy=&who=&q=
PUT    /api/v1/observe/access-log
GET    /api/v1/observe/access-log/export?decision=&policy=&who=&q=&utc_offset=
GET    /api/v1/observe/failure-log?page=&per_page=&cause=&host=&q=
PUT    /api/v1/observe/failure-log
GET    /api/v1/observe/failure-log/export?cause=&host=&q=&utc_offset=
~~~

**접근 판정 지표**
- 공개 요청에 처음 일치한 정책마다 판정을 시간 단위로 센다. `passed`는 우회, 또는 정책이 받는 세션·토큰으로 앱에 전달한 요청, `challenged`는 로그인으로 보낸 요청(브라우저가 아니면 `401`), `denied`는 차단, 정책이 들이지 않는 세션·토큰, 공급자 비활성화나 관리자 확인 오류로 거부한 요청이다. 정책이 적용되지 않은 요청과 `/.meerkat/access/` 아래 경로는 세지 않는다.
- 요청은 잠금 없이 메모리에서 세고 30초마다(그리고 아래 `GET` 때) 제어 DB에 저장하며, 400일 지난 시간은 지운다. 저장하지 못한 수는 다음에 더한다.
- `GET /api/v1/observe/access-metrics`는 `GET /api/v1/usage`와 같이 `from`·`to`를 `tz`의 단위 경계로 맞춰 `{"unit", "buckets": [{"start", "passed", "challenged", "denied"}], "policies": {"<정책 ID>": {"passed", "challenged", "denied"}}}`를 반환한다. `buckets`는 범위의 모든 구간이고 `policies`는 범위 합계다(지운 정책도 남는다). 맞지 않는 범위는 `400 invalid_usage_range`이다.

**지연 시간**
- 서비스로 보낸 요청마다 걸린 시간을 둘로 나눠 잰다. `outlet`은 Outlet이 요청을 받은 때부터 서비스로 가는 연결을 얻을 때까지(요청 제한·차단·접근 보호·경로 확인, 터널 연결 열기)로 Meerkat이 더한 시간이고, `upstream`은 요청을 본문까지 다 보낸 때부터 최종 응답 헤더까지(터널 왕복과 앱의 처리)에, 재시도 전에 실패한 서비스에 쓴 시간을 더한 것이다. 클라이언트의 느린 업로드는 어느 쪽에도 넣지 않는다. `100 Continue`나 `103 Early Hints` 같은 1xx는 응답으로 치지 않으므로 그 뒤 최종 응답까지도 `upstream`이다. 앱이 본문을 다 받기 전에 답하면 헤더를 보낸 때부터 센다. Outlet에서는 터널 왕복과 앱의 처리를 나눌 수 없으므로 터널 왕복은 `upstream`에 들어간다. Outlet이 막았거나 서비스로 보내지 못한 요청은 재지 않는다. 요청 기록과 관계없이 항상 잰다.
- 요청은 잠금 없이 메모리의 시간별 히스토그램(상한 0.1·0.25·0.5·1·2.5·5·10·25·50·100·250·500ms, 1·2.5·5·10·30초와 그 너머)에 더하고, 접근 판정 지표와 함께 30초마다(그리고 아래 `GET` 때) 제어 DB의 `latency_hourly`에 저장하며 400일 지난 시간은 지운다.
- `GET /api/v1/observe/latency`는 접근 판정 지표와 같은 범위 규칙으로 `{"unit", "buckets": [{"start", "outlet", "upstream"}], "outlet", "upstream"}`을 반환한다. 각 값은 `{"count", "avg_ms", "p50_ms", "p95_ms", "p99_ms"}`이고, 평균은 정확한 값, 백분위수는 히스토그램 칸 안에 고르게 퍼져 있다고 보고 추정한 값이다(마지막 칸은 30초로 센다). 센 요청이 없으면 모두 0이다. 맞지 않는 범위는 `400 invalid_usage_range`이다.

**기록 공통**
- 요청 기록(`requests`), 접근 기록(`access`), 실패 요청(`failures`)은 데이터 디렉터리의 별도 SQLite 파일(`request-log.db`, `access-log.db`, `failure-log.db`)에 두어 제어 DB와 연결도 용량도 나누지 않는다.
- 기록마다 설정 `{"enabled", "max_mib", "retention_days"}`가 있다. 처음에는 꺼져 있고(256MiB, 가장 빨리 차는 요청 기록은 512MiB, 30일), `max_mib`는 16~4096, `retention_days`는 1~365이다. SQLite가 파일 크기를 `max_mib`로(`max_page_count`) 막고, 가득 차면 가장 오래된 10분의 1을 최대 1만 건까지 지우고(일상 항목부터, 아래) 다시 쓴다. 큰 파일도 한 번에 1만 건만 지우므로 쓰기가 오래 멈추지 않는다. 보관 기간이 지난 기록은 한 시간마다 지우고, 상한을 낮추면 바로 상한의 90%까지 지운 뒤 빈 페이지를 4096개씩 돌려주어 파일을 줄인다. WAL은 체크포인트 뒤 16MiB로 줄인다. 읽기가 겹쳐 SQLite가 WAL을 처음부터 다시 쓰지 못한 채 64MiB를 넘으면, 쓰기와 별도로 읽기가 없는 순간을 2초까지 찾아 비우고, 찾지 못하면 다음 쓰기 때 다시 한다. 이때 읽기는 기다리지 않는다. 256MiB를 넘으면 진행 중인 통계를 멈추고(체크포인트가 끝난 뒤 한 번 다시 실행) 새 읽기가 진행 중인 읽기의 끝을 기다리게 해 비운다. 그래서 WAL은 256MiB와 그 사이 쓰인 만큼을 넘지 않는다. 통계는 한 번에 하나씩, 1분까지 실행한다. 다른 빌드가 만든 다른 형식의 파일은 비우고 새로 시작한다.
- 쓰기는 연결 하나로, 읽기(목록, 통계, 내려받기)는 그와 별도의 연결 네 개까지로 한다. WAL이라 오래 걸리는 통계나 내려받기가 쓰기를 막지 않고, 쓰기도 읽기를 막지 않는다. 시각은 UTC로 나노초까지 고정 폭(`2006-01-02T15:04:05.000000000Z`)으로 적어 문자열 순서가 시간 순서와 같다(0.5.1이 남긴 항목은 초 단위까지만 맞고, 보관 기간이 지나면 사라진다).
- 요청은 기록을 기다리지 않는다. 기록이 꺼져 있으면 아무것도 하지 않고, 켜져 있으면 8192건짜리 대기열에 넣기만 하며 차 있으면 버린다. 백그라운드가 1초마다(또는 5000건마다) 묶어 쓰고, 쓰기에 실패하면 2만 건까지 메모리에 두었다가 최대 30초 간격으로 다시 쓰며, 넘는 것은 버린다. 디스크에 따로 쌓지 않는다.
- 항목은 **일상**(`routine: true`)이거나 **주목할 만한** 항목이다. 일상 항목은 누구나 자격 없이, 서비스가 멀쩡해도 마음대로 만들 수 있는 것(Outlet이 막은 요청, 익명의 로그인 요구)이고, 사람이나 토큰이 드러나거나 서비스에 문제가 있는 항목은 주목할 만하다. 요청을 쏟아부어 실제 실패를 밀어내거나 묻지 못하도록 두 가지를 둔다.
  - 클라이언트마다 기록별로 1분에 일상 항목은 60건, 주목할 만한 항목은 600건까지만 남긴다(요청 기록은 아래와 같이 600건, 6,000건). 앱의 5xx나 내려간 서비스로 가는 요청처럼 주목할 만한 항목도 누구나 만들 수 있기 때문이다. 클라이언트는 연결 제한과 같은 키(IPv4 주소, IPv6 /56)다. 넘는 항목은 대기열에 넣기 전에 버리고 `suppressed`로 센다. 예산은 분과 건수를 담은 `atomic.Uint64` 1024칸에 두고, 칸은 프로세스마다 무작위 시드를 쓰는 해시로 고른다. 클라이언트가 얼마나 많아도 메모리가 일정하고 잠금이 없으며, 칸을 같이 쓰는 클라이언트는 예산도 같이 쓴다(더 엄격해질 뿐이다). 접근 지표는 모든 판정을 그대로 센다.
  - 파일이 가득 차거나 상한을 넘으면 가장 오래된 일상 항목부터 지우고, 일상 항목이 남지 않았을 때만 주목할 만한 항목 중 가장 오래된 것을 지운다(요청 기록은 아래와 같이 종류와 관계없이 가장 오래된 것부터). 보관 기간은 모든 항목에 적용한다. 가득 찬 파일을 비우는 일은 쓰기 한 번(5초)과 따로 2분까지 걸릴 수 있다.
- `GET`은 `{"settings", "available", "size_bytes", "dropped", "suppressed", "total", "entries": [...]}`를 최신순으로 반환한다. `available`은 파일을 열었는지, `size_bytes`는 파일 크기, `dropped`는 Outlet 시작 이후 대기열이 차서 버린 수, `suppressed`는 같은 기간에 예산을 넘어 남기지 않은 항목 수, `total`은 조건에 맞는 수다. `page`(1부터)와 `per_page`(1~200, 기본 50)로 나누어 받는다. `q`는 128바이트까지의 텍스트 검색으로, 아래 열에서 부분 문자열을 ASCII 대소문자 구분 없이 찾고 `%`와 `_`는 문자 그대로 본다. 읽기 전에 대기 중인 기록을 쓴다. 맞지 않는 값(128바이트를 넘는 `q` 포함)은 `400 invalid_log_filter`이다.
- `PUT`은 설정을 받아 저장하고 바로 적용하며 `GET`의 첫 쪽으로 답한다. 맞지 않는 값은 `400 invalid_log_settings`, 파일을 열 수 없는데 켜려면 `409 log_unavailable`이다. `observe.log_settings`에 `{"log", "enabled", "max_mib", "retention_days"}`를 남긴다.

**접근 기록**
- 정책이 거부하거나 로그인을 요구한 요청은 모두, 통과한 요청은 로그인한 사용자나 토큰으로 들어온 것만 기록한다(익명 우회는 지표로만 센다).
- 항목은 `{"id", "at", "host", "method", "path", "client", "policy", "decision", "reason", "email", "token", "routine"}`이다. `path`는 쿼리 없이 512바이트까지, `client`는 클라이언트 IP, `policy`는 정책 ID, `token`은 토큰 ID로, 비밀값이 맞은 토큰만 적는다(ID만 맞고 비밀값이 틀린 요청은 익명·일상 항목이며 감사 로그에 ID가 남는다). `reason`은 `bypass`·`session`(통과), `no_session`(로그인 요구), `blocked`·`not_allowed`·`token_invalid`·`token_expired`·`token_out_of_scope`·`token_not_allowed`·`unavailable`(거부)이다. `decision`과 `policy`로 거르고, `q`는 `host`, `path`, `client`, `email`, `token`에서 찾는다.
- `email`도 `token`도 없는 항목은 일상(`routine: true`)이다. `who=named`는 `email`이나 `token`이 있는 항목(주목할 만한 항목)만, `who=anonymous`는 일상 항목만 거른다. 그 밖의 값은 `400 invalid_log_filter`이다.

**실패 요청**
- 경로가 있는 공개 주소로 온 요청 중 실패했거나 Outlet이 앱에 보내기 전에 막은 요청을 기록한다. 경로가 없는 주소와 접근 보호가 막은 요청(접근 기록)은 남기지 않는다. 다른 서비스로 다시 보낸 요청은 마지막 결과로 한 번만 남긴다.
- 항목은 `{"id", "at", "host", "method", "path", "client", "cause", "status", "service", "duration_ms", "routine"}`이며 `cause`와 `host`로 거르고, `q`는 `host`, `path`, `client`, `service`에서 찾는다. `cause`는 `app_error`(앱이 5xx로 응답), `unreachable`(서비스에 닿지 못함, `502`), `timeout`(연결이나 응답 헤더 시간 초과, `504`), `no_service`(살아 있는 서비스 없음, `404`), `rate_limited`(요청 제한, `429`), `blocked`(차단 규칙, `403`), `too_long`(경로 길이, `414`), `too_large`(업로드 크기, `413`), `busy`(연결 제한, 클라이언트별 `429`·전체 `503`), `trapped`(`ban` 규칙이 거부하고 클라이언트를 차단, `403`), `banned`(이미 차단된 클라이언트, `403`)이고, `status`는 클라이언트가 받은 상태 코드, `duration_ms`는 요청을 받은 때부터 실패까지(앱의 5xx는 응답 헤더까지)다. `rate_limited`·`blocked`·`too_long`·`too_large`·`busy`·`trapped`·`banned`(Outlet이 막은 요청)는 일상(`routine: true`), 나머지(서비스에 문제가 있는 요청)는 주목할 만한 항목이다. `cause`는 두 묶음도 받는다. `refused`는 일상 항목 일곱 가지 전체이고 `service`는 `app_error`·`unreachable`·`timeout`·`no_service` 전체다. 클라이언트 쪽 오류(`400`, `408`, `499`)는 남기지 않는다. 요청 제한과 클라이언트별 연결 제한은 주소를 보기 전에 거부하므로, 경로가 있는 주소로 온 요청일 때만 남긴다.

**요청 기록**
- 경로가 있는 공개 주소로 온 요청을 결과와 관계없이 모두 기록한다. 공격 지점을 찾고 요청을 세는 데 쓴다. Outlet이 막은 요청, 접근 보호가 막은 요청도 남기며, 경로가 없는 주소, 관리 주소, Outlet 도메인의 Connector API는 남기지 않는다. 응답이 끝난 뒤(업그레이드한 연결은 연결이 끝난 뒤) 한 번 남긴다.
- 항목은 `{"id", "at", "host", "method", "path", "client", "source", "status", "class", "cause", "service", "duration_ms", "outlet_us", "upstream_us", "bytes", "agent", "routine"}`이다. `path`는 쿼리 없이 512바이트까지(UTF-8이 아닌 바이트는 U+FFFD로 바꾸어 JSON에 보이는 그대로 묶고 거른다), `source`는 한도와 통계가 클라이언트를 세는 단위(IPv4 주소, IPv6 /56), `status`는 클라이언트가 받은 상태 코드(업그레이드는 `101`, 응답 헤더 전에 클라이언트가 떠나면 `499`, 헤더 뒤에 떠나면 앱의 상태 코드와 그때까지 보낸 바이트), `class`는 그 묶음(`1xx`~`5xx`), `cause`는 Outlet이 막았거나 서비스가 실패한 이유(실패 요청의 `cause`와 같은 값, 없으면 빈 문자열), `service`는 응답했거나 마지막으로 시도한 서비스, `duration_ms`는 요청을 받은 때부터 응답이 끝날 때까지, `outlet_us`와 `upstream_us`는 그 가운데 Outlet의 시간과 서비스의 시간(위 **지연 시간**, 마이크로초, 서비스로 보내지 않은 요청은 0), `bytes`는 클라이언트에 보낸 응답 본문 바이트(주소를 보기 전에 거부한 요청 제한·클라이언트별 연결 제한은 0), `agent`는 `User-Agent` 256바이트까지다. 4xx는 일상(`routine: true`), 나머지는 주목할 만한 항목이다.
- 기록이 다른 기록보다 훨씬 많으므로 클라이언트(`source`)마다 1분에 일상 항목 600건, 주목할 만한 항목 6,000건까지 남긴다. 스캐너의 4xx는 초당 10건까지, 주소 하나 뒤의 사무실이 보내는 요청은 대부분 남는다. 누구나 2xx도 4xx만큼 쉽게 만들 수 있으므로 파일이 차면 일상 여부와 관계없이 가장 오래된 항목부터 지운다. 그래야 값싼 요청을 쏟아부어 스캐너 흔적만 먼저 밀어낼 수 없다.
- `class`(`1xx`~`5xx`), `host`, `path`(512바이트까지), `source`로 정확히 거르고, `q`는 `host`, `path`, `client`, `service`, `agent`에서 찾는다. `path`와 `source`는 통계의 행에서 해당 기록으로 갈 때 쓴다.
- `GET /api/v1/observe/request-log/top`은 기간 안의 기록을 묶어 센다. `by=endpoint`(기본)는 `host`와 `path`로, `by=client`는 `source`로 묶는다(IPv6 주소를 바꿔 가며 보내도 한 클라이언트로 센다). `period`는 `1h`, `24h`(기본), `7d`, `30d`, `all`이고, `host`(정확히)와 `q`(목록과 같은 열)로 거른다. 거르는 열이 없으면 기간의 항목만 시각 인덱스로 읽는다. 응답은 `{"settings", "available", "from", "total", "rows": [...]}`이며 `from`은 기간의 시작(`all`이면 `null`), `total`은 묶음 수다. 행은 `{"host", "path"}` 또는 `{"client"}`(`source` 값)와 `requests`, `client_errors`(4xx), `server_errors`(5xx), `distinct`(엔드포인트별이면 클라이언트(`source`) 수, 클라이언트별이면 엔드포인트 수), `last_at`(마지막 요청)이다. `sort`는 `<열>.<asc|desc>`로 열은 `requests`(기본 `requests.desc`), `client_errors`, `server_errors`, `distinct`, `last_at`이고, 같은 값은 묶음 열 순서로 놓는다. `page`, `per_page`(1~200, 기본 50)로 나누어 받는다. `format=csv`면 같은 조건의 묶음을 1만 개까지 CSV로 내려받는다. 기록이 남아 있는 만큼만 세므로 상한, 보관 기간, 클라이언트별 한도에 따라 실제보다 적을 수 있다. 맞지 않는 값은 `400 invalid_log_filter`이다.

**CSV 내려받기**
- 목록, 통계, 내려받기처럼 Outlet에 부담이 되는 읽기는 콘솔 자신의 페이지에서만 받는다. 모든 공개 주소가 관리 주소와 같은 사이트라 `SameSite=Strict` 세션 쿠키도 터널로 연결된 앱의 페이지에서 보내지므로, `Sec-Fetch-Site`가 `same-origin`이나 `none`이 아닌 요청은 `403 cross_origin_request`다(헤더를 보내지 않는 스크립트는 그대로 받는다). 내려받기는 동시에 두 개까지 하고 나머지는 차례를 기다린다.
- `…/export`와 `top?format=csv`는 목록과 같은 조건(쪽 없이)에 맞는 항목을 모두 최신순으로 `text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="meerkat-<기록>-<UTC 시각>.csv"`로 내려받는다. 스프레드시트가 UTF-8로 읽도록 BOM으로 시작한다. 콘솔은 링크로 내려받으므로 세션 쿠키로 인증한다.
- 첫 행은 열 이름이다. 기록은 `id`, `at`, 그리고 항목의 열(`routine` 제외) 순서다. `utc_offset`(UTC에서 동쪽으로 몇 분, -840~840, 기본 0)으로 시각을 `2006-01-02 15:04:05.000` 꼴로 적고, 그 열 이름에 `at (UTC+09:00)`처럼 오프셋을 적는다.
- 경로, `User-Agent` 같은 값은 클라이언트가 보낸 그대로이므로 `=`, `+`, `-`, `@`, 탭, 줄바꿈으로 시작하는 칸은 앞에 `'`를 붙여 스프레드시트가 수식으로 실행하지 않게 한다.
- 1000행씩 읽고 쓰므로 Outlet 메모리와 기록 파일의 연결을 오래 잡지 않으며, 1000행마다 쓰기 기한을 1분씩 늘려 큰 파일도 끝까지 받는다. 받기 시작한 뒤 추가된 항목은 담지 않는다. 감사 로그는 오프셋이 아니라 마지막으로 읽은 항목 다음부터 읽어 제어 DB를 오래 잡지 않는다. 기록 파일을 열 수 없으면 `409 log_unavailable`, 맞지 않는 값은 `400 invalid_log_filter`(감사 로그는 `400 invalid_audit_query`)이다. 첫 행을 보내기 전에 읽기가 실패하면 `503 log_failed`(감사 로그는 `503 persistence_failed`)로 답하고, 보낸 뒤에 실패하면 연결을 끊어 브라우저가 잘린 파일을 완성된 것처럼 저장하지 않게 한다.

### Outlet 사용량

~~~
GET /api/v1/usage?unit=minute|hour|day|month|year&from=<RFC3339>&to=<RFC3339>&tz=<IANA>
~~~

`unit` 경계에 맞춘 `from`(포함)부터 `to`(미포함)까지의 Outlet 전체 합계를 `tz` 달력 기준 구간으로 나누어 반환한다. 각 `buckets` 항목은 `start`, `requests`, `request_bytes`, `response_bytes`, `error_responses`와 Outlet이 429로 거부한 요청 수 `rate_limited`를 담는다. 같은 범위의 터널별 합계는 `connectors`에 요청 수가 많은 순서로 담기며 요청이 없는 터널은 빠진다.

~~~
{"unit": "day", "buckets": [{"start": "2026-10-26T00:00:00Z", "requests": 120, "request_bytes": 1200, "response_bytes": 9000, "error_responses": 3, "rate_limited": 0}],
 "connectors": [{"connector_id": "m_…", "name": "home-server", "requests": 120, "request_bytes": 1200, "response_bytes": 9000, "error_responses": 3}]}
~~~

터널별 합계는 경로별 시간 집계로 계산하므로 보관 기간(30일)이 지난 구간은 포함되지 않고, 삭제된 공개 주소의 기록은 빠진다. Outlet 전체 합계는 시간별로 10년, 분별로 7일 보관한다. 합계는 UTC 정시 단위(분별은 분 단위)로 저장하므로, UTC와 30분·45분 차이가 나는 시간대(인도, 네팔 등)에서는 구간 경계에 걸친 한 시간이 통째로 한쪽 구간에 들어간다. 접근 판정 지표와 지연 시간도 같다. `unit=minute`은 분 단위 구간(최대 1000개)으로 분별 합계를 나누고, 이때 터널별 합계는 범위가 걸친 시간들의 비율로 범위의 합계를 나눈 값이다(합이 그래프와 같다). 접근 판정 지표와 지연 시간은 시간별로만 보관하므로 `minute`이면 `400 invalid_usage_range`이다.

## 3. Connector 등록

~~~
POST /connector/v1/enrollment
Authorization: Bearer <one-time-token>
~~~

요청 예시:

~~~
{
  "connector_name": "server3-example",
  "connector_version": "0.1.0",
  "public_key": "<connector-public-key>"
}
~~~

성공 응답은 Connector ID, 인증 자격 증명과 설정 revision을 반환한다. 새 관리형 Connector의 인증 자격 증명은 요청에 사용한 실행 토큰과 같다. 서비스 등록 전에는 `services`가 비어 있다. 서비스 등록 후 설정 조회에서 각 대상과 터널 정보를 반환한다. 개인키는 반환하지 않는다. 응답에는 Cache-Control: no-store를 적용한다.

무효한 토큰은 401, 토큰과 Connector 이름 불일치는 403이다. 기존 일회용 등록 토큰의 재사용과 이미 등록된 Connector는 409 또는 401을 반환한다.

## 4. Connector API

~~~
GET  /connector/v1/config
POST /connector/v1/heartbeat
POST /connector/v1/apply-results
POST /connector/v1/stream
~~~

Connector 설정에는 자신에게 허용된 Service만 포함한다.

~~~
{
  "connector_id": "connector_1",
  "revision": 12,
  "services": [
    {
      "service_id": "service_1",
      "target": {"host": "app", "port": 3000},
      "tunnel": {"remote_address": "outlet.example.com:2333", "server_public_key": "<key>", "service_name": "service_1", "service_token": "<secret>"}
    }
  ]
}
~~~

heartbeat에는 Connector 버전, `service_healths`의 서비스별 health, 관찰 시각을 포함한다. apply-results는 prepared, committed, aborted, failed 중 하나를 보고한다.

한 Connector의 rathole client 설정에 여러 서비스가 들어간다. Outlet은 서비스마다 고유한 rathole server 포트를 할당하고, 공개 경로를 해당 포트로 전달한다. 기존 단일 서비스 설정 필드는 저장된 Connector 세션과 상태 파일을 읽기 위해 유지한다.

## 5. 상태와 비밀정보

- Connector 상태: pending, connected, stale, disconnected, revoked
- Service 상태: unknown, healthy, unhealthy
- Route 상태: draft, applying, active, failed, disabled
- Operation 상태: pending, preparing, activating, verifying, rolling_back, applied, failed, degraded
- 등록 토큰은 해시만 저장하고 원문은 생성 직후 한 번만 보여준다.
- Connector identity 개인키는 Connector 호스트에만 저장한다. Noise NK의 Outlet 서버 개인키는 Outlet의 권한 제한 rathole 설정에 저장하고, Connector에는 서버 공개키만 전달한다.
- Caddy의 공개 HTTPS 인증서는 Outlet에서만 관리한다. Outlet과 서비스 대상 사이에는 애플리케이션 인증서를 요구하지 않는다.
- 로그와 일반 목록·상태 API 응답에는 토큰, 개인키, 전체 서비스 payload를 넣지 않는다. 등록·token 발급·자신의 설정 조회 응답에는 해당 호출자에게 필요한 자격 증명과 service token만 HTTPS와 `Cache-Control: no-store`로 전달한다.
