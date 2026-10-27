# MVP 사용 흐름과 데이터 모델

이 문서는 하나의 터널에 여러 서비스를 연결하는 사용 경험과 관리 데이터 의미를 정한다.

## 1. 범위와 구성

- Outlet 한 대에 관리 UI/API, SQLite, Caddy, HAProxy를 배치한다.
- 서비스 서버에는 Docker Compose로 앱과 service-connector를 함께 실행할 수 있다.
- Connector는 터널마다 하나씩 실행되며, 하나의 rathole client 설정으로 여러 서비스를 전달한다.
- 같은 터널에서는 앱 호스트와 포트의 조합을 서비스마다 고유하게 유지한다. 같은 앱을 여러 공개 주소로 노출할 때는 하나의 서비스에 주소를 여러 개 추가한다.
- HTTP/HTTPS Route는 Caddy가 TLS를 종료한 뒤 Outlet이 호스트 이름으로 선택하고, 공개 TCP Route는 HAProxy가 처리한다.
- Caddy가 공개 HTTP 도메인의 ACME 인증서 발급·갱신과 외부 TLS 종료를 담당한다.
- Caddy에서 TLS를 종료하므로 Outlet과 서비스 사이에 애플리케이션 인증서는 필요하지 않다. 터널 자체는 rathole TLS 또는 Noise로 암호화한다.
- 공개 UDP 프록시는 MVP 범위 밖이다.

~~~
외부 웹 사용자 ─HTTPS─> Caddy ─HTTP─> Outlet ─service tunnel─> Connector ─TCP─> app:3000
외부 TCP 사용자 ─TCP───> Outlet/HAProxy ─service tunnel─> Connector ─TCP─> postgres:5432
~~~

Connector는 먼저 Outlet에 연결하므로 서버의 인바운드 포트와 포트포워딩이 필요 없다. Connector의 기본 대상은 같은 Compose 네트워크의 명시된 서비스 이름이다. 앱이 같은 Linux 서버에서 직접 실행 중이면 Connector를 호스트 네트워크로 실행하고 루프백 주소를 대상으로 지정할 수 있다. 임의의 외부 주소 연결은 MVP에서 허용하지 않는다.

## 2. 첫 사용자 흐름

### 2.1 Outlet 설치

1. 운영자가 도메인 없이 Outlet을 초기화한다. 설치 도구는 로컬 HTTPS 인증서, 일회용 설정 코드와 터널 키를 서버에 생성한다.
2. Outlet 관리 포트를 서버의 `127.0.0.1`에만 연결하고 Outlet, 터널 서버, Caddy를 함께 실행한다. 도메인을 등록하기 전 Caddy는 공개 HTTPS 호스트를 설정하지 않는다.
3. 운영자는 SSH 포트 포워딩으로 `https://localhost:18443`에 접속하고, 서버의 설정 코드로 관리자 계정을 만든다. 설정 코드는 관리자 생성 후 다시 사용할 수 없다.
4. 관리 UI에서 대표 도메인과 DNS 방식(와일드카드 또는 개별 호스트)을 고른다. Outlet은 8글자 무작위 관리 주소를 정한다. 운영자는 DNS 제공자에 `*` 레코드 하나 또는 관리 주소·공개 주소별 레코드를 설정한다. 루트는 다른 서버에 둘 수 있다.
5. TCP 80·443을 외부에 연다. Caddy가 새 도메인 설정을 읽고 인증서를 발급하면 `https://<관리 주소>`로 접속한다. 대표 도메인의 루트 주소에서는 기존 터널의 API 요청만 허용한다. 로컬 관리 포트는 복구 경로로 유지한다.

구체적인 명령과 파일 경로는 [운영 가이드의 Outlet 초기 접속 절](GUIDE.ko.md#outlet-초기-접속)을 따른다.

### 2.2 터널 설치와 서비스 등록

1. 관리자가 UI에서 터널 이름을 만든다.
2. Outlet이 터널 생성 직후 재사용 가능한 실행 토큰을 발급한다. UI는 Outlet 주소·터널 이름·토큰을 설치 코드로 묶어 설치 명령만 표시한다. 토큰 단독 입력·복사 UI는 제공하지 않는다.
3. 운영자가 앱 네트워크 입력 없이 Connector를 실행한다. Connector는 Docker의 기본 네트워크에서 Outlet으로 연결한다.
4. Connector는 키와 identity를 생성하고 HTTPS로 등록한다. Outlet은 토큰을 소비하고 Connector ID와 인증 자격 증명을 반환한다. 서비스가 아직 없으면 Connector는 heartbeat를 보내며 설정을 조회하고, UI는 인증 연결 상태를 표시한다.
5. 관리자가 별도 연결 서비스 화면에서 터널을 선택하고 하나 이상의 서비스 이름과 앱 호스트·포트를 등록한다. Docker 앱에 필요하면 이 단계에서 터널 컨테이너를 앱 네트워크에 연결한다. Linux 호스트의 loopback 앱은 터널 컨테이너를 호스트 네트워크로 다시 실행한다. Outlet은 각 서비스의 대상과 전달 포트를 생성한다.
6. Connector가 설정을 받아 앱 주소를 확인하고 rathole 터널과 heartbeat를 시작한다. 그다음 공개 주소를 등록한다.

설치 코드가 유실되면 실행 토큰을 교체한다. Connector 폐기 시 실행 토큰과 관련 Route를 비활성화한다.

### 2.3 Docker Compose 예시

앱과 Connector가 같은 Compose 파일에 있다면 Connector에 실행 토큰이 들어 있는 설치 코드를 전달하고 앱 네트워크와 외부 통신 네트워크를 함께 연결한다. 관리 UI가 해당 서비스 설정을 생성한다. Connector는 컨테이너 재생성 후에도 같은 코드로 다시 연결하며 영구 볼륨은 필요하지 않다.

앱이 같은 Linux 서버에서 컨테이너 밖에 실행 중이고 `127.0.0.1:3000`에서 수신한다면, 설치 화면에서 호스트 네트워크를 선택하고 서비스의 앱 호스트를 `127.0.0.1`, 내부 포트를 `3000`으로 등록한다. 이 서비스에 Compose의 `networks`를 함께 선언하지 않는다.

### 2.4 HTTPS 공개

1. 관리자가 Service에 example.com Route를 연결한다.
2. Outlet이 도메인 중복, Connector 상태, Route 설정을 검사한다.
3. Outlet이 등록된 호스트와 Connector의 서비스 연결 상태를 저장한다. Caddy 설정은 변경하지 않는다.
4. 첫 HTTPS 요청에서 Caddy가 Outlet에 인증서 발급 허용 여부를 묻고, TLS 종료 후 Outlet으로 요청을 전달한다. Outlet이 호스트 이름에 맞는 rathole 전달 포트를 선택한다.

공개 Route는 각 호스트 이름의 인증서를 사용한다. Caddy가 HTTP-01로 인증서를 발급·갱신한다. 와일드카드 또는 호스트별 DNS는 공개 주소를 Outlet으로 보내기 위해 설정하고, 외부 TCP 80·443을 Caddy에 연결한다.
Route 변경으로 Caddy를 다시 읽지 않으므로 다른 호스트의 기존 WebSocket 연결은 유지된다.

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
- Connector 상세: 설치 명령, 버전, 연결 상태, heartbeat, 서비스
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
