# Meerkat 제품 방향과 최종 목표

> 제품 수준의 기준 문서입니다. 구현 세부사항과 API 계약은 이 문서가 아니라 관련 기술 문서를 기준으로 합니다.

## 1. 한 문장 정의

Meerkat은 공개 서버에 하나의 Outlet을 운영하고, 사설망이나 가정 서버에는 가벼운 Connector를 설치하여 내부 서비스를 원하는 도메인으로 안전하게 공개하는 self-hosted 서비스 게이트웨이입니다.

사용자는 서버별 설정 파일과 Cloudflare Zero Trust 화면을 반복해서 만지는 대신, Outlet 대시보드에서 다음을 한 번에 관리합니다.

`Connector → 내부 서비스 → 공개 주소 → HTTPS`

## 2. 해결하려는 문제

현재 개인 서버나 소규모 팀이 서비스를 외부에 공개하려면 다음 작업이 흩어져 있습니다.

- 사설 서버의 네트워크와 포트를 외부에 노출해야 함
- 터널, reverse proxy, DNS, TLS 인증서를 각각 설정해야 함
- 서비스가 늘어날 때마다 인프라 설정을 다시 수정해야 함
- Cloudflare 같은 외부 Zero Trust 제품의 대시보드와 정책에 의존하게 됨

Meerkat의 핵심 가치는 “내가 소유한 도메인과 서버를 사용하면서도, 서비스 공개의 복잡성은 하나의 운영 화면으로 줄이는 것”입니다.

## 3. 대상 사용자

첫 번째 대상은 다음과 같습니다.

- 홈 서버와 개인 클라우드를 운영하는 사용자
- 여러 대의 개발·서비스 서버를 가진 개발자
- 소규모 팀 내부 서비스를 직접 운영하는 관리자
- 특정 클라우드 프록시나 Zero Trust 제품에 종속되고 싶지 않은 사용자

초기 제품은 다중 테넌트 SaaS가 아닙니다. 한 명의 운영자가 자신의 Outlet과 도메인을 관리하는 self-hosted 운영 모델을 우선합니다.

## 4. 제품 모델

| 개념 | 역할 |
| --- | --- |
| Outlet | 공개 서버에서 실행되는 관리·터널·프록시의 중심입니다. 대시보드, API, rathole server, Caddy가 함께 동작합니다. |
| Connector | 내부 서버에서 아웃바운드로 Outlet에 연결되는 실행 에이전트입니다. 앱 서버에 공개 인바운드 포트를 요구하지 않습니다. |
| Service | Connector가 접근할 내부 앱의 호스트와 포트입니다. |
| Route | Service를 외부에 노출하는 공개 호스트 이름입니다. 예: `memo.saeoo.com` |
| Domain | 운영자가 소유하고 Outlet을 가리키도록 DNS를 설정한 도메인입니다. 와일드카드 DNS로 새 Route마다 DNS 레코드를 추가하지 않는 것이 기본 방향입니다. |

제품에서 중요한 단위는 “서버”가 아니라 “서비스와 공개 주소”입니다. 사용자는 Connector의 존재를 이해해야 하지만, 일상적인 작업은 내부 서비스 하나를 공개 주소 하나에 연결하는 흐름이어야 합니다.

## 5. 최종 사용자 흐름

최종적으로 사용자는 다음 순서만 이해하면 됩니다.

1. 도메인의 `A/AAAA`와 `*` 와일드카드 DNS를 Outlet으로 지정합니다.
2. 공개 VM에 Outlet을 설치하고 TCP 80·443을 엽니다.
3. Outlet 대시보드에서 최초 관리자를 만듭니다.
4. 내부 서버를 Connector로 등록하고 일회성 설치 토큰을 발급합니다.
5. Connector가 접근할 내부 Service의 호스트와 포트를 등록합니다.
6. 원하는 Route를 입력합니다. 예: `memo.saeoo.com`.
7. Connector가 연결되면 Meerkat이 터널과 Caddy 설정을 생성하고 적용합니다.
8. Caddy가 인증서를 발급한 뒤 사용자는 공개 HTTPS 주소로 서비스에 접속합니다.

