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

`GET /api/v1/outlet`과 이 응답을 돌려주는 변경 API(`PUT /api/v1/outlet`, `POST /api/v1/outlet/domains` 등)의 본문에는 Outlet 소프트웨어 버전 `version`(예: `"0.1.0"`)이 들어 있다. 로그인 화면이 세션 없이 버전을 보여 줄 수 있도록 인증이 필요 없는 `GET /api/v1/setup/status`도 `{"setup_complete": false, "version": "0.1.0"}`처럼 같은 값을 반환한다.

Outlet 응답의 `tunnel`은 Connector가 데이터 터널에 접속하는 `address`, 직접 지정하지 않았을 때의 `default_address`, 직접 지정 여부 `custom`을 담는다. 주소는 관리 화면 설정, `outlet.json`의 `tunnel_remote_address`, `관리주소:터널수신포트` 순서로 정한다. 관리 주소(`admin_hostname`)는 대표 도메인을 처음 등록할 때 만든 8글자 무작위 이름과 대표 도메인을 합친 값이며, 그 전에 설정한 Outlet은 `meerkat`을 유지한다. `PUT /api/v1/outlet/tunnel`에 `{"address":"tunnel.example.com:2333"}`을 보내 지정하고, 빈 문자열을 보내면 기본값으로 되돌린다. 기본값이 없으면(대표 도메인과 `tunnel_remote_address`가 모두 없음) 비울 수 없으며 `409 tunnel_default_unavailable`을 반환한다. 포트를 생략하면 터널 수신 포트를 붙이며 형식이 잘못되었거나 루프백 주소이면 `400 invalid_tunnel_address`를 반환한다. 주소가 바뀌면 관리형 Connector의 모든 서비스 설정에 반영하고 revision을 올린다. 대표 도메인을 바꿔 기본 주소가 바뀔 때와 Outlet이 시작할 때도 같다. 한 Connector의 서비스는 항상 같은 터널 주소를 쓰므로 서비스를 추가할 때도 기존 서비스를 현재 주소로 맞춘다.

Outlet 응답의 `modes`는 도메인마다 DNS 방식을 담는다. `wildcard`는 `*` 레코드 하나, `hosts`는 관리 주소와 공개 주소마다 레코드를 만드는 방식이며 기본값은 `wildcard`다. `PUT /api/v1/outlet`과 `POST /api/v1/outlet/domains`에 `mode`를 함께 보낼 수 있고(대표 도메인을 바꿀 때 생략하면 기존 방식을 유지), `PUT /api/v1/outlet/domains/{domain}/mode`에 `{"mode":"hosts"}`를 보내 바꾼다. 잘못된 값은 `400 invalid_domain_mode`, 등록되지 않은 도메인은 `404 domain_not_found`다. 방식은 DNS 안내와 확인 대상만 바꾸며 라우팅과 인증서 발급은 같다. `POST /api/v1/outlet/domains/{domain}/check-dns`는 대표 도메인이면 관리 주소, `wildcard`면 임의의 하위 주소 하나, `hosts`면 그 도메인의 공개 주소마다, 그리고 그 도메인에 속한 터널 주소를 조회한다. 루트는 터널 주소일 때만 조회한다.

`POST /api/v1/outlet/tunnel/check`는 `{"address":"..."}`의 주소를, 비어 있으면 현재 터널 주소를 Outlet에서 DNS 조회한 뒤 조회된 IP마다(최대 8개) TCP 연결을 시도해 `reachable`과 IP별 `results`(`latency_ms`, `error`: `connection_refused`, `timeout`, `unreachable`, `connection_failed`)를 반환한다. DNS 조회 실패는 `error: dns_lookup_failed`로 표시한다. 응답한 서버가 이 Outlet인지와 Connector 쪽 방화벽은 확인하지 않는다. 터널 주소가 없으면 `409 tunnel_address_unset`을 반환한다.

