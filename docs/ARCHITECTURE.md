# Outlet / Service Connector Architecture

## 1. 목표

Meerkat은 외부 요청을 받는 Outlet과 실제 서비스를 실행하는 Service Connector를 분리하는 self-hosted gateway다.

- 사용자는 Outlet에 접속한다.
- 서비스 서버는 인바운드 포트를 공개하지 않아도 된다.
- 각 터널의 Connector가 Outlet으로 아웃바운드 연결을 만든다.
- HTTP/HTTPS와 TCP 서비스를 지원한다.
- 관리 UI에서 Outlet, Connector, 서비스, 라우팅, 권한, 상태를 관리한다.
- 특정 국가나 지역에 종속되지 않는다.

WireGuard와 서버 간 공통 인트라넷은 기본 구성요소에서 제외한다. 필요한 서비스만 등록된 Connector 터널에 연결한다.

## 2. 확정된 MVP 방향

- Outlet 한 대에 UI, API, 관리 DB, 프록시를 함께 배포한다.
- 서비스 배포 시 `service-connector`를 함께 실행할 수 있는 Docker Compose 구성을 제공한다.
- Connector는 등록, 인증, 아웃바운드 터널 유지, 로컬 서비스 전달, 상태 보고를 담당한다.
- Connector는 기본적으로 같은 Compose 네트워크의 서비스로만 연결한다.
- HTTP/HTTPS와 TCP를 지원한다. 공개 UDP 프록시는 MVP에서 제외한다.
- HTTP/HTTPS 인증서는 Outlet에서 종료한다.
- Caddy가 HTTP/HTTPS 인증서를 발급·갱신하고, Outlet API가 상태·오류를 관찰한다.
- DB/Redis 같은 TCP 서비스는 기본적으로 공개하지 않는다.
- 외부 TCP 공개는 명시적으로 활성화하고 서비스별 ACL을 적용한다.
- 하나의 Connector 컨테이너에 여러 서비스를 등록할 수 있다.
- Connector의 rathole client 하나가 등록된 서비스들을 전달하며, Outlet은 서비스별 수신 포트를 구분한다.

## 3. 논리 구조

```text
                         Control Plane
                  +------------------------+
                  | Admin UI                |
                  | API                     |
                  | Config DB               |
                  | Desired-state manager   |
                  | Certificate status      |
                  +------------+-----------+
                               |
                               v
                         Data Plane
                  +------------------------+
                  | HTTP/HTTPS Proxy        |
                  | TCP Proxy               |
                  | Tunnel Registry         |
                  +------------+-----------+
                               |
                    outbound Connector tunnels
                               |
                  +------------v-----------+
                  | Service Connector       |
                  | Local service forwarder |
                  | Health/status reporting |
                  +------------+-----------+
                               |
                         app:3000 / db:5432
```

외부 요청의 기본 경로는 다음과 같다.

```text
HTTP 사용자 ─HTTPS─> Outlet/Caddy ─service tunnel─> Connector ─TCP─> app:3000
TCP 사용자 ─TCP────> Outlet/HAProxy ─service tunnel─> Connector ─TCP─> db:5432
```

Connector가 먼저 Outlet으로 연결하므로 서비스 서버의 인바운드 포트나 포트포워딩이 필요하지 않다.

## 4. 핵심 설계 원칙

### 4.1 서비스 단위 연결

서버 전체를 하나의 VPN이나 사설망으로 묶지 않는다. Connector가 담당하는 서비스와 대상 주소를 명시적으로 제한한다.

```text
example.com     -> connector-a -> app:3000
api.example.com -> connector-a -> api:8080
```

### 4.2 검증된 데이터 플레인 사용

HTTP/TCP 프록시를 직접 구현하지 않는다. 프로젝트의 핵심은 프록시 엔진이 아니라 등록, 설정, 인증, 권한, 상태 관리와 Connector 터널 관리다.

### 4.3 선언형 설정

```text
사용자 변경
  -> DB에 desired state 저장
  -> Outlet 프록시 설정과 Connector 설정 검증
  -> Connector가 준비되었는지 확인
  -> 프록시 전환·실제 연결 확인
  -> 성공하면 applied state 갱신
  -> 실패하면 이전 정상 경로 복원
```

### 4.4 Connector 최소 권한

Connector는 임의의 shell command를 실행하지 않는다. 등록, 상태 보고, 허용된 대상 연결, heartbeat처럼 명시된 typed command만 처리한다. 기본적으로 `127.0.0.1` 또는 같은 Compose 네트워크의 명시된 서비스 이름만 대상으로 허용한다.

### 4.5 실패 시 기존 정상 상태 유지

새 설정이나 Connector 연결 실패가 전체 Outlet을 중단시키면 안 된다. 새 터널을 준비하고 연결을 확인한 뒤에만 Route를 활성화한다.

## 5. 핵심 객체

```text
Outlet
  외부 트래픽을 받고 Connector 터널을 종단하는 실행 위치

Connector
  하나 이상의 서비스를 Outlet에 연결하는 실행 구성요소

ConnectorIdentity
  Connector의 고유 식별자와 인증 정보

Service
  Connector가 연결할 실제 대상 서비스
  예: app:3000, postgres:5432

Route
  외부 endpoint와 Service의 매핑
  예: app.example.com -> service-app

AccessPolicy
  IP allowlist, 인증 방식, 공개 여부

Certificate
  HTTP/HTTPS Route의 인증서 상태
```