운영자는 rathole 설정, Caddyfile, 인증서 파일을 직접 편집하지 않습니다. 이 파일들은 제품의 desired state에서 자동으로 생성되는 런타임 산출물입니다.

## 6. 지켜야 할 제품 원칙

### 한 번 등록하면 운영 화면에서 관리한다

서비스 공개에 필요한 설정은 대시보드가 소유해야 합니다. 서비스마다 Cloudflare에 DNS나 Tunnel Route를 수동으로 추가하는 흐름은 Meerkat의 기본 사용 경험이 아닙니다.

### 연결은 아웃바운드가 기본이다

내부 서버는 Outlet으로 나가는 연결만 만들면 됩니다. 앱 자체에 공개 포트를 열거나, 내부 네트워크를 인터넷에 직접 노출하는 것을 요구하지 않습니다.

### HTTPS는 기본값이다

공개 HTTP 서비스는 Caddy가 관리하는 HTTPS 주소로 제공되어야 합니다. Caddy는 Cloudflare DNS-01로 Outlet 도메인의 와일드카드 인증서를 발급·갱신합니다. Outlet에는 대상 DNS zone에 한정한 최소 권한 API token을 안전하게 제공해야 합니다.

### 원하는 상태와 실제 적용 상태를 구분한다

대시보드의 등록 정보와 실제 Connector·Caddy·rathole 상태는 다를 수 있습니다. UI는 `등록 대기`, `연결됨`, `오래된 연결`, `오류`를 구분해서 보여줘야 하며, 사용자가 다음 조치를 알 수 있어야 합니다.

### 실패한 적용은 기존 서비스를 깨뜨리지 않는다

새 설정을 계산하고 검증하는 과정이 실패하면 마지막으로 정상 적용된 런타임 설정을 유지합니다. 설정 생성과 reload는 원자적으로 처리하고, 민감 정보는 로그에 남기지 않습니다.

### 비밀값은 최소 권한·일회성으로 다룬다

Connector 등록 토큰은 한 번만 표시하고 만료시킵니다. Outlet의 내부 키와 관리자 인증 정보는 서버 파일 권한으로 보호합니다. Cloudflare API token은 대시보드 DB에 저장하지 않고 Outlet 실행 환경의 보호된 배포 설정으로 전달합니다.

## 7. 현재 v1 범위

현재 목표는 “한 명의 운영자가 하나의 Outlet에서 Connector 하나의 HTTP 서비스를 HTTPS Route로 공개하는 vertical slice”를 안정화하는 것입니다.

포함되는 항목:

- Go 기반 Outlet API와 관리 대시보드
- Connector 등록, 일회성 enrollment token, heartbeat
- rathole 기반 Noise 터널
- 내부 Service 등록과 공개 Route 등록
- 연결 상태에 따른 rathole/Caddy 설정 자동 생성
- 설정 변경 감지와 안전한 reload/restart
- Caddy Cloudflare DNS-01 와일드카드 인증서 발급·갱신
- Docker Compose와 GHCR 이미지 기반 배포
- Oracle 같은 ARM64 VM에서의 운영

현재 v1의 의도적인 제약:

- 운영자는 우선 한 개의 Outlet을 관리합니다.
- Connector 등록 시 첫 Service를 함께 연결하는 흐름을 우선합니다.
- HTTP 공개 흐름을 먼저 안정화하고 TCP 공개는 후속 범위입니다.
- 팀·역할·과금·공개 SaaS 멀티테넌시는 아직 제품 범위가 아닙니다.

## 8. 로드맵

### 단계 A — 현재: vertical slice 안정화

신규 설치부터 Connector 연결, Service 등록, Route 생성, HTTPS 접속까지 한 번에 성공하는 흐름을 완성합니다. 실패 시 원인과 복구 방법이 대시보드에 보여야 합니다.

### 단계 B — 운영 경험 개선

개요와 Connector 관리 화면에서 연결 상태·최근 heartbeat·인증서 상태·마지막 적용 결과를 명확하게 표시합니다. Service와 Route의 수정·비활성화·삭제, Connector 토큰 재발급, 설치 안내와 로그 확인을 추가합니다.