`POST /api/v1/connectors`는 `name`과 선택 항목 `description`(관리자 메모, 줄바꿈을 제외한 제어 문자 없이 최대 500자)을 받는다. `PATCH /api/v1/connectors/{id}`에 `{"description":"..."}`나 `{"display_name":"..."}`(관리 화면에 보일 이름, 64자까지, 비우면 `name`을 보여줌)를 보내 바꾼다. 둘 다 없으면 `400 invalid_request`, 형식이 잘못되면 `400 invalid_description`/`invalid_display_name`, 없는 Connector면 `404 connector_not_found`다. `name`은 Connector 등록과 설치 명령·컨테이너 이름에 쓰이므로 바꿀 수 없다. 메모는 관리 화면에만 쓰이고 Connector 설정에는 포함되지 않는다.

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
PATCH  /api/v1/routes/:id
POST   /api/v1/routes/:id/disable
~~~

Service 생성은 외부 공개를 뜻하지 않는다. Route를 활성화해야 외부 endpoint가 생긴다.
서비스의 앱 호스트와 포트는 `PATCH /api/v1/connectors/:id/services/:service_id`에 `{"target":{"host":"app","port":3000}}`을 보내 수정한다. 한 터널에 여러 서비스를 등록할 수 있지만, 같은 터널 안에서 대상 호스트(대소문자 구분 없음)와 포트의 조합은 고유해야 한다. 생성·수정 시 중복이면 `409 service_target_taken`을 반환한다. 같은 앱의 공개 주소가 여러 개 필요하면 하나의 서비스에 Route를 여러 개 추가한다. 서비스 추가·수정·삭제 시 설정 revision을 올린다. Connector는 새 설정을 받아 Rathole에 반영하고, Outlet은 변경된 서비스의 probe 결과가 확인될 때까지 그 공개 경로만 보류한다. 변경되지 않은 서비스의 상태와 공개 경로는 유지한다.

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
GET    /api/v1/audit
~~~

오류 응답은 항상 `{"code": "...", "request_id": "..."}`이다. 아래 경로는 `GET /api/v1/alerts`부터 모두 관리자 세션이 필요하다.

**Telegram 설정.** Outlet마다 봇 하나를 쓴다. `GET /api/v1/alerts`는 다음을 반환한다. 봇 토큰은 어떤 응답에도 나오지 않고 마지막 네 글자 `token_hint`만 나온다.

~~~
{
  "telegram": {"configured": true, "enabled": true, "chat_id": "-1001234567890", "token_hint": "b-cd", "verified_at": "2026-10-26T14:00:00Z"},
  "rules": {
    "tunnel_down": {"enabled": true, "after_minutes": 5},
    "route_error": {"enabled": true},
    "dns_failure": {"enabled": true},
    "error_spike": {"enabled": true, "threshold_percent": 5, "min_requests": 50},
    "login": {"enabled": true},
    "rate_limit": {"enabled": true, "min_blocked": 500}
  },
  "events": [{"id": 12, "kind": "tunnel_down", "subject": "home-server", "message": "Tunnel home-server disconnected (5 min)",
              "opened_at": "2026-10-26T14:00:00Z", "resolved_at": null, "sent": true, "error": null}],
  "login_second_factor": false
}
~~~

