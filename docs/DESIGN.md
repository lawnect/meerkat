# MVP 사용 흐름과 데이터 모델

이 문서는 서비스별 Connector 터널 구조의 첫 버전 사용 경험과 관리 데이터 의미를 정한다.

## 1. 범위와 구성

- Outlet 한 대에 관리 UI/API, SQLite, Caddy, HAProxy를 배치한다.
- 서비스 서버에는 Docker Compose로 앱과 service-connector를 함께 실행할 수 있다.
- Connector는 서비스마다 하나씩 실행되며, rathole client 하나와 터널 하나를 유지한다.
- HTTP/HTTPS Route는 Caddy가, 공개 TCP Route는 HAProxy가 처리한다.
- Caddy가 공개 HTTP 도메인의 ACME 인증서 발급·갱신과 외부 TLS 종료를 담당한다.
- Caddy에서 TLS를 종료하므로 Outlet과 서비스 사이에 애플리케이션 인증서는 필요하지 않다. 터널 자체는 rathole TLS 또는 Noise로 암호화한다.
- 공개 UDP 프록시는 MVP 범위 밖이다.

~~~
외부 웹 사용자 ─HTTPS─> Outlet/Caddy ─service tunnel─> Connector ─TCP─> app:3000
외부 TCP 사용자 ─TCP───> Outlet/HAProxy ─service tunnel─> Connector ─TCP─> postgres:5432
~~~

Connector는 먼저 Outlet에 연결하므로 서버의 인바운드 포트와 포트포워딩이 필요 없다. Connector의 기본 대상은 같은 Compose 네트워크의 명시된 서비스 이름이며, 임의의 외부 주소 연결은 MVP에서 허용하지 않는다.

## 2. 첫 사용자 흐름

### 2.1 Outlet 설치

1. 운영자가 Outlet 호스트에 패키지를 설치한다.
2. 설치 과정에서 한 번만 표시되는 설정 코드로 관리자 계정을 만든다.
3. Outlet의 공인 주소와 관리 UI 주소를 설정한다.
4. UI는 Caddy, HAProxy, 디스크, 인증서 저장소의 준비 상태를 표시한다.

### 2.2 서비스 Connector 등록

1. 관리자가 UI에서 Connector 이름과 연결할 Service를 만든다.
2. Outlet이 짧은 수명의 일회성 등록 토큰을 발급한다.
3. 운영자가 Compose 파일에 Connector 이미지와 토큰을 추가해 실행한다.
4. Connector는 로컬에서 키와 identity를 생성하고 HTTPS로 등록한다. 개인키는 Connector 호스트에 남는다.
5. Outlet은 토큰을 소비하고 Connector ID, 인증 자격 증명, 허용된 대상 설정을 반환한다.
6. Connector는 rathole client 하나를 실행해 Outlet에 장기 연결을 만들고 heartbeat와 대상 health를 보고한다.

등록 실패나 시간 초과 시 새 토큰을 발급한다. Connector 폐기 시 identity와 관련 Route를 비활성화한다.

### 2.3 Docker Compose 예시

~~~
services:
  app:
    image: example/app:latest

  connector:
    image: meerkat/connector:latest
    environment:
      MEERKAT_OUTLET: https://outlet.example.com
      MEERKAT_ENROLLMENT_TOKEN: <one-time-token>
      MEERKAT_TARGET: app:3000
    depends_on:
      - app
~~~

Connector는 app:3000만 대상으로 연결한다. 서비스 컨테이너에 ports를 선언하지 않아도 된다.

### 2.4 HTTPS 공개

1. 관리자가 Service에 example.com Route를 연결한다.
2. Outlet이 도메인 중복, Connector 상태, Route 설정을 검사한다.
3. Caddy 설정을 생성·검증·적용하고 ACME 인증서 발급을 시작한다.
4. Caddy가 외부 HTTPS를 종료하고 rathole 터널을 통해 app:3000으로 요청을 전달한다.

`*.도메인` DNS 레코드는 Route 하위 도메인을 Outlet으로 보내는 데 사용한다. Caddy는 등록된 정확한 Route 호스트명별로 HTTP-01 인증서를 발급·갱신하므로 Cloudflare API token은 필요하지 않다. 인증서 발급과 HTTP redirect를 위해 TCP 80, HTTPS 서비스에 TCP 443을 Outlet까지 연다.