### 단계 C — Connector 운영 기능

여러 Service 지원, 토큰 회전, Connector 버전 호환성, 자동 업그레이드 안내, mTLS 강화, 재시작 후 복구를 제품 수준으로 다룹니다.

### 단계 D — 확장 기능

TCP Route, 여러 Outlet, 팀과 역할, 감사 로그, 백업·복구, 더 세밀한 정책 제어를 검토합니다. 이 단계의 기능은 HTTP vertical slice의 운영 안정성을 해치지 않는 선에서 추가합니다.

## 9. 최종 목표의 완료 조건

아래 시나리오가 문서만 읽고도 재현 가능하며, 중간에 수동 설정 파일 편집 없이 동작하면 첫 제품 목표를 달성한 것으로 봅니다.

- 새 VM에 Outlet을 설치할 수 있다.
- 도메인과 와일드카드 DNS만 준비하면 Route별 DNS 레코드 추가가 필요 없다.
- 대시보드에서 Connector와 Service를 등록하고 일회성 설치 명령을 받을 수 있다.
- 내부 서버에서 Connector를 실행하면 Outlet이 heartbeat와 연결 상태를 반영한다.
- Route를 만들면 rathole과 Caddy 설정이 자동 생성·검증·적용된다.
- 인증서 발급 후 공개 HTTPS 주소가 내부 서비스 응답을 전달한다.
- 잘못된 변경은 이전 정상 설정을 유지하고, UI에서 실패 이유를 확인할 수 있다.
- Outlet과 Connector를 재시작해도 등록 정보와 런타임 상태가 복구된다.
- 관리자 비밀번호, enrollment token, 내부 키가 로그나 일반 화면에 노출되지 않는다.

## 10. 하지 않을 것

- Cloudflare Zero Trust를 Meerkat의 필수 데이터 경로로 만들지 않습니다.
- 서비스마다 운영자가 수동으로 프록시 설정 파일을 편집하게 만들지 않습니다.
- 초기 버전부터 복잡한 멀티테넌시·과금·조직 권한을 끼워 넣지 않습니다.
- 연결이 안 된 Service를 공개된 것처럼 표시하지 않습니다.
- 현재 HTTP 흐름이 안정화되기 전에 TCP와 여러 고급 프로토콜을 우선하지 않습니다.

## 11. 결정 기록

| 결정 | 이유 |
| --- | --- |
| Cloudflare DNS는 DNS-only로 사용 | 도메인은 계속 사용할 수 있지만 실제 트래픽 경로와 인증서 운영은 Meerkat이 소유해야 합니다. |
| Caddy를 Outlet에 포함 | Route별 HTTPS 발급·갱신과 reverse proxy를 제품 흐름에 포함하기 위해서입니다. |
| rathole을 터널 계층으로 사용 | 내부 Connector가 아웃바운드로 연결하는 구조와 서비스별 포워딩에 적합합니다. |
| Docker Compose를 첫 배포 단위로 사용 | Oracle VM과 로컬 검증 환경의 실행 방식을 맞추고 설치 단계를 줄이기 위해서입니다. |
| SQLite와 파일 기반 런타임 산출물로 시작 | 단일 Outlet 운영의 설치 부담을 낮추고 desired state와 runtime state를 분리하기 위해서입니다. |
| Cloudflare API token을 Outlet 실행 환경에만 제공 | 와일드카드 DNS-01 인증서 발급에 필요하며, 대시보드 DB에 외부 자격 증명을 저장하지 않습니다. |

## 12. 관련 문서

- [아키텍처](ARCHITECTURE.md): 구성요소와 런타임 구조
- [제품 설계](DESIGN.md): MVP 사용 흐름과 데이터 모델
- [API 계약](API_CONTRACT.md): Outlet·Connector API 계약
- [설정 적용과 롤백](CONFIG_ROLLOUT.md): desired state를 런타임에 적용하는 방식
- [구현 계획](IMPLEMENTATION_PLAN.md): 단계별 작업 계획
- [TODO](TODO.md): 현재 남은 작업과 결정 사항