- `PUT /api/v1/alerts/telegram` 본문은 `{"bot_token"?: string, "chat_id": string, "enabled": bool}`이다. `bot_token`을 생략하거나 비우면 저장된 토큰을 유지한다. 토큰 형식(`123456789:AA…`)이 틀리거나 저장된 토큰도 없으면 `400 invalid_telegram_token`, 채팅 ID가 숫자(`-100…` 포함)나 `@채널이름`이 아니면 `400 invalid_telegram_chat`이다. 토큰이나 채팅 ID를 바꾸면 `verified_at`이 지워지고, 확인된 봇을 전제로 하는 로그인 보안 코드도 함께 꺼진다. 응답은 `GET`과 같다.
- `POST /api/v1/alerts/telegram/test`는 저장된 설정으로 테스트 메시지를 보낸다. 성공하면 `verified_at`을 기록하고 `GET` 형태를 반환한다. 설정이 없으면 `409 telegram_not_configured`, Telegram이 거부하거나 닿지 않으면 `502`와 `telegram_unauthorized`(토큰이 틀림), `telegram_chat_not_found`(채팅 ID가 틀렸거나 봇이 시작·초대되지 않음), `telegram_unreachable`(네트워크·시간 초과), `telegram_failed` 중 하나를 반환한다.
- `DELETE /api/v1/alerts/telegram`은 토큰과 채팅 ID를 지우고 알림과 로그인 보안 코드를 끄며 열려 있던 알림 이벤트를 조용히 닫는다. 응답은 `GET`과 같다.
- `PUT /api/v1/alerts/rules`는 규칙 여섯 개를 모두 담은 객체로 설정을 통째로 바꾼다. 범위는 `after_minutes` 1–1440, `threshold_percent` 0.1–100, `min_requests` 1–1,000,000, `min_blocked` 1–1,000,000이며 벗어나면 `400 invalid_alert_rules`다. 키가 빠지면 `400 invalid_request`다. 응답은 `GET`과 같다.
- `PUT /api/v1/security/second-factor`는 `{"enabled": bool}`을 받아 `{"login_second_factor": bool}`을 반환한다. 켜려면 Telegram이 설정되고 테스트 메시지로 확인되어 있어야 하며 아니면 `409 telegram_not_verified`다.

**알림 규칙과 이벤트.** 알림은 Telegram이 설정되고 `enabled`일 때만 평가한다. 이벤트의 `kind`는 `tunnel_down`, `route_error`, `dns_failure`, `error_spike`, `login`, `rate_limit`이다. 같은 대상의 알림은 열려 있는 동안 다시 보내지 않고, 풀리면 복구 메시지(`tunnel_down`, `route_error`, `dns_failure`)를 한 번 보내며 `resolved_at`을 채운다. 같은 대상은 1시간 안에 다시 열지 않는다. `error_spike`, `login`, `rate_limit`은 즉시 닫히는 한 번짜리 이벤트(`resolved_at`이 `opened_at`과 같음)이다. `subject`는 터널 이름, 공개 주소, 도메인, 시간 구간(RFC 3339), 관리자 이메일 등 대상을 나타내는 짧은 문자열이다. 전송에 실패하면 `sent`가 `false`, `error`에 위 오류 코드가 남는다. 열려 있는 이벤트(`tunnel_down`, `route_error`, `dns_failure`)는 한 시간 동안 1분마다 다시 시도하고, 한 번도 알리지 못한 이벤트는 복구 메시지도 보내지 않는다. 최근 50개만 `events`에 담기며 오래된 닫힌 이벤트는 200개를 넘으면 삭제한다.

- `tunnel_down`: 등록 완료된 터널이 `after_minutes` 이상 `connected`가 아니면 알린다.
- `route_error`: 공개 주소에 `last_error`가 생기면 알린다.
- `dns_failure`: 15분마다 도메인별 필수 이름(DNS 연결 확인과 같은 목록)을 조회해 실패하면 알린다.
- `error_spike`: 현재와 직전 UTC 시간대의 5xx 비율이 `threshold_percent` 이상이고 요청이 `min_requests` 이상이면 시간대마다 한 번 알린다.
- `login`: 관리자 로그인이 성공할 때마다 이메일, 접속 주소(또는 로컬 관리 포트), 브라우저·OS 요약을 보낸다. 비동기로 보내므로 Telegram 장애가 로그인을 막거나 늦추지 않는다.
- `rate_limit`: 최근 10분 동안 거부한 요청이 `min_blocked` 이상이면 작동한 제한 종류와 상위 IP·서브넷·엔드포인트를 알린다. 계속되는 동안 30분에 한 번만 보낸다.

Telegram 메시지는 HTML(`parse_mode`)이며 이모지와 굵은 제목 한 줄, `이름: 값` 줄 두세 개, 마지막 줄의 관리 화면 링크(`https://관리주소/connectors/ID`, `/domains/도메인`, `/`)로 통일한다. 모든 동적 값은 이스케이프하고 식별자는 `<code>`로 감싼다.

