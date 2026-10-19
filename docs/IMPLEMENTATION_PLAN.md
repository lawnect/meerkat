# Meerkat 구현 계획 및 새 세션 인수인계

이 문서는 첫 vertical slice의 구현 순서와 당시 인수인계 기록이다. Go Outlet/Connector 코드, 로컬 Compose, 초기 OpenAPI 스키마의 설계와 검증 이력을 보존한다. 현재 설치 순서는 [README의 Outlet 초기 접속 절](../README.md#outlet-초기-접속)을 따른다. 빈 서버에서는 도메인 없이 Outlet을 시작하고 SSH 포트 포워딩으로 첫 관리자를 만든 뒤, 대표 도메인·DNS·Cloudflare token을 설정하고 Caddy를 시작한다. 실제 서버 간 운영 검증은 별도 과제로 남아 있다.

2026-10-09: 로컬 ARM64 Compose에서 두 Connector의 Noise/HTTP 전달, 등록 토큰·권한 거부, rathole crash 복구, 대상 서비스 장애 격리, Connector 재시작과 Outlet 재시작 복구를 E2E 6개 항목으로 검증했다. Go race·정적 검사, 초기 OpenAPI 검사, ARM64/AMD64 이미지 빌드도 통과했다. 테스트 컨테이너는 종료하고 `.local/poc-verified/`의 설정과 상태 파일을 보존한다. SQLite를 시작하기 전에 실제 서버 테스트 정보 또는 로컬 검증을 기준으로 다음 단계로 진행한다는 범위를 확인한다.

## 1. 프로젝트 한 줄 요약

Meerkat은 rathole을 데이터 플레인 터널 엔진으로 사용하고, 하나의 Connector 터널에 여러 서비스를 연결하는 self-hosted gateway다.

## 2. 현재 확정된 결정

### 네트워크와 역할

- 서버 전체를 WireGuard 인트라넷으로 묶지 않는다.
- 서버 간 공통 VPN도 기본 구성에 넣지 않는다.
- 서비스 서버가 먼저 Outlet으로 아웃바운드 연결을 만든다.
- 서비스 서버의 인바운드 포트와 공유기 포트포워딩은 필요하지 않다.
- 서버1은 공인 IP를 가진 Outlet이다.
- 실제 서비스가 실행되는 서버에는 터널별 Connector를 설치하고 필요한 서비스를 등록한다.

### 터널

- 터널 하나당 Connector 컨테이너 하나를 실행한다.
- 한 Connector의 rathole client 설정에 여러 서비스 항목을 넣고, Outlet에서 서비스별 전달 포트를 관리한다.
- Meerkat은 자체 암호화 터널 프로토콜을 구현하지 않는다.
- rathole client/server의 설정을 Meerkat이 생성하고 프로세스를 관리한다.
- rathole HTTP API에 의존하지 않는다. 초기에는 설정 파일과 프로세스 관리 방식을 사용한다.
- rathole transport는 TLS 또는 Noise를 사용한다.

### HTTPS와 TCP

- 외부 HTTP/HTTPS TLS는 Outlet의 Caddy에서 종료한다.
- Outlet에서 서비스까지는 애플리케이션 레벨 인증서가 필요하지 않다.
- 단, Outlet과 Connector 사이의 rathole transport는 암호화한다.
- HTTP 흐름은 Caddy → rathole server → rathole client → HTTP 서비스다.
- TCP 흐름은 HAProxy → rathole server → rathole client → TCP 서비스다.
- 공개 UDP는 MVP에서 제외한다.
- DB·Redis·SSH 같은 민감한 TCP 서비스는 기본 비공개이며 ACL 없이 공개하지 않는다.

### 기술 스택

- Backend, Outlet 제어 로직, Connector: Go
- Frontend: SvelteKit + TypeScript
- 관리 DB: SQLite
- HTTP/HTTPS 프록시와 ACME: Caddy
- TCP 프록시: HAProxy
- 터널 엔진: rathole
- 배포: Docker Compose 우선

## 3. 최종 목표 구조

~~~
                    외부 사용자
                       |
                 DNS: Outlet IP
                       |
              +--------v---------+
              | 서버1: Outlet    |
              |                  |
              | Caddy :80/:443  |
              | HAProxy :TCP    |
              | Meerkat API/UI  |
              | SQLite          |
              | rathole server  |
              +---+----------+---+
                  |          |
          rathole tunnel  rathole tunnel
                  |          |
       +----------v--+   +---v-----------+
       | 서버3         |   | 서버4         |
       | app           |   | postgres      |
       | Connector     |   | Connector     |
       | rathole client|   | rathole client|
       +---------------+   +--------------+
~~~

HTTP 예시:

~~~
사용자
  -> https://example.com
  -> 서버1 Caddy에서 TLS 종료
  -> Caddy가 localhost의 rathole server service port로 전달
  -> rathole client가 app:3000으로 전달
~~~

TCP 예시:

~~~
DB 클라이언트
  -> 서버1의 명시된 TCP 포트
  -> HAProxy ACL 검사
  -> localhost의 rathole server service port
  -> rathole client
  -> postgres:5432
~~~

## 4. 첫 구현의 범위

첫 단계는 전체 제품이 아니라 작은 vertical slice다.

### 반드시 구현할 것

1. Go Connector 프로그램의 기본 실행 구조
2. Connector 설정 파일 또는 환경변수 로딩
3. rathole client 설정 생성
4. rathole client 자식 프로세스 실행
5. rathole client 종료 감지와 제한된 재시작
6. Outlet에 등록 요청을 보내는 최소 API
7. 등록 성공 후 Connector ID와 rathole 설정 저장
8. rathole client를 통한 테스트 HTTP 서비스 연결
9. heartbeat 또는 최소 연결 상태 보고
10. 수동 설정으로 Outlet의 rathole server를 실행할 수 있는 예제

### 첫 단계에서 하지 않을 것

아래 목록은 첫 vertical slice 당시의 범위 기록이다. 현재는 Svelte UI, Caddy 자동 설정, 다중 서비스 전달이 구현되어 있다.

- Svelte UI
- 사용자 로그인
- Caddy 자동 설정
- HAProxy 자동 설정
- 다중 Outlet
- failover
- UDP
- Kubernetes
- WireGuard
- 자체 터널 프로토콜
- 여러 서비스를 하나의 Connector에서 전달
- Coolify API 연동
- 운영 서버에 자동 설치
- 인증서 자동 발급 UI

첫 vertical slice는 구성 파일과 curl로 검증해도 된다.

## 5. 첫 코드 작업 제안

처음부터 전체 도메인 모델을 만들지 말고 Connector 실행 경로부터 만든다.

권장 초기 디렉터리:

~~~
cmd/
  connector/
    main.go
internal/
  connector/
    config.go
    supervisor.go
    enrollment.go
    heartbeat.go
  rathole/
    config.go
    process.go
  transport/
    client.go
  health/
    check.go
configs/
  connector.example.yaml
deploy/
  rathole/
    server.example.toml
    client.example.toml
docs/
  ...
~~~

Go 모듈은 `github.com/lawnect/meerkat`이며 `cmd/outlet`, `cmd/connector`, `internal/`과 로컬 PoC 도구가 있다. 새 세션에서는 현재 파일 목록과 git 상태를 확인하고 이미 구현된 첫 단계를 재생성하지 않는다.

초기 구현은 다음 경계를 지킨다.

- Connector가 rathole을 직접 재구현하지 않는다.
- Connector가 임의 shell command를 실행하지 않는다.
- 실행할 rathole binary 경로와 인자는 설정에서 허용된 값만 사용한다.
- 대상 주소는 초기에는 Compose 서비스 이름 또는 loopback만 허용한다.
- 토큰과 키는 로그에 출력하지 않는다.
- child process의 stdout/stderr에는 민감정보가 포함될 수 있으므로 그대로 구조화 로그로 남기지 않는다.

## 6. 초기 Connector 설정 예시

실제 필드명은 구현하면서 확정한다. 개념적으로는 다음 정도를 사용한다.

~~~
outlet:
  enrollment_url: https://outlet.example.com/connector/v1/enrollment
  stream_address: outlet.example.com:2333

connector:
  name: server3-example
  data_dir: /var/lib/meerkat

service:
  id: service-example
  target:
    host: app
    port: 3000

rathole:
  binary: /usr/local/bin/rathole
  transport: noise
  client_retry_interval: 1s
~~~

초기에는 등록 토큰을 환경변수로 전달하되, 등록 성공 후 토큰을 삭제하거나 더 이상 사용하지 않는다. 장기 자격 증명은 data_dir의 권한 제한 파일에 저장한다.

## 7. 첫 PoC에서 rathole을 배치하는 방식

첫 PoC에서는 Outlet의 rathole server 설정을 수동으로 작성하고, Connector가 생성한 rathole client 설정과 맞춘다.

개념적 서버 설정:

~~~
[server]
bind_addr = "0.0.0.0:2333"

[server.services.example]
token = "<service-token>"
bind_addr = "127.0.0.1:31001"
~~~

개념적 클라이언트 설정:

~~~
[client]
remote_addr = "outlet.example.com:2333"

[client.transport]
type = "noise"

[client.services.example]
token = "<service-token>"
local_addr = "app:3000"
~~~

rathole 버전에 따라 설정 키와 transport 상세가 달라질 수 있으므로, 실제 구현 시 고정된 버전을 먼저 선택하고 해당 버전의 공식 예제를 기준으로 검증한다. 위 내용은 구조 설명용이다.

검증 명령 예시:

~~~
curl -H 'Host: example.com' http://127.0.0.1:31001
~~~

첫 단계에서 Caddy를 거치지 않고 rathole server의 localhost service port를 직접 확인해도 된다. rathole 연결이 검증된 뒤 Caddy를 앞에 붙인다.

## 8. 등록 흐름

초기 등록 흐름은 다음과 같다.

1. 관리자가 Outlet에서 Connector를 만든다.
2. Outlet이 짧은 수명의 일회성 등록 토큰을 생성한다.
3. 사용자가 Connector Compose 실행 시 토큰을 환경변수로 전달한다.
4. Connector가 HTTPS로 등록 요청을 보낸다.
5. Outlet이 토큰을 검증하고 Connector ID와 rathole service 설정을 반환한다.
6. Connector가 설정을 로컬에 저장한다.
7. Connector가 rathole client를 실행한다.
8. Connector가 heartbeat를 보낸다.
9. Outlet이 연결 상태를 connected로 표시한다.
10. 관리자가 Route를 활성화한다.

초기 등록 API는 우선 최소 필드만 갖는다.

요청:

~~~
POST /connector/v1/enrollment
Authorization: Bearer <one-time-token>

{
  "connector_name": "server3-example",
  "connector_version": "0.1.0",
  "public_key": "<public-key>"
}
~~~

응답에 포함할 후보:

- connector_id
- rathole server endpoint
- service_id
- service token 또는 암호화된 설정
- transport 방식
- config revision
- heartbeat endpoint
- Connector 자격 증명

토큰 원문과 개인키는 DB나 로그에 남기지 않는다.

## 9. 상태 모델

첫 구현에서 최소한 다음 상태를 구분한다.

Connector:

- pending
- enrolling
- connected
- stale
- disconnected
- revoked

rathole process:

- not_started
- starting
- running
- restarting
- stopped
- failed

Service:

- unknown
- healthy
- unhealthy

Route:

- draft
- active
- failed
- disabled

rathole process가 살아 있다는 사실만으로 Service가 healthy인 것으로 판단하지 않는다. 최소한 Connector가 대상 app:3000 연결 가능 여부를 별도로 확인한다.

## 10. 완료 조건

첫 vertical slice는 다음을 만족하면 완료다.

- 서버1에서 rathole server가 실행된다.
- 서버3의 Docker Compose에서 테스트 HTTP 서비스와 Connector가 실행된다.
- Connector가 서버1로 아웃바운드 연결을 만든다.
- 서버1의 rathole server localhost service port로 요청을 보내면 서버3의 app:3000 응답이 나온다.
- Connector를 재시작하면 자동으로 재연결된다.
- 잘못된 토큰으로 등록이 실패한다.
- 등록 토큰이 재사용되지 않는다.
- Connector heartbeat 또는 연결 상태가 Outlet에 기록된다.
- 서비스 하나를 중지하면 Connector와 Service health가 구분되어 표시된다.
- 다른 Connector가 있다면 하나의 Connector 장애가 다른 연결에 영향을 주지 않는다.

## 11. 이후 구현 순서

첫 vertical slice 다음 순서로 확장한다.

1. SQLite schema
2. Connector·Service·Route CRUD API
3. rathole server 설정 생성·검증·reload
4. Connector 설정 전달과 apply result
5. Caddy HTTP Route 자동 설정
6. Caddy ACME 상태 표시
7. HAProxy TCP Route 자동 설정
8. TCP ACL
9. SvelteKit 관리 UI
10. Compose 설치 템플릿
11. 감사 로그
12. 장애 복구와 설정 rollback
13. Coolify 연동 검토
14. 다중 Outlet과 failover

## 12. 구현 전 확인할 것

새 세션에서 코딩 전에 다음을 확인한다.

- rathole 최신 안정 버전과 라이선스
- ARM64와 AMD64 실행 이미지 지원
- rathole Noise/TLS 설정의 실제 필드명
- rathole server 설정 hot reload 방식
- rathole 프로세스 종료 시 기존 연결 처리
- Caddy를 붙이기 전 localhost 전달 테스트 방법
- 서버1의 공인 방화벽에서 rathole control port를 열 수 있는지
- 실제 테스트 서비스의 Docker Compose 네트워크 이름

## 13. 새 세션 첫 프롬프트

아래 프롬프트는 첫 vertical slice 당시의 기록이며 현재 작업 지시로 사용하지 않는다.

~~~
README.md와 docs/ 아래의 Markdown 문서를 먼저 읽고 docs/IMPLEMENTATION_PLAN.md의 4~10절을 기준으로 작업해줘.
아직 전체 UI를 만들지 말고 첫 vertical slice부터 구현해.
목표는 서버1의 rathole server와 서버3의 서비스별 Connector/rathole client를 연결해 app:3000으로 HTTP 요청을 전달하는 것이다.
먼저 현재 파일과 git 상태를 확인하고, Go module과 최소 Connector 구조를 만든 뒤 테스트와 실행 방법을 문서화해줘.
WireGuard, 다중 서비스 multiplex, Caddy 자동화, HAProxy 자동화는 이번 단계에서 구현하지 마.
~~~

## 14. 관련 문서

- 아키텍처: ARCHITECTURE.md
- 사용 흐름과 데이터 모델: DESIGN.md
- TODO: TODO.md
- API 계약: API_CONTRACT.md
- 설정 적용과 롤백: CONFIG_ROLLOUT.md
