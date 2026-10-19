# API Contract 방향

이 문서는 터널별 Connector 모델의 API 방향이다. 구현된 경로와 정확한 필드는 [api/openapi.json](../api/openapi.json)에 정의한다. 일부 폐기·설정 적용·stream 경로는 아직 구현하지 않았다.

Connector별 bearer credential을 사용하며, 기본 수명은 등록 토큰 1시간·API credential 30일이다. 두 값은 설정 가능하며 자동 갱신과 mTLS는 후속 범위다. 등록 응답은 201이다. 서비스 등록 전에는 `services`가 없거나 비어 있고 Connector는 설정을 주기적으로 조회한다. 서비스 등록 후에는 각 서비스의 `service_id`, `target`, `tunnel`을 담은 `services` 배열을 반환한다.

## 1. 채널과 인증

| 경로 | 용도 | 인증 |
| --- | --- | --- |
| /api/v1/* | 관리자 UI/API | 관리자 세션 또는 관리자 API 토큰 |
| /connector/v1/enrollment | Connector 최초 등록 | 짧은 수명의 일회성 등록 토큰 + HTTPS |
| /connector/v1/* | heartbeat·설정·터널 제어 | Connector별 자격 증명 또는 mTLS |

Connector는 항상 연결을 먼저 시작한다. 공인 Outlet은 외부 HTTP/HTTPS와 등록 endpoint를 제외하고 Connector 관리 포트를 직접 공개하지 않는다.

## 2. 관리자 API

### Outlet과 Connector

~~~
GET    /api/v1/outlet
GET    /api/v1/diagnostics/trace
GET    /api/v1/connectors
POST   /api/v1/connectors
GET    /api/v1/connectors/:id
POST   /api/v1/connectors/:id/enrollment-tokens
POST   /api/v1/connectors/:id/revoke
~~~

`GET /api/v1/diagnostics/trace`는 관리자 세션 인증이 필요하다. Outlet 호스트명과 설정된 `region`, 요청 peer 주소, 전달 헤더, `CF-Ray` 및 colo 접미사를 반환한다. `region`은 자동 위치 판정이 아니라 `outlet.json`에 설정한 설명용 값이다. 전달 헤더와 `CF-Ray`는 직접 연결에서 위조될 수 있으므로 접근 제어나 인증 판단에 사용하지 않는다.

Connector 폐기는 관련 Service·Route의 새 연결을 먼저 차단한 뒤 identity를 폐기한다.

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
서비스의 앱 호스트와 포트는 `PATCH /api/v1/connectors/:id/services/:service_id`에 `{"target":{"host":"app","port":3000}}`을 보내 수정한다. 한 터널에 여러 서비스를 등록할 수 있다. 서비스 추가·수정·삭제 시 설정 revision을 올려 Connector가 저장된 identity로 재시작하고, 각 대상의 응답이 확인될 때까지 해당 공개 경로를 보류한다. 재시작 중에는 같은 터널의 다른 서비스도 잠시 중단될 수 있다.

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

성공 응답은 Connector ID, 인증 자격 증명과 설정 revision을 반환한다. 서비스 등록 전에는 `services`가 비어 있다. 서비스 등록 후 설정 조회에서 각 대상과 터널 정보를 반환한다. 개인키와 등록 토큰 원문은 다시 반환하지 않는다. 응답에는 Cache-Control: no-store를 적용한다.

토큰 만료·재사용은 401, 토큰과 Connector 불일치는 403, 이미 등록된 Connector는 409다.

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