**로그인 보안 코드.** `login_second_factor`가 켜져 있으면 인터넷(공개 주소)에서 온 `POST /api/v1/session`은 이메일과 비밀번호가 맞아도 세션을 만들지 않는다. 대신 6자리 코드를 Telegram으로 보내고 `202 {"second_factor": "telegram", "challenge": "<id>"}`를 반환한다. 코드는 해시만 메모리에 두며 5분 동안 유효하고 5번까지 입력할 수 있다. Telegram 전송에 실패하면 `502 telegram_unreachable`이다. 이어서 `POST /api/v1/session/second-factor`에 `{"challenge": "...", "code": "123456"}`을 보내면 기존 로그인과 같은 `201`과 세션 쿠키를 받는다. 코드가 틀렸거나 만료·모르는 challenge이면 모두 같은 `401 invalid_second_factor`이고, 다섯 번째 오답은 challenge를 폐기하며 `429 too_many_attempts`다. 두 로그인 경로 모두 클라이언트 주소당 분당 10회로 제한하며 넘으면 `429 rate_limited`와 `Retry-After: 60`을 반환한다.

SSH 터널로 접속하는 **로컬 관리 포트**의 로그인은 코드 없이 세션을 만든다. 운영자가 서버에 SSH로 들어갈 수 있으면 언제든 복구할 수 있다는 뜻이다. 요청이 공개 경로를 거쳤는지는 헤더가 아니라 Outlet이 `PublicHandler` 안에서만 요청 컨텍스트에 남기는 표시로 판단하므로 클라이언트가 위조할 수 없다. 접속 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목만 쓰고, 로컬 포트에서는 어떤 헤더도 믿지 않는다.

**감사 로그.** `GET /api/v1/audit?limit=50&offset=0&action=<접두사>&q=<텍스트>`는 최신순으로 반환한다. `limit`은 1–200(기본 50), `offset`은 0 이상이며 어긋나면 `400 invalid_audit_query`다. `action`은 동작 이름의 앞부분(`connector.` 등), `q`는 `actor_email`, `resource_id`, `detail`을 대소문자 구분 없이 부분 일치로 찾는다(128자까지).

~~~
{"events": [{"id": "m_…", "created_at": "2026-10-26T14:00:00.000Z", "actor_email": "admin@example.com",
             "action": "connector.create", "resource_type": "connector", "resource_id": "m_…",
             "source": "public", "ip": "203.0.113.7", "user_agent": "Mozilla/5.0 …", "detail": {"name": "home-server"}}],
 "total": 1}
~~~

`source`는 `public`(인터넷 경로) 또는 `local`(로컬 관리 포트)이고, `ip`는 `public`일 때만 Caddy가 전달한 접속 주소이며 그 밖에는 `null`이다. `actor_email`은 세션의 관리자이며, 관리자 계정이 생기기 전 설정 토큰으로 한 작업과 알 수 없는 시도에서는 `null`(로그인 실패는 시도한 이메일)이다. `detail`은 비밀이 아닌 필드만 담는다. 토큰, 비밀번호, 인증 코드, 봇 토큰은 기록하지 않는다. 기록은 변경이 끝난 뒤 같은 요청에서 하며, 기록에 실패하면 요청은 `503 audit_failed`가 된다(이미 적용된 변경은 되돌리지 않는다). 로그인은 기록하지 못하면 방금 만든 세션을 바로 폐기한다.

감사 로그는 추가만 가능하다. SQLite 트리거가 `audit_events`의 모든 `UPDATE`와 `DELETE`(`REPLACE` 포함)를 `audit log is append-only`로 중단하고, 삭제·수정·정리 API와 보관 기간은 없다. 데이터베이스 파일에 직접 접근할 수 있는 사람은 트리거를 지울 수 있으므로 파일 권한(0600)과 서버 접근 통제가 전제다.

기록하는 동작 이름은 다음과 같다.

