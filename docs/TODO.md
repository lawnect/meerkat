# Outlet / Service Connector Gateway TODO

## 프로젝트 목표

- [ ] 외부 요청을 받는 **Outlet**과 실제 서비스를 연결하는 **Service Connector**를 분리한다.
- [ ] 서비스 서버는 인바운드 포트를 공개하지 않고 Connector의 아웃바운드 연결만 사용한다.
- [ ] 여러 지역에 Outlet을 배치할 수 있도록 지역에 종속되지 않는 구조로 설계한다.
- [ ] 여러 Connector를 등록하고 서비스 단위로 Outlet에서 라우팅한다.
- [ ] 웹 UI에서 Outlet, Connector, 서비스, 라우팅, 연결 상태를 관리한다.
- [ ] HTTP/HTTPS와 TCP 서비스를 지원한다.

## 현재 합의된 방향

- [x] WireGuard를 기본 구성요소에서 제외한다.
- [x] 서버 전체를 하나의 인트라넷으로 묶지 않는다.
- [x] 서비스별 Connector가 Outlet으로 아웃바운드 터널을 만든다.
- [x] 서비스별 Connector를 Docker Compose로 배포할 수 있게 한다.
- [x] 서비스 하나당 rathole client 하나와 터널 하나를 사용한다.
- [x] 하나의 Outlet이 여러 Connector를 처리하게 한다.
- [ ] 하나의 Connector가 여러 Outlet으로 failover하는 정책을 정의한다.
- [x] HTTP 계층(L7)과 TCP 계층(L4)을 분리해서 설계한다.
- [x] 데이터베이스 같은 TCP 서비스는 기본 비공개이며, 명시적인 ACL 없이는 노출하지 않는다.

## 확정된 MVP 결정

- [x] 첫 MVP는 Outlet 한 대에 관리 UI, API, 프록시를 함께 배포한다.
- [x] Connector별 등록·인증·heartbeat·재연결을 지원한다.
- [x] HTTP/HTTPS와 TCP를 지원한다.
- [x] 공개 UDP 프록시는 첫 MVP에서 제외한다.
- [x] DB 공개 TCP 프록시는 명시적으로 활성화한 경우에만 허용한다.
- [x] HTTPS 인증서는 첫 MVP에서 Outlet에서 종료한다.
- [x] TCP 공개 포트는 관리자가 직접 지정한다.
- [x] MVP 프록시는 Caddy(HTTP/HTTPS)와 HAProxy(TCP)로 구성한다.
- [x] 인증서 발급·갱신은 Caddy가 맡고, Outlet API는 상태를 보여준다.

## 설계 산출물

- [x] 첫 사용 흐름 정의 — [DESIGN.md](DESIGN.md)
- [x] 핵심 객체와 관계 정의 — [DESIGN.md](DESIGN.md)
- [x] HTTPS, 공개 TCP 흐름 정의 — [DESIGN.md](DESIGN.md)
- [x] 관리 API와 Connector API의 인증 방향 정의 — [API_CONTRACT.md](API_CONTRACT.md)
- [ ] 기계가 읽는 OpenAPI 스키마 작성 및 요청·응답 검증
- [x] 첫 vertical slice API의 OpenAPI 스키마와 요청 검증 — [api/openapi.json](../api/openapi.json)
- [ ] Connector 인증서·자격 증명 수명 정책 확정
- [x] 설정 적용·롤백의 기본 방향 설계 — [CONFIG_ROLLOUT.md](CONFIG_ROLLOUT.md)
- [ ] HAProxy 버전별 reload·재시작 복구 방식 실제 검증
- [ ] Connector prepare/commit/abort와 중단 후 정리 동작 구현·검증

## 먼저 결정할 아키텍처