## 6. 기술 스택

### 6.1 애플리케이션 언어: Go

Go를 API, Outlet 제어 로직, Connector에 사용한다. 네트워크 서버와 장시간 실행 프로세스, 정적 바이너리 배포에 적합하다.

### 6.2 프록시

초기 조합은 다음을 권장한다.

```text
HTTP/HTTPS: Caddy       # ACME 발급·갱신 + HTTP reverse proxy
TCP:        HAProxy     # raw TCP proxy
Tunnel:     Connector가 실행하는 rathole client의 TLS 또는 Noise 연결
```

장기적으로 HTTP와 TCP를 하나의 데이터 플레인에서 처리해야 할 필요가 커지면 Envoy를 검토한다. MVP에서 자체 프록시 엔진이나 자체 암호 프로토콜을 만들지는 않는다.

### 6.3 인증서 관리

MVP에서는 Caddy가 ACME 인증서를 발급·갱신한다. Outlet 관리 API는 도메인 설정, 인증서 상태와 실패 알림을 담당한다.

빈 서버의 초기 실행에서는 도메인과 공개 인증서가 없다. Outlet 자체는 `localhost`를 포함한 로컬 HTTPS 인증서로 시작하고, Compose는 관리 포트를 호스트의 `127.0.0.1`에만 게시한다. Caddy는 공개 호스트가 없는 설정으로 함께 실행한다. 운영자는 SSH 포트 포워딩과 서버에 저장된 일회용 설정 코드로 첫 관리자를 만든다. 대표 도메인을 관리 UI에 등록하면 Outlet이 Caddy 설정을 갱신한다. DNS를 설정하고 TCP 80·443을 열면 인증서를 발급할 수 있다. 로컬 관리 포트는 공개 인증서·DNS 장애 시에도 사용할 수 있는 복구 경로다.

HTTP Route는 Caddy가 해당 호스트 이름에 발급한 인증서를 사용한다. Caddy는 HTTP-01로 각 인증서를 발급·갱신하므로 외부 TCP 80이 Caddy까지 도달해야 한다. 공개 HTTPS 서비스에는 TCP 443도 필요하다. 와일드카드 DNS는 새 공개 주소를 Outlet으로 연결하지만 와일드카드 인증서는 사용하지 않는다. SQLite에는 인증서·개인키 원문을 저장하지 않는다.

### 6.4 Connector 터널

Connector는 rathole client를 실행해 Outlet의 rathole server로 아웃바운드 연결을 만든다.

- Connector별 자격 증명과 폐기 상태를 관리한다.
- 연결이 끊기면 지수 백오프로 재연결한다.
- Outlet은 Connector ID가 허용한 Service만 요청한다.
- HTTP 요청은 Outlet의 Caddy에서 애플리케이션 TLS를 종료한 뒤 터널로 전달한다.
- TCP 요청은 원본 바이트를 터널 스트림으로 전달한다.
- Outlet과 서비스 사이에 애플리케이션 인증서는 필요하지 않다.
- 단, 인터넷 구간의 터널 자체는 rathole TLS 또는 Noise로 암호화한다.
- 프로토콜과 자격 증명은 버전이 있는 내부 계약으로 관리한다.

WireGuard는 기본 의존성으로 사용하지 않는다. 향후 특수한 내부망 요구가 생길 경우 선택적 운영 도구로 지원할 수 있지만, Meerkat의 서비스 공개 경로에는 포함하지 않는다.

## 7. 관리 DB

Outlet 자체의 메타데이터는 SQLite에 저장한다.

- Connector, Service, Route, AccessPolicy
- 등록 토큰과 자격 증명 지문
- 설정 revision과 적용 작업
- 인증서 상태
- 감사 이벤트

사용자가 Connector 뒤에서 운영하는 PostgreSQL, MySQL, Redis는 Meerkat 관리 DB와 별개의 서비스다.

## 8. API와 UI

관리 UI는 SvelteKit/TypeScript로 구현한다. 관리 API와 Connector API는 인증·권한 모델을 분리한다.

```text
/api/v1/*        관리자 UI/API
/connector/v1/*  Connector 등록·heartbeat·설정·터널 세션
```

관리 API는 Connector가 허용된 대상만 연결하도록 선언형 설정을 제공한다. Connector는 Outlet이 보낸 전체 관리자 설정이나 다른 Connector의 정보를 받지 않는다.

## 9. 하지 않을 것

- 기본 구성에서 서버 전체를 WireGuard 인트라넷으로 묶지 않는다.
- Connector에 임의의 shell command 실행 권한을 주지 않는다.
- Outlet이 Connector의 임의 주소로 접속하게 하지 않는다.
- 인증서 개인키나 Connector 개인키를 일반 로그에 기록하지 않는다.
- MVP에서 공개 UDP 프록시를 지원하지 않는다.
- Kubernetes를 MVP에 사용하지 않는다.