| 분류 | 동작 |
| --- | --- |
| 세션 | `session.login`, `session.login_failed`, `session.second_factor`, `session.second_factor_failed`, `session.logout`, `setup.admin_create` |
| 터널 | `connector.create`, `connector.delete`, `connector.describe`, `connector.token_issue`, `connector.token_reset` |
| 서비스·공개 주소 | `service.create`, `service.update`, `service.delete`, `route.create`, `route.delete` |
| 도메인·터널 주소 | `domain.set`, `domain.add`, `domain.remove`, `domain.mode`, `tunnel.update` |
| 알림·보안 | `telegram.update`, `telegram.test`, `telegram.delete`, `alert_rules.update`, `second_factor.update`, `security.rate_limit` |

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

- `per_ip`는 클라이언트 IP 하나, `per_subnet`은 IPv4 /24 또는 IPv6 /64, `per_address`는 공개 호스트 이름 하나(모든 클라이언트 합계)를 제한한다. `rps`와 `burst`가 모두 0이면 끄는 것이고, 켜려면 `rps` 1–10000, `burst`는 `rps` 이상 100000 이하여야 한다.
- `endpoints`는 최대 50개이며 `host`(호스트 이름 또는 모든 호스트를 뜻하는 `*`)와 `/`로 시작하는 `path_prefix`가 같은 요청에 적용된다. 가장 긴 접두사가 이기고, 길이가 같으면 이름이 있는 호스트가 `*`보다 우선한다. 경로는 `//`, `.`, `..`을 정리하고 소문자로 바꿔 비교한다. `per`가 `total`이면 모든 클라이언트를 합쳐, `ip`이면 클라이언트 IP마다 제한한다. 같은 `host`와 `path_prefix`는 한 번만 쓸 수 있다.
- 저장된 설정이 없으면 위 값이 기본이며 관리 호스트의 로그인 경로 규칙도 목록에 포함된다. 대표 도메인을 바꾸면 저장된 규칙의 관리 호스트도 새 이름으로 바뀐다.
- 요청은 해당하는 모든 버킷에 토큰이 있어야 통과하고, 하나라도 비어 있으면 거부한다. 거부된 요청은 다른 버킷의 토큰을 쓰지 않는다. 거부는 `429 Too Many Requests`, `Retry-After: 1`, 본문 `Too many requests (ip limit)` 형태이며 `ip`, `subnet`, `address`, `endpoint` 중 어느 제한인지 알려 준다. 프록시하지 않는다.
- 접속 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목이다. 없으면 IP·서브넷·IP별 엔드포인트 제한은 건너뛴다(모든 클라이언트가 한 버킷을 나누는 일을 피한다). `/.well-known/meerkat/cert-ask`와 `/.well-known/acme-challenge/`는 제한하지 않으며 로컬 관리 포트는 이 제한을 거치지 않는다.
- 버킷은 10분 동안 쓰지 않으면 지우고 전체 약 10만 개를 넘으면 가장 오래 쓰지 않은 것부터 지운다.

### Outlet 사용량

~~~
GET /api/v1/usage?unit=hour|day|month|year&from=<RFC3339>&to=<RFC3339>&tz=<IANA>
~~~

`unit` 경계에 맞춘 `from`(포함)부터 `to`(미포함)까지의 Outlet 전체 합계를 `tz` 달력 기준 구간으로 나누어 반환한다. 각 `buckets` 항목은 `start`, `requests`, `request_bytes`, `response_bytes`, `error_responses`와 Outlet이 429로 거부한 요청 수 `rate_limited`를 담는다. 같은 범위의 터널별 합계는 `connectors`에 요청 수가 많은 순서로 담기며 요청이 없는 터널은 빠진다.

~~~
{"unit": "day", "buckets": [{"start": "2026-10-26T00:00:00Z", "requests": 120, "request_bytes": 1200, "response_bytes": 9000, "error_responses": 3, "rate_limited": 0}],
 "connectors": [{"connector_id": "m_…", "name": "home-server", "requests": 120, "request_bytes": 1200, "response_bytes": 9000, "error_responses": 3}]}
~~~

터널별 합계는 경로별 시간 집계로 계산하므로 보관 기간(30일)이 지난 구간은 포함되지 않고, 삭제된 공개 주소의 기록은 빠진다. Outlet 전체 합계는 10년 동안 보관한다.

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