### 2.5 TCP 공개

1. 관리자가 PostgreSQL Service에 외부 수신 포트를 지정한다.
2. 공개를 명시적으로 켜고 허용 CIDR을 입력한다.
3. Outlet이 포트 충돌·정책·HAProxy 설정을 검사한다.
4. HAProxy가 TCP 스트림을 Connector 터널로 보내고 Connector가 postgres:5432로 전달한다.

DB·Redis·SSH 같은 민감한 서비스는 기본적으로 비공개다. TCP Route가 없으면 외부에서 접근할 수 없다.

## 3. 핵심 데이터 모델

SQLite는 Outlet의 관리 데이터를 저장한다. Connector 뒤의 PostgreSQL, MySQL, Redis는 별도의 사용자 서비스다.

| 객체 | 핵심 필드 | 제약 |
| --- | --- | --- |
| Outlet | id, name, public_endpoint, created_at | MVP 설치당 1개 |
| Connector | id, name, status, version, created_at, revoked_at | 서비스 연결 구성요소 |
| ConnectorIdentity | id, connector_id, credential_fingerprint, certificate_expires_at, status | 개인키는 Connector에만 저장 |
| EnrollmentToken | id, connector_id, token_hash, expires_at, consumed_at | 일회성 |
| Service | id, connector_id, name, protocol, target_host, target_port, health_check, enabled, observed_status | target은 allowlist로 제한 |
| Route | id, service_id, kind, hostname, listen_port, access_policy_id, enabled, desired_version, applied_version, last_error | HTTP는 hostname, TCP는 listen_port 필수 |
| AccessPolicy | id, name, allowed_cidrs, public_enabled | TCP 공개는 명시적 활성화 필요 |
| CertificateStatus | hostname, status, not_after, last_checked_at, last_error | 원문 인증서·개인키 미저장 |
| ConfigRevision | id, version, candidate_digest, state, last_good, created_at, error | 마지막 정상 설정 추적 |
| Operation | id, resource_id, state, step, cleanup_pending, started_at, finished_at | 적용·복구 추적 |
| AuditEvent | id, actor_id, action, resource_type, resource_id, created_at | 비밀 값 미기록 |

## 4. 필수 제약과 상태

- 같은 Outlet에서 HTTP hostname은 활성 Route끼리 중복될 수 없다.
- TCP listen port는 관리 UI·HTTP 수신 포트와 충돌할 수 없다.
- Route의 protocol과 Service의 protocol은 일치해야 한다.
- Connector는 자신의 Service만 보고·수정할 수 있다.
- Connector는 허용된 대상 주소·포트만 연결한다.
- Connector 연결, heartbeat, Service health, Route 적용, 인증서는 각각 별도 상태다.
- Connector가 끊겨도 다른 Route와 Outlet 프로세스는 계속 동작한다.

## 5. 설정 적용과 복구

1. DB에 desired state를 저장한다.
2. Outlet 프록시 설정과 Connector 설정을 검증한다.
3. Connector가 후보 설정을 적용하고 대상 health를 보고한다.
4. Outlet이 터널 연결을 확인한다.
5. Caddy/HAProxy를 후보 설정으로 전환한다.
6. 실제 HTTP/TCP probe가 성공하면 applied state를 확정한다.
7. 실패하면 Route를 공개하지 않고 이전 정상 설정을 유지한다.

## 6. 최소 UI

- 대시보드: Outlet, Connector, 인증서, 적용 오류
- Connector 상세: 등록 토큰, 버전, 연결 상태, heartbeat, 서비스
- Service 상세: Compose 대상, health, 연결된 Route
- Route 상세: 도메인 또는 TCP 포트, ACL, 인증서·적용 상태
- 변경 이력: 설정 revision과 감사 기록

## 7. 기술 검증 완료 조건

- Outlet 1대와 서로 다른 서버의 Connector 2개가 동시에 연결된다.
- 각 Connector의 Compose HTTP 서비스가 별도 HTTPS 도메인으로 접근된다.
- Caddy가 두 도메인의 인증서를 발급·갱신한다.
- Connector 뒤의 PostgreSQL 등 TCP 서비스가 지정 포트와 ACL을 통해 연결된다.
- 잘못된 Route 설정을 제출해도 기존 정상 Route가 계속 동작한다.
- Connector 하나를 끊어도 다른 Connector의 Route가 유지된다.