- [x] 단일 Outlet에 관리 UI와 데이터 플레인을 함께 둔다.
- [ ] 중앙 Control Plane을 별도 서비스로 분리할지 결정한다.
- [x] 서버 단위 Inlet Agent 대신 서비스 단위 Connector를 사용한다.
- [ ] Connector 하나가 담당할 수 있는 Compose 서비스 범위 확정
- [x] MVP에서는 Connector multiplex를 사용하지 않고 서비스별 터널 하나만 사용한다.
- [ ] Outlet 장애 시 Connector 재연결·failover 동작 정의
- [ ] 하나의 Connector를 여러 Outlet에서 사용할 때의 라우팅 정책 정의
- [ ] 설정 저장소와 비밀정보 저장 방식을 결정한다.

## MVP 범위

## 구현 시작 지점

- [x] 로컬 vertical slice: Outlet rathole server와 서비스별 Connector rathole client 연결
- [ ] 실제 서버1·서버3의 Compose/방화벽 환경에서 vertical slice 검증
- [x] Connector가 rathole client 프로세스를 실행·감시·재시작
- [x] Connector 등록 API와 일회성 등록 토큰
- [x] Service를 Connector에 연결하고 HTTP 요청 전달 — 로컬 2개 서비스
- [x] Connector heartbeat와 연결 상태 표시 — 관리자 JSON API
- [ ] 첫 vertical slice 완료 후 SQLite와 Svelte UI 연결

구체적인 구현 순서와 새 세션 인수인계 내용은 [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md)를 따른다.

### Outlet과 Connector

- [ ] Outlet 설치 및 최초 관리자 생성
- [ ] Connector 생성 및 일회성 등록 토큰 발급
- [x] Docker Compose용 Connector 설정 예시 제공 — 로컬 PoC
- [ ] Connector 최초 인증 및 자격 증명 회수
- [x] Connector 연결 상태·heartbeat·재연결 상태 확인 — 로컬 API/E2E
- [x] Connector가 허용된 Compose 서비스로만 연결하는지 검증
- [ ] rathole server 설정 생성·검증·reload
- [x] 서비스별 rathole client 설정 생성 및 실행

### HTTP/HTTPS(L7)

- [ ] 도메인 기반 라우팅
- [ ] Outlet에서 TLS 종료
- [ ] Caddy ACME 발급·갱신
- [x] Connector 터널을 통한 HTTP 전달 — 로컬 2개 서비스
- [ ] WebSocket과 streaming response 동작 검증

### TCP(L4)

- [ ] 서비스별 TCP 공개 포트 지정
- [ ] HAProxy raw TCP 전달
- [ ] TCP Route별 CIDR ACL
- [ ] PostgreSQL 등 DB 연결 검증
- [ ] 연결 수·idle timeout·대역폭 제한 검토

### 보안

- [x] Connector별 일회성 등록 토큰
- [ ] Connector별 자격 증명과 폐기 상태
- [x] Outlet API TLS와 rathole Noise 터널 암호화 분리 검증 — 로컬 PoC
- [x] Connector의 대상 서비스 allowlist
- [ ] TCP 공개 기본 비활성화
- [ ] 민감정보·개인키 로그 출력 방지
- [ ] 감사 로그

### UI

- [ ] Outlet 상태
- [ ] Connector 목록·상세·연결 상태
- [ ] 서비스와 Connector 연결
- [ ] HTTP Route와 인증서 상태
- [ ] TCP Route와 ACL
- [ ] 설정 적용 작업과 오류

## 이후 범위

- [ ] 여러 Outlet과 지역별 선택
- [ ] Outlet 이중화 및 failover
- [ ] Connector 다중 Outlet failover
- [ ] 서비스별 Connector 통합 관리와 재시작 복구
- [ ] 서비스별 rate limit과 circuit breaker
- [ ] UDP 지원 검토
- [ ] 선택적 WireGuard 연동 검토
- [ ] PostgreSQL 등 외부 관리 DB 지원

## 기록해 둔 결정

- [x] 기본 네트워크 모델 — 서비스별 아웃바운드 Connector 터널
- [x] 서버 간 WireGuard 인트라넷 — 사용하지 않음
- [x] 프론트엔드 — SvelteKit/TypeScript
- [x] 백엔드·Connector — Go
- [x] MVP 관리 DB — SQLite
- [x] HTTP 프록시·인증서 — Caddy
- [x] TCP 프록시 — HAProxy
