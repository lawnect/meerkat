<p align="center"><img src="web/src/assets/meerkat-inverted.svg" width="96" height="96" alt="Meerkat logo"></p>
<p align="center"><a href="README.md">English</a> · <b>한국어</b></p>

# Meerkat

서비스 서버의 Connector가 Outlet으로 아웃바운드 터널을 만드는 self-hosted gateway입니다. 하나의 터널에 여러 서비스를 연결하고 각각 HTTPS 주소로 공개할 수 있습니다. 초기 구현 이력은 [구현 계획](docs/IMPLEMENTATION_PLAN.md)에 남아 있습니다.

제품의 최종 목표와 현재 범위는 [제품 방향 문서](docs/PRODUCT_DIRECTION.md)에서 확인할 수 있습니다.

## 관리 UI 개발

관리 UI는 Svelte와 TypeScript, Vite로 빌드하고 Outlet Go 바이너리에 포함합니다. Node.js 22.18+가 필요합니다.

화면은 [shadcn-svelte](https://www.shadcn-svelte.com/)(lyra 스타일, 모서리 둥글림 없음)와 Tailwind CSS 4로 만들고, 아이콘은 Fluent UI System Icons, 글꼴은 저장소에 포함한 SUIT를 씁니다. 라이트·다크 테마 색은 `web/src/app.css`에 정의되어 있습니다. 처음에는 운영체제 설정을 따르고, 사이드바 아래의 테마 버튼으로 고른 값은 브라우저에 저장됩니다. 기본 언어는 영어이고 한국어 화면은 같은 경로 앞에 `/ko`를 붙여 엽니다(예: `/ko/connectors`). `/ko`가 없는 주소는 언어 버튼으로 고른 언어(브라우저에 저장)를 따르며, 고르기 전에는 브라우저 언어 설정에서 영어와 한국어 중 먼저 나오는 언어를 따릅니다. 목록의 검색어·필터·정렬·페이지, 상세 화면의 탭처럼 화면 상태는 주소의 쿼리에 남으므로 새로고침하거나 주소를 공유해도 그대로 열립니다. Outlet은 `style-src 'self'`, `script-src 'self'`인 CSP로 화면을 제공하므로 인라인 스타일·스크립트를 쓰지 않습니다. 컴포넌트 작성 규칙은 [web/src/components/README.md](web/src/components/README.md)를 참고합니다.

```sh
npm ci
npm run dev
```

Vite는 기본적으로 로컬 PoC API `https://localhost:18443`에 `/api` 요청을 전달합니다.
다른 Outlet에 연결하려면 `MEERKAT_DEV_API=https://outlet.example.com npm run dev`를 사용합니다.
화면 작업만 할 때는 `VITE_MOCK_API=true npm run dev`로 브라우저 내 가짜 API를 사용할 수 있습니다. 터널 32개(연결·끊김·설치 전 상태 섞음), 도메인 5개(와일드카드·개별 호스트), 사용량·알림·감사 로그 예제를 제공하며, 변경 사항은 페이지를 새로고침하면 초기화됩니다. 가짜 API에서 로그인 2단계 인증을 켜면 인증 코드는 `123456`입니다.

프로덕션 번들을 미리 확인하려면 다음 명령으로 정적 파일을 생성합니다.

```sh
npm run build
```

Vite가 `web/dist/`에 자산을 생성합니다. Docker 빌드는 이를 Go 바이너리에 포함하며, Node.js는 최종 이미지에 들어가지 않습니다.

## 로컬에서 실행

Go 1.26+, Docker Engine, Docker Compose가 필요합니다. 명령은 저장소 루트에서 실행합니다.

현재 작업에서 검증한 설정은 `.local/poc-verified/`에 보존되어 있습니다. 재실행하려면 아래 예시의 `.local/poc/` 대신 그 경로를 사용하고 `poc-init`은 생략합니다. E2E에도 `MEERKAT_POC_DIR`로 그 절대 경로를 지정합니다.

먼저 이미지를 빌드하고 새 테스트 설정을 생성합니다. 등록 토큰은 생성 후 1시간에 만료되므로 빌드 후 생성합니다. `poc-init`은 기존 디렉터리를 덮어쓰지 않습니다.

```sh
docker build -t meerkat-poc:local -f deploy/Dockerfile .
go run ./cmd/poc-init -dir .local/poc
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml up -d --no-build
```

## GitHub Actions와 GHCR 배포

이미지는 릴리스 태그로 빌드합니다. 태그 하나에 이미지 하나이며 `linux/amd64`와 `linux/arm64`로 만듭니다.

| 태그 | 이미지 | 내용 |
|---|---|---|
| `meerkat-v1.0.0` | `ghcr.io/lawnect/meerkat:v1.0.0` | Outlet, Connector, rathole. 관리 화면에 보이는 버전도 태그에서 가져옵니다. |
| `meerkat-caddy-v0.1.1` | `ghcr.io/lawnect/meerkat-caddy:v0.1.1` | 버전을 고정한 Caddy 이미지. Caddy를 바꿀 때만 릴리스합니다. |

```sh
git tag meerkat-v1.0.0 && git push origin meerkat-v1.0.0
```

rathole은 공식 릴리스 바이너리 대신 v0.5.0 소스에서 직접 빌드합니다. 해당 릴리스가 알려진 취약점이 있는 크레이트를 고정하고 있기 때문입니다(snow 0.9.3, RUSTSEC-2024-0011 등). 빌드에는 v0.5.0 락파일에 `cargo update`를 적용한 [deploy/rathole/Cargo.lock](deploy/rathole/Cargo.lock)을 쓰고, server·client·Noise·hot-reload 기능만 포함하므로 TLS 코드와 OpenSSL은 들어가지 않습니다. 프로토콜은 공식 v0.5.0 바이너리와 양방향으로 호환됩니다.

정식 버전 태그는 `latest`도 옮기고, `meerkat-v1.1.0-rc.1` 같은 사전 릴리스는 옮기지 않습니다. `main`에 push하는 것만으로는 빌드하지 않습니다. 같은 릴리스를 다시 빌드하려면 그 태그에서 워크플로를 수동으로 실행합니다. 원격 Outlet/Connector VM에서는 소스 코드를 빌드하지 않고 GHCR 이미지를 가져와 Compose로 실행합니다.

새 서버의 첫 실행은 아래 **Outlet 초기 접속** 순서대로 진행합니다. 기존 설치를 업데이트할 때는
서버의 `compose.env`와 `compose.outlet.yaml`을 확인한 뒤 이미지를 가져와 Compose를 다시 실행합니다.
GHCR 패키지가 private이면 VM에서 `read:packages` 권한이 있는 GitHub 토큰으로 `docker login ghcr.io`를 실행합니다.
운영 배포에서는 `latest` 대신 `ghcr.io/lawnect/meerkat:v1.0.0` 같은 릴리스 태그를 `MEERKAT_IMAGE`(와 `MEERKAT_CADDY_IMAGE`)에 고정하는 것을 권장합니다.

### Outlet 초기 접속

운영용 Outlet은 로컬 `poc-init`을 사용하지 않고, 게시된 Meerkat 이미지의
`outlet-init`으로 키와 설정 코드를 생성합니다. 첫 실행에는 도메인이 필요하지 않습니다.
서버에 쓰기 가능한 배포 디렉터리를 만듭니다.

```sh
sudo install -d -o "$(id -u)" -g "$(id -g)" -m 700 /opt/meerkat-outlet
```

운영자의 컴퓨터에서 저장소의 Compose 파일을 서버로 복사합니다.

```sh
scp deploy/compose.outlet.yaml user@SERVER_IP:/opt/meerkat-outlet/compose.outlet.yaml
```

이후 서버에서 다음을 실행합니다.

```sh
docker pull ghcr.io/lawnect/meerkat:latest
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -dir /out -region 'Chuncheon, KR'
```

`outlet-init`은 `/opt/meerkat-outlet/configs/`에 내부 Outlet 인증서,
일회성 관리자 설정 코드(`admin.token`)와 Noise 서버 키를 생성합니다. 도메인을
등록하기 전에는 공개 호스트가 없는 Caddyfile을 만듭니다. 실행 중인 Outlet이 관리하는
`outlet-data/rathole/server.toml`과 `outlet-data/caddy/Caddyfile`은 직접 수정하지 않습니다.

경로 진단 화면의 Outlet 지역은 위 `-region` 값으로 표시됩니다. 기존 설치에서는
`/opt/meerkat-outlet/configs/outlet.json`에 `"region": "Chuncheon, KR"`를 추가한 뒤
Outlet 컨테이너를 재시작하면 됩니다.
그 다음 `/opt/meerkat-outlet/compose.env`를 다음처럼 생성합니다.

```sh
cat >/opt/meerkat-outlet/compose.env <<'EOF'
MEERKAT_DEPLOY_DIR=/opt/meerkat-outlet
MEERKAT_UID=1000
MEERKAT_GID=1000
MEERKAT_BIND_ADDRESS=0.0.0.0
MEERKAT_ADMIN_PORT=18443
MEERKAT_IMAGE=ghcr.io/lawnect/meerkat:latest
MEERKAT_CADDY_IMAGE=ghcr.io/lawnect/meerkat-caddy:latest
EOF
chmod 600 /opt/meerkat-outlet/compose.env
```

`MEERKAT_UID`와 `MEERKAT_GID`는 실제 `id -u`, `id -g` 결과로 바꿉니다.
`MEERKAT_BIND_ADDRESS`에는 서버의 터널·공개 HTTP 수신 인터페이스를 지정합니다.
모든 인터페이스에서 수신하려면 예시처럼 `0.0.0.0`을 사용합니다.
`compose.env`는 서버 전용 파일이며 Git에 올리지 않습니다. 모든 서비스를 함께 시작합니다.

```sh
docker compose --env-file /opt/meerkat-outlet/compose.env \
  -f /opt/meerkat-outlet/compose.outlet.yaml up -d
```

Compose는 Outlet의 HTTPS 관리 포트 `8443`을 서버의 `127.0.0.1:18443`에만 연결합니다.
공인 IP의 `18443` 포트에는 접속할 수 없습니다. 운영자의 컴퓨터에서 공개 인증서만 복사하고
SSH 터널을 엽니다. 개인키나 `admin.token` 파일은 복사하지 않습니다.

```sh
scp user@SERVER_IP:/opt/meerkat-outlet/configs/root-ca.pem ./meerkat-root-ca.pem
ssh -N -L 18443:127.0.0.1:18443 user@SERVER_IP
```

브라우저에서 `https://localhost:18443`을 엽니다. 서버에서만
`/opt/meerkat-outlet/configs/admin.token` 값을 확인해 설정 코드로 입력하고 관리자 계정을 만든 뒤
로그인합니다. 이 코드는 관리자 생성에 한 번만 사용할 수 있습니다.
첫 도메인을 등록할 때까지는 이 SSH 경로로 관리합니다. 브라우저가 자체 서명 인증서를
신뢰하지 않으면 복사한 `meerkat-root-ca.pem`을 브라우저 또는 운영체제의 신뢰 저장소에
등록합니다. HTTPS를 유지해야 세션 쿠키가 정상적으로 동작합니다. Telegram 로그인 보안 코드를 켜도 이 SSH 경로의 로그인에는 코드가 필요 없어, 코드를 받을 수 없을 때의 복구 경로가 됩니다([자세히](#telegram-알림과-로그인-보안)).

### 공개 도메인으로 전환

1. 로컬 관리 화면의 **도메인 관리**에서 대표 도메인과 DNS 방식을 고릅니다. 이때 Caddy가 고정된 공개 프록시 설정으로 전환되고,
   관리 주소 `<8글자 무작위 이름>.대표도메인`이 정해집니다(예: `k7m2x9qa.example.com`). 이 이름은 도메인을 바꿔도 유지됩니다.
   이 기능 전에 도메인을 등록한 Outlet은 기존 Connector와 북마크가 끊기지 않도록 `meerkat.대표도메인`을 그대로 씁니다.
2. Cloudflare DNS에서 A 레코드를 Outlet의 공인 IPv4 주소로 지정하고 DNS-only로 둡니다.
   - **와일드카드:** `*` 레코드 하나로 관리 주소와 모든 공개 주소를 보냅니다.
   - **개별 호스트:** 관리 주소와 공개 주소마다 레코드를 만듭니다. 루트나 다른 하위 주소를 다른 서버에 쓰고 있을 때 고릅니다.
   루트(`@`)는 터널 주소를 루트로 직접 지정한 경우에만 필요합니다. IPv6를 사용한다면 AAAA 레코드도 설정합니다.
3. TCP 80·443과 터널 포트(기본 2333)를 외부에 엽니다. HTTP-01 인증을 위해 80번 포트의 요청이
   Caddy까지 도달해야 합니다.

Connector는 관리 API(`https://관리주소`)와 별개로 **터널 주소**에 TCP로 접속해 데이터
터널을 엽니다. 기본값은 `관리주소:2333`이라 DNS 레코드를 따로 만들 필요가 없고 루트는 다른 서버에 둘 수 있습니다.
`tunnel.대표도메인:2333`처럼 다른 호스트를 쓰려면 **도메인 관리 → 터널 주소**에서 바꾸고
**접속 테스트**로 확인합니다. 저장하면 등록된 Connector가 다음 상태 보고 때
새 설정을 받아 다시 연결합니다. 화면에서 지정한 값이 `outlet.json`의 `tunnel_remote_address`보다
우선하며, 비워 두면 `tunnel_remote_address` 또는 `관리주소:2333`을 사용합니다. Outlet은 시작할 때도
모든 Connector 설정을 현재 터널 주소로 맞추므로 `tunnel_remote_address`를 바꾼 뒤에는 재시작하면
됩니다. `127.0.0.1`, `localhost` 같은 루프백 주소는 지정할 수 없습니다. 접속 테스트는 Outlet
서버에서 DNS 조회와 TCP 연결만 확인하므로 Connector 서버의 방화벽은 해당 서버에서
`nc -vz 터널호스트 2333`으로 따로 확인합니다. 이 테스트는 rathole 핸드셰이크 없이 연결을 닫으므로
rathole 로그에 실패 기록이 남을 수 있습니다.

Caddy는 각 호스트에 첫 HTTPS 연결이 들어올 때 Outlet에 등록 여부를 확인하고, HTTP-01로 인증서를 발급·갱신합니다. 공개 주소를 추가하거나 삭제해도 Caddy 설정은 갱신되지 않습니다. 와일드카드 DNS 레코드는
공개 주소가 서버를 가리키게 하므로 계속 사용하지만 와일드카드 인증서는 발급하지 않습니다.
인증서 발급 후 `https://관리주소`에 접속해 다시 로그인합니다. 대표 도메인의 루트 주소는 루트로 등록된 기존 Connector의 API 연결에만 응답하며 관리 화면은 제공하지 않습니다. SSH 터널은 DNS 또는
인증서 장애 시 복구 경로로 유지합니다. 추가 도메인은 **도메인 관리**에서 등록하고
도메인마다 와일드카드 또는 개별 호스트 방식을 고릅니다. **DNS 연결 확인**은 방식에 맞는 이름(와일드카드는 임의의 하위 주소, 개별 호스트는 공개 주소마다, 대표 도메인은 관리 주소, 그 도메인에 속한 터널 주소)만 조회합니다.

공개 요청은 Caddy에서 HTTPS를 종료한 뒤 Outlet과 터널을 통해 내부 앱에 HTTP로 전달됩니다. 내부 앱에는 원래 공개 `Host`와 `X-Forwarded-Host`, `X-Forwarded-Proto: https`, `X-Forwarded-Port: 443`, `X-Forwarded-For`, `X-Real-IP`가 전달됩니다. `X-Real-IP`는 Caddy가 확인한 클라이언트 IP 하나(포트·목록 제외)이며 WebSocket 업그레이드 요청에도 동일하게 설정됩니다. 클라이언트가 보낸 값은 덮어쓰고, 접속 주소를 확인할 수 없으면 헤더를 제외합니다. 신뢰할 클라이언트 주소로는 이 헤더를 사용하십시오. 클라이언트가 위조할 수 있는 주소 헤더(`CF-Connecting-IP`, `CF-Connecting-IPv6`, `True-Client-IP`, `X-Client-IP`, `Forwarded`)는 제거되므로, Cloudflare에서 옮겨 온 앱은 `X-Real-IP`로 변경해야 합니다. 앱이 HTTPS 여부를 판단할 때는 프록시 헤더를 신뢰하도록 해당 앱의 프록시 설정을 켜야 합니다. 앱이 내부 연결 자체에도 TLS를 요구한다면 현재 HTTP 연결 대상과는 맞지 않습니다.

같은 터널 또는 다른 터널의 여러 서비스가 하나의 공개 주소를 함께 사용할 수 있습니다. 다른 서비스에서 사용 중인 주소를 추가하면 **대상으로 추가**를 제안하며, 이후 해당 주소에 **장애 조치 · N** 또는 **부하 분산 · N**이 표시되고 이를 눌러 설정을 엽니다. **장애 조치**는 지정한 순서에서 가장 앞선 정상 대상으로 모든 요청을 전달합니다. **부하 분산**은 정상 대상에 요청을 순서대로 분배하며, **클라이언트 IP 고정**을 활성화하면 세션을 메모리에 보관하는 앱을 위해 동일 클라이언트 IP를 정상인 동일 대상으로 전달합니다. 대상이 중단되면 해당 대상의 클라이언트만 다른 대상으로 이동합니다. 터널이 연결되어 있고 앱이 Outlet과 Connector의 상태 확인에 모두 응답하는 대상을 정상으로 판단하며(관리 화면의 상태와 동일), 요청에 실패한 대상은 다른 정상 대상이 있으면 10초 동안 제외합니다. 대상에 연결하지 못한 요청은 본문이 없으면 다음 정상 대상으로 재시도합니다. 전송되지 않은 요청은 모든 메서드를, 응답 전에 연결이 끊기거나 시간이 초과된 경우 GET, HEAD, OPTIONS를 재시도하며, 대상이 응답한 경우에는 재시도하지 않습니다. 본문이 있는 요청은 두 번 전송하지 않습니다. 장애 조치는 즉시 이루어지지 않습니다. 터널 보고가 중단되고 약 15초 후 대상을 중단으로 판단하며, 이미 전송된 요청은 해당 대상의 응답을 기다립니다. Caddy가 주소 단위로 집계하므로 공유 주소의 사용량은 첫 번째 대상에 기록됩니다. 한 대상의 주소를 삭제하면 해당 대상만 제외됩니다.

기존 설치는 `outlet-init -domain` 방식을 계속 사용할 수 있습니다. 이전 DNS-01
설치에서 전환할 때는 새 Outlet 이미지와 Compose 파일을 적용하고 서비스를 다시 시작합니다.
`compose.env`에 남아 있는 `MEERKAT_CLOUDFLARE_API_TOKEN`은 삭제할 수 있습니다.

기존 설치를 이 구조로 처음 전환할 때는 Caddy 설정이 한 번 바뀌므로 기존 WebSocket 연결이 끊길 수 있습니다. 이후에는 서비스와 공개 주소를 추가해도 Caddy가 다시 설정을 읽지 않아 다른 주소의 WebSocket 연결을 유지합니다.

구형 초기화 디렉터리에 도메인·터널 설정이 없다면 새 키를 만들지 말고 아래 업그레이드를
한 번 실행합니다. 이미 업그레이드한 디렉터리에는 다시 실행하지 않습니다.

```sh
docker run --rm --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -upgrade -dir /out -domain example.com
```

### 로컬 PoC 확인

실행되는 컨테이너는 Outlet API, rathole server, Connector 2개, HTTP 테스트 서비스 2개입니다. API에는 로컬 테스트용 HTTPS 인증서를 사용하고, 데이터 터널에는 Noise를 사용합니다. 앱과 rathole service 포트에는 호스트 공개 포트를 만들지 않습니다. rathole service 포트는 Outlet 컨테이너 네트워크에서만 접근할 수 있습니다. 두 앱은 서로 다른 Compose 네트워크에 있습니다.

```sh
curl --cacert .local/poc/configs/root-ca.pem https://localhost:18443/healthz
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml exec -T outlet /usr/local/bin/poc-probe
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml exec -T outlet /usr/local/bin/poc-probe -url http://127.0.0.1:31002/ -expect meerkat:app-b
```

터널 응답은 각각 `meerkat:app-a`, `meerkat:app-b`입니다. localhost:18443은 관리 HTTPS API, localhost:2333은 rathole control port입니다. `poc-probe`는 Outlet 컨테이너 안의 loopback service port로 실제 요청을 보냅니다.

관리 API의 상태를 확인할 때는 토큰을 명령행 인자에 넣지 않고 curl 설정을 표준 입력으로 전달할 수 있습니다. 토큰 값을 터미널에 출력하지 않습니다.

```sh
IFS= read -r meerkat_admin_token < .local/poc/configs/admin.token
printf 'header = "Authorization: Bearer %s"\n' "$meerkat_admin_token" |
  curl --config - --cacert .local/poc/configs/root-ca.pem https://localhost:18443/api/v1/connectors
unset meerkat_admin_token
```

`status`는 관리 API heartbeat 상태이며, `tunnel_status`는 Outlet의 HTTP probe 결과입니다. `heartbeat.process_state`와 `heartbeat.service_health`는 각각 자식 프로세스와 대상 서비스 상태입니다. heartbeat가 15초 없으면 `stale`, 60초 없으면 `disconnected`입니다. Outlet 재시작 시 이전 연결 상태는 초기화하고 새 heartbeat를 기다립니다.

## 검증

```sh
go test -race ./...
go vet ./...
npx --yes @redocly/cli@2 lint api/openapi.json --extends=spec
MEERKAT_E2E=1 go test -v ./test/e2e -count=1
```

E2E는 실행 중인 `meerkat-poc` 프로젝트만 대상으로 합니다. 두 HTTP 터널, 잘못된/재사용 등록 토큰, 서비스 권한, rathole crash 후 재시작, 서비스 장애 격리, Connector 재시작, Outlet 재시작 후 저장 상태 복구를 확인합니다. 이 테스트는 해당 PoC의 앱과 Connector를 중지·재시작하므로 실제 운영 환경에서는 실행하지 않습니다.

2026-10-09 검증: macOS/ARM64 Docker 환경에서 E2E 6개 항목, `go test -race ./...`, `go vet ./...`, OpenAPI 문법 검사 통과. Linux ARM64와 AMD64 이미지 빌드 및 두 플랫폼의 rathole 실행도 확인했습니다. 실제 서버 간 연결과 AMD64의 전체 E2E는 아직 수행하지 않았습니다.

다른 설정 디렉터리를 사용했다면 Compose의 `--env-file` 경로를 바꾸고 E2E에 그 절대 경로를 전달합니다.

```sh
MEERKAT_E2E=1 MEERKAT_POC_DIR=/absolute/path/to/fixture go test -v ./test/e2e -count=1
```

중지·정리할 때 파일과 자격 증명은 보존됩니다. 다시 실행하면 Connector가 저장된 identity를 사용합니다.

```sh
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml down
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml up -d --no-build
```

## 설정과 인증

Connector JSON 예제는 [configs/connector.example.json](configs/connector.example.json)에 있습니다. `MEERKAT_OUTLET`, `MEERKAT_NAME`, `MEERKAT_DATA_DIR`, `MEERKAT_TARGET`, `MEERKAT_RATHOLE_BINARY`, `MEERKAT_ROOT_CA`, `MEERKAT_HEARTBEAT_INTERVAL` 환경변수로 덮어쓸 수 있습니다.

### 터널 설치와 서비스 등록

관리 화면에서는 설치하고 연결 상태를 확인하는 대상을 **터널**이라고 부릅니다. 실행 프로그램과 API의 이름은 `Connector`입니다.

**터널 관리** 목록에는 터널 이름, 연결 상태, 연결 서비스, 최근 응답, 생성일이 표시됩니다. 터널 이름을 누르면 상세 화면에서 현황, 연결 서비스, 공개 주소와 요청 경로를 확인할 수 있습니다. 터널 ID는 목록 대신 상세 정보에 표시됩니다. 터널을 만들면 상세 화면에 설치 명령이 바로 표시됩니다. 명령에는 해당 터널의 실행 토큰이 포함됩니다. 이 명령으로 터널을 실행하면 Outlet이 서비스가 없는 터널의 인증 연결 상태를 확인합니다. 화면에 **터널 연결됨**이 표시되면 같은 상세 화면에서 서비스 이름, 앱 호스트, 내부 포트를 등록하고 공개 주소를 추가할 수 있습니다. 실행 중인 Connector가 새 설정을 받아 서비스 터널을 시작합니다. 별도 **연결 서비스** 메뉴에서도 등록된 서비스를 관리할 수 있습니다.

터널 상세 화면에서 연결을 기다리는 동안에는 연결 상태를 2초마다 조회합니다. **연결 확인** 버튼으로 즉시 다시 조회할 수도 있습니다. 연결되면 일반 대시보드의 10초 갱신 주기로 돌아갑니다.

터널의 **연결 상태**는 Connector가 최근 Outlet에 인증된 응답을 보냈는지를 나타냅니다. 연결 서비스를 추가하거나 수정한 직후에는 해당 서비스의 상태가 **확인 대기**로 바뀌며, 내부 앱 접근과 터널 경로가 확인되면 **연결됨**으로 표시됩니다. 서비스 확인 중에도 터널 자체의 연결 상태는 별도로 유지됩니다.

앱 호스트 이름을 터널 컨테이너에서 찾을 수 없다면 Connector는 연결을 유지하며 해당 서비스를 **오류**로 보고하고 5초마다 다시 확인합니다. 서비스의 앱 호스트와 내부 포트 또는 Docker 네트워크를 고치면 새 설정을 받아 연결을 재시도합니다. 같은 Compose 파일의 `app` 서비스에서 `traefik/whoami`를 실행한다면 연결 대상은 `app:80`입니다.

연결 서비스 화면의 **서비스 이름**은 대시보드에서 구별할 이름이고, **앱 호스트**와 **내부 포트**는 터널이 실제 앱에 접속할 주소입니다. 하나의 터널에 여러 서비스를 등록할 수 있지만, 같은 터널 안에서 앱 호스트와 내부 포트의 조합은 중복할 수 없습니다. 같은 앱을 여러 주소로 공개하려면 기존 서비스에 공개 주소를 추가하세요. 공개 서브도메인은 이후 **공개 주소**에서 별도로 입력합니다.
터널 상세 화면의 **개요** 탭에서는 연결 상태와 서비스별 내부 연결 대상을 확인하고 서비스를 등록·수정할 수 있습니다. 서비스의 **경로 보기**를 누르거나 **경로** 탭을 선택하면 요청 흐름과 서비스별 공개 주소를 확인·관리할 수 있습니다. 연결 대상의 **수정**을 누르면 앱 호스트와 내부 포트를 변경할 수 있습니다. 서비스 삭제 전에는 해당 서비스의 공개 주소를 먼저 삭제해야 합니다. 서비스를 추가·수정·삭제하면 Connector가 변경된 전체 서비스 설정을 다시 받아 연결하므로 같은 터널의 다른 서비스도 잠시 중단될 수 있습니다.

터널 상세의 **사용량** 탭에는 24시간·7일·30일 동안의 처리 건수, 받은 데이터(요청 크기 추정량), 보낸 데이터(응답 본문 크기), 5xx 응답을 한 그래프에 겹쳐 보여주고, 서비스가 둘 이상이면 서비스별 비율을 도넛 그래프로 보여줍니다. 서비스 필터로 한 서비스만 볼 수 있습니다. Outlet은 Caddy의 호스트별 메트릭을 15초마다 수집해 SQLite에 시간별로 저장하며 30일이 지난 집계는 삭제합니다. 화면도 15초마다 갱신합니다. Outlet 전체 시간별 합계는 10년 동안 보관해 개요 화면의 시간별·일별·월별·연별 요청 그래프에 사용합니다. 개요의 트래픽 카드는 기본으로 5초마다 새로 불러오며(카드의 스위치로 끌 수 있음), 옆에 같은 기간의 터널별 처리 건수 비율을 보여줍니다. 기능 적용 이후의 공개 HTTP 경로만 집계하며, Caddy도 함께 재시작되어 수집되지 않은 요청은 복구할 수 없습니다. Cloudflare 엣지 캐시에서 처리되어 Outlet에 도달하지 않은 요청은 집계되지 않습니다. URL·클라이언트 IP·헤더별 요청 기록은 저장하지 않습니다.

| 앱 실행 방식 | 앱 호스트 | 내부 포트 | 앱 연결 시 작업 |
| --- | --- | --- | --- |
| Docker 컨테이너의 앱 | `app` (Compose 서비스 이름) | 앱이 컨테이너 안에서 듣는 포트, 예: `3000` | 서비스 등록 시 터널을 앱 네트워크에 연결 |
| 같은 Linux 서버에서 직접 실행 중인 앱 | `127.0.0.1` | 앱이 서버에서 듣는 포트, 예: `3000` | 터널을 호스트 네트워크로 재생성 |

터널 설치 단계에는 앱 네트워크를 입력하지 않습니다. Docker 명령으로 설치한 터널은 Docker 기본 브리지에서 Outlet에 연결되고, Compose 터널은 해당 프로젝트의 기본 네트워크에서 연결됩니다. 앱 등록 시 터널이 Docker 앱과 다른 네트워크에 있다면 앱 서버에서 `docker network connect APP_NETWORK meerkat-TUNNEL_NAME`을 실행합니다. 같은 Compose 파일의 기본 네트워크를 이미 공유한다면 추가 연결은 필요 없습니다. Compose가 컨테이너를 재생성해도 연결이 유지되게 하려면 터널 서비스의 `networks:`에도 앱 네트워크를 추가하세요. 앱 네트워크가 `internal: true`이면 터널의 기존 외부 통신 네트워크도 유지해야 합니다.

Linux 서버의 앱이 `127.0.0.1:3000`에서만 수신한다면 터널 컨테이너도 Linux 호스트 네트워크에서 실행해야 합니다. 같은 실행 토큰을 유지하며 컨테이너를 `--network host`로 다시 만들거나 Compose 서비스에 `network_mode: host`를 지정하고 다시 생성하세요. 연결 서비스에는 `127.0.0.1`과 `3000`을 입력합니다. 앱 포트를 인터넷에 공개할 필요는 없습니다.

설치 안내에서는 **Docker 명령** 또는 **Docker Compose**를 선택할 수 있습니다. Compose 안에서는 기존 파일에 터널 서비스를 추가하거나 새 터널 전용 파일을 만들 수 있습니다. 어느 방법도 터널 설치 시 앱 네트워크를 요구하지 않습니다. 전용 파일은 `meerkat-tunnel.yaml`로 저장하고 `docker compose -f meerkat-tunnel.yaml up -d`로 실행합니다.

관리 화면은 Outlet 주소, 터널 이름, 재사용 가능한 실행 토큰을 설치 코드 하나로 묶어 `docker run ... ghcr.io/lawnect/meerkat:latest connect <설치코드>` 형태의 명령을 만듭니다. 설치 코드에는 비밀 토큰이 포함되므로 공유하지 마세요. 컨테이너를 다시 만들 때도 같은 명령을 사용합니다. 서비스와 공개 주소는 터널이 처음 연결되기 전에 미리 등록할 수 있으며, 연결 시 Connector에 함께 전달되고 앱이 응답하면 공개 주소가 활성화됩니다. 실행 파일은 임시 저장 공간에 런타임 파일을 만들며 영구 Docker 볼륨이 필요하지 않습니다. 설치 명령은 컨테이너를 읽기 전용 루트 파일시스템(`--read-only`), 모든 권한 제거(`--cap-drop ALL`), 권한 상승 금지(`no-new-privileges`)로 실행하고, 자격 증명이 들어 있는 `/var/lib/meerkat`과 `/tmp`는 메모리(tmpfs)에만 써서 호스트 디스크에 남기지 않습니다. 설치 코드가 유실되면 관리 화면에서 연결을 초기화해 새 토큰을 발급하고, Compose의 `command`를 교체하세요. 이때 이전 토큰과 연결은 무효가 됩니다. 기존 일회용 등록 토큰과 저장된 자격 증명을 사용하는 Connector는 계속 지원합니다.

새 실행 토큰은 자동 만료되지 않으며 서버에는 해시만 저장됩니다. 관리 화면에서 초기화하거나 터널을 삭제하면 무효가 됩니다. 기존 PoC의 일회용 등록 토큰과 Connector API 자격 증명에는 기존 만료 설정이 적용됩니다.

관리 API의 토큰과 등록 토큰은 Outlet 상태 파일에 SHA-256 해시로 저장합니다. Connector API 자격 증명도 Outlet에는 해시만 남깁니다. rathole service token과 서버 Noise 개인키는 rathole 설정에 필요한 별도의 비밀정보입니다. 파일은 0600, state directory는 0700으로 저장하며 `.local/`은 Git 및 Docker 빌드 컨텍스트에서 제외합니다. 같은 data directory를 두 프로세스가 동시에 사용하지 못하도록 잠급니다.

수동 설정에서 `MEERKAT_TARGET`을 지정한 경우 Connector는 등록된 모든 서비스 대상이 해당 host/port와 일치하는지 확인합니다. 여러 서비스를 연결하려면 이 설정을 생략하고 관리 화면에서 각 서비스 대상을 등록하세요. 대상은 loopback 또는 명시한 Compose 서비스 이름만 허용하고, DNS 결과가 local/private IP인지 확인해 실제 rathole 대상 IP를 고정합니다. 새 서비스의 이름을 아직 찾을 수 없어도 연결 가능한 기존 서비스는 터널을 유지하며, 새 대상이 확인되면 Rathole 설정을 갱신해 해당 서비스만 추가합니다. Outlet과 Connector는 이때 기존 서비스의 Rathole 프로세스와 공개 경로를 유지합니다. Compose 서비스가 재생성되어 기존 대상의 IP가 바뀌면 Connector를 재시작해야 합니다. 사설 IP라는 사실만으로 네트워크 격리가 보장되지는 않으므로 실제 배포에도 필요한 Docker 네트워크만 터널에 연결해야 합니다.

### Telegram 알림과 로그인 보안

**Telegram 알림.** BotFather에서 봇을 만들어 토큰을 받고, 알림을 받을 채팅에서 봇에게 먼저 말을 걸거나 그룹·채널에 봇을 초대합니다. 관리 화면의 알림 설정에 봇 토큰과 채팅 ID(개인 채팅은 숫자, 그룹은 보통 음수, 공개 채널은 `@채널이름`)를 입력하고 **테스트 메시지**를 보내 확인합니다. 봇 토큰은 Outlet의 SQLite(`control.db`, 0600)에만 저장하며 API와 화면에는 마지막 네 글자만 보입니다. 로그와 오류 메시지에도 남기지 않습니다. 알림은 영어로 전송하며 모든 종류가 같은 모양(이모지와 굵은 제목, `이름: 값` 줄, 관리 화면 링크)입니다.

| 규칙 | 기본값 | 알림 시점 |
| --- | --- | --- |
| 터널 끊김 | 켜짐, 5분 | 등록이 끝난 터널이 설정한 시간 넘게 `connected`가 아니면 알리고, 다시 연결되면 복구를 알립니다. |
| 공개 주소 오류 | 켜짐 | 공개 주소에 오류가 생기면 알리고 풀리면 복구를 알립니다. |
| DNS 확인 실패 | 켜짐 | 15분마다 **DNS 연결 확인**과 같은 이름을 조회해 실패하면 알립니다. |
| 5xx 급증 | 켜짐, 5%·50건 | 현재·직전 UTC 시간대에서 요청이 50건 이상이고 5xx 비율이 5% 이상이면 시간대마다 한 번 알립니다. |
| 관리자 로그인 | 켜짐 | 로그인할 때마다 이메일, 접속 주소(로컬 관리 포트면 그렇게 표시), 브라우저·OS를 알립니다. |
| 요청 제한 | 켜짐, 10분에 500건 | 거부된 요청이 많으면 어떤 제한이 작동했는지와 상위 IP·서브넷·엔드포인트를 30분에 한 번 알립니다. |

같은 문제는 열려 있는 동안 다시 알리지 않으며, 같은 대상이 1시간 안에 다시 생겨도 알리지 않습니다. 전송에 실패한 알림은 이벤트에 오류 코드를 남기며, 지속되는 문제(터널·공개 주소·DNS)는 한 시간 동안 다시 시도합니다. 로그인 알림은 로그인 응답과 별개로 전송하므로 Telegram이 느리거나 장애여도 로그인이 늦어지지 않습니다. 모든 규칙은 Telegram을 설정하고 켠 뒤에만 평가합니다.

**로그인 보안 코드.** Telegram을 확인한 뒤 켤 수 있습니다. 켜면 인터넷(공개 주소)에서 이메일과 비밀번호가 맞게 로그인할 때 6자리 코드를 Telegram으로 보내며, 5분 안에 입력해야 세션이 만들어집니다. 코드는 5번 틀리면 폐기됩니다. 로그인 시도는 클라이언트 주소당 분당 10회, 인터넷 로그인 전체로는 15분에 오답 30회로 제한합니다. 인터넷에서 로그인한 적이 있는 브라우저는 신뢰 기기 쿠키를 받아 이 공용 제한에 막히지 않으므로, 다른 사람이 틀린 비밀번호를 넣어도 관리자가 잠기지 않습니다. 봇 토큰이나 채팅 ID를 바꾸면 확인이 풀려 이 기능도 꺼집니다.

SSH 터널로 여는 **로컬 관리 포트**(`https://localhost:18443`)에서는 코드 없이 로그인합니다. Telegram 계정을 잃거나 봇 설정이 틀려도 서버에 SSH로 들어갈 수 있으면 언제든 복구할 수 있습니다. 공개 경로인지는 요청 헤더가 아니라 Outlet 내부에서 붙이는 표시로 판단하므로 `X-Forwarded-For` 같은 헤더로 속일 수 없습니다. 접속 주소는 Caddy가 붙인 `X-Forwarded-For`의 마지막 항목만 믿습니다.

### 감사 로그

관리자가 한 모든 작업(로그인 성공·실패, 보안 코드 확인, 로그아웃, 터널·서비스·공개 주소·도메인·터널 주소 변경, Telegram·알림·요청 제한 설정 변경)을 SQLite `audit_events`에 시간, 계정, 접속 경로(`public`/`local`), 접속 주소, 브라우저, 대상, 비밀이 아닌 세부 정보와 함께 기록합니다. 토큰, 비밀번호, 인증 코드, 봇 토큰은 기록하지 않습니다. 기록은 추가만 가능합니다. 수정·삭제를 SQLite 트리거가 막고 삭제 API나 보관 기간이 없어 화면에서는 지울 수 없습니다. 데이터베이스 파일을 직접 고칠 수 있는 서버 관리자까지 막는 것은 아니므로 서버 접근 통제가 전제입니다. 기록은 요청을 기다리게 하지 않습니다. 항목은 메모리 큐에 들어가 백그라운드에서 저장되고, 데이터베이스가 받지 못하면 메모리에, 그다음 데이터 디렉터리의 `spool/audit/` 파일에 보관했다가 한가할 때 다시 씁니다. 이 한도까지 넘으면 그 항목은 버리고 개수를 로그에 남기지만 요청은 그대로 처리합니다.

### 관찰

**관찰** 페이지는 Outlet이 보는 것을 네 탭으로 모아 보여 줍니다.
- **지표**: 접근 정책이 판정한 요청(통과, 로그인 요구, 거부)을 시간·일·월·연 단위로, 대시보드의 트래픽 카드처럼 기간을 옮겨 가며 전체와 정책별로 확인합니다.
- **접근 기록**: 접근 정책이 거부하거나 로그인을 요구한 요청과, 로그인한 사용자·토큰으로 통과한 요청
- **실패 요청**: 실패한 공개 요청(앱의 5xx, 서비스 연결 실패·시간 초과, 살아 있는 서비스 없음)과 Outlet이 앱에 보내기 전에 막은 요청(요청 제한, 차단 규칙, 경로·업로드 크기, 연결 제한)
- **감사 로그**: 관리자 작업(위)

기록은 켜기 전까지 꺼져 있으며, 제어 DB가 커지지 않도록 기록마다 별도 SQLite 파일(`access-log.db`, `failure-log.db`)에 둡니다. 용량 상한(16MiB~4GiB, 기본 256MiB)은 SQLite가 직접 막고, 지정한 보관 기간(기본 30일)이 지나거나 파일이 가득 차면 오래된 것부터 지웁니다. 요청은 기록을 기다리지 않으며, 처리량을 넘는 기록은 버리고 그 수를 셉니다.

인터넷의 누구나 마음대로 만들 수 있는 기록이 있습니다. 요청 제한이나 차단 규칙이 거부한 요청, 로그인을 요구받은 익명 요청입니다. 이런 *일상* 기록은 기록마다 클라이언트(IPv4 주소 또는 IPv6 /56) 하나당 1분에 60건까지만 남기고, 넘는 기록은 남기지 않고 억제된 수로 셉니다. 요청이 쏟아져도 대기열과 파일이 차지 않습니다. 파일이 가득 차면 일상 기록을 오래된 것부터 먼저 지우고, 사용자나 토큰이 드러나거나 서비스에 문제가 있는 기록은 일상 기록이 남지 않았을 때에만 지우므로, 요청을 쏟아부어도 실제 실패가 밀려나지 않습니다. 두 기록 모두 검색(호스트·경로·클라이언트, 그리고 이메일·토큰 또는 서비스)과 필터를 지원합니다. 접근 기록은 *사용자 있음*(사용자나 토큰)과 *익명*으로, 실패 요청은 원인이나 묶음(*서비스* 문제, *거부*된 요청)으로 거를 수 있습니다.

### 요청 제한

공개 요청은 프록시하기 전에 토큰 버킷으로 제한합니다. 클라이언트 IP별, 서브넷(IPv4 /24, IPv6 /56)별, 공개 호스트 이름별 제한과 관리자가 정하는 엔드포인트별 규칙(호스트와 경로 접두사, 전체 합계 또는 IP별)이 있고, 하나라도 넘으면 `429 Too Many Requests`와 `Retry-After: 1`로 거부하며 어떤 제한이 작동했는지 알려 줍니다. 기본값은 일반 사용에서는 걸리지 않을 만큼 넉넉합니다(IP당 초당 300건·순간 1000건, 서브넷당 1500건·5000건, 호스트별 제한 없음). 관리 호스트의 로그인 경로(`/api/v1/session`)에는 IP당 초당 3건·순간 30건 규칙이 따로 있고 목록에서 고칠 수 있습니다. 설정은 재시작 없이 바로 적용되며, 거부된 요청 수는 시간별로 집계해 사용량 그래프에서 볼 수 있습니다. 인증서 발급 확인(`cert-ask`)과 ACME 경로는 제한하지 않으며, SSH로 여는 로컬 관리 포트는 이 제한을 거치지 않습니다.

이 기능은 애플리케이션 계층(L7)의 보호입니다. 요청이 Outlet까지 도착한 뒤에 거부하므로 서버의 회선과 방화벽을 가득 채우는 대규모(volumetric) DDoS는 막지 못합니다. 그런 공격에는 Cloudflare 프록시(주황 구름)나 호스팅 업체의 DDoS 방어 같은 네트워크 계층의 보호를 함께 사용하세요. 프록시를 거치게 되면 접속 주소가 달라질 수 있으니 IP별 제한이 의도대로 동작하는지 먼저 확인하세요.

### 연결 제한

요청 하나나 클라이언트 하나가 Outlet의 자원을 붙잡지 못하도록 따로 제한합니다. 요청 본문은 기본 100MB까지 받고 호스트별로 따로 정할 수 있으며, 선언한 길이가 상한을 넘으면 본문을 받기 전에 `413`으로 거부합니다. 클라이언트 주소 하나(IPv6는 /56 단위)는 동시에 512건까지 처리하고(넘으면 `429`), 서비스로 전달 중인 요청은 전체 4,096건까지입니다(요청 하나에 약 170KB이므로 최대 약 700MB)(넘으면 `503`. 관리 호스트와 커넥터 API는 세지 않으므로 관리 화면과 터널은 계속 동작합니다). 업로드나 다운로드가 30초 동안 진행되지 않거나 업로드 평균 속도가 1KB/s보다 느리면 끊습니다. 서비스 응답을 기다리는 시간, 스트림 중간의 쉼, WebSocket 연결은 포함하지 않습니다. Caddy는 16KB를 넘는 요청 헤더를 거부합니다. 모든 값은 설정 → 보안에서 바꿀 수 있고 0이면 그 제한을 끕니다.

### SSO

관리 콘솔에 OpenID Connect 공급자(Google Workspace, Microsoft Entra ID, Okta, Keycloak, Authentik 등)로 로그인할 수 있습니다. 설정 → 보안에서 공급자의 발급자(issuer) URL, 클라이언트 ID, 시크릿을 입력하고, 화면에 나온 리디렉션 URI를 공급자에 등록하세요. 허용한 사람만 들어올 수 있습니다. 이메일 주소나 `@도메인`, 공급자가 ID 토큰에 넣어 주는 그룹, 또는 둘 다로 지정하며, 둘 다 지정하면 두 조건을 모두 충족해야 합니다(Google은 그룹을 넣지 않고, Entra ID는 테넌트 ID가 들어간 issuer가 필요합니다). 공급자가 이메일을 인증된 것으로 표시(`email_verified`)해야 합니다. 허용된 사람에게 관리자 계정이 없으면 비밀번호로는 로그인할 수 없는 계정을 만들고, 각 관리자는 처음 로그인한 공급자 계정에 연결됩니다. 공개 주소에서 비밀번호 로그인을 끌 수 있지만, SSH로 여는 로컬 관리 포트에서는 항상 비밀번호로 로그인할 수 있어 공급자에 문제가 생겨도 접속할 수 있습니다. SSO로 생성된 관리자에게는 비밀번호가 없으므로, 로컬 관리 포트의 **관리자 계정 복구**에서 서버의 `admin.token`으로 비밀번호를 설정하십시오. Outlet은 다른 스택과 공유하는 네트워크(Coolify의 사전 정의 네트워크 등)에 연결하지 마십시오. 같은 네트워크의 컨테이너는 컨테이너 내부의 로컬 관리 포트에 접근할 수 있습니다. 터널 포트는 루프백에서만 수신합니다.

### 환경 변수로 설정

아래 설정은 Outlet의 환경 변수로도 줄 수 있어서, 관리 화면 없이 배포에서 정할 수 있습니다. Telegram은 관리 화면에서 저장한 값이 환경 변수보다 우선합니다. 화면에는 **환경 변수로 구성됨** 또는 **환경 변수 재정의됨**으로 표시되고, **환경 변수 값으로 복원**을 누르면 저장한 값을 지우고 환경 변수 값을 다시 씁니다. 환경 변수로 지정한 SSO, 대표 도메인, 관리 주소는 관리 화면에서 수정할 수 없어, 잘못된 저장으로 관리자 로그인이 차단되지 않습니다.

| 변수 | 설정 |
|---|---|
| `MEERKAT_DOMAIN`, `MEERKAT_ADMIN_HOSTNAME` | 대표 도메인(예: `example.com`)과 그 바로 아래의 관리 주소(예: `console.example.com`, 지정하지 않으면 무작위 8자). Outlet 시작 시 적용되며 관리 화면에서 변경할 수 없습니다. 도메인 추가와 이 도메인의 DNS 레코드 방식 변경은 관리 화면에서 합니다. 대표 도메인은 개별 호스트 방식으로 시작합니다. 관리 주소(443: 관리 콘솔, 2333: 터널 접속)와 공개 주소마다 DNS 레코드를 서버로 지정하거나, 와일드카드 방식으로 변경하십시오. Caddy는 도메인이 있어야 80/443을 엽니다. 관리 화면 주소와 공급자에 등록할 SSO 리디렉션 URI는 로그에 출력됩니다. 관리 주소를 나중에 변경하면 관리 콘솔과 Connector API 주소가 바뀌므로, 기존 Connector를 다시 등록하고 새 리디렉션 URI를 공급자에 등록하십시오. 공개 주소가 있는 동안에는 도메인 변경이 적용되지 않습니다. |
| `MEERKAT_OIDC_ISSUER`, `MEERKAT_OIDC_CLIENT_ID`, `MEERKAT_OIDC_CLIENT_SECRET` | OpenID Connect 공급자. 발급자 URL을 넣으면 SSO가 켜집니다. |
| `MEERKAT_OIDC_LABEL`, `MEERKAT_OIDC_SCOPES`, `MEERKAT_OIDC_GROUPS_CLAIM` | 관리자용 공급자 이름(로그인 화면에는 "SSO로 로그인"으로 표시), 범위(기본 `openid email profile`), 그룹 클레임(기본 `groups`). |
| `MEERKAT_ADMIN_EMAILS`, `MEERKAT_ADMIN_GROUPS` | 관리자로 로그인할 사람: 이메일이나 `@도메인`, 그룹. 둘 다 쓰면 둘 다 맞아야 합니다. |
| `MEERKAT_PASSWORD_LOGIN` | `false`면 공개 주소에서는 SSO로만 로그인합니다(SSH로 연결한 로컬 관리 포트는 예외). |
| `MEERKAT_TELEGRAM_BOT_TOKEN`, `MEERKAT_TELEGRAM_CHAT_ID`, `MEERKAT_TELEGRAM_ENABLED` | 텔레그램 알림(`false`가 아니면 켜짐). |
| `MEERKAT_CADDY_UPSTREAM`, `MEERKAT_CADDY_METRICS` | Caddy가 Outlet에 연결하는 주소(기본 `outlet:8080`)와 Outlet이 Caddy 지표를 읽는 주소(기본 `caddy:9090`). 서비스 이름을 바꾼 경우 `outlet-k3x9:8080`, `caddy-k3x9:9090`처럼 지정해 다른 Compose 스택과 이름이 충돌하지 않게 합니다. 값이 올바르지 않으면 다른 스택이 쓸 수 있는 기본 이름으로 대체하지 않고 Outlet이 시작되지 않습니다. 관리 화면에는 표시되지 않습니다. |

목록은 쉼표로 구분합니다. 환경 변수로 SSO를 켜면 공개 주소에서는 초기 설정이 필요 없고, 목록에 있는 관리자가 처음 로그인할 때 계정이 만들어집니다. 관리자가 생기기 전까지 SSH로 연결한 로컬 관리 포트에서는 `admin.token`으로 초기 설정을 할 수 있습니다. 값이 빠졌거나 잘못돼도 Outlet은 멈추지 않아 터널은 계속 동작합니다. 그 설정만 적용하지 않고 어느 변수인지 로그에 남깁니다.

### 접근 보호

Cloudflare Access처럼 공개 주소 또는 그 아래 경로에 로그인을 요구할 수 있으며, Meerkat 관리자가 아닌 사용자도 허용할 수 있습니다. 사이드바 **접근 보호** 화면의 **접근 보호 SSO** 탭에서 공급자를 구성하십시오. 관리자 SSO와 별개의 OpenID Connect 클라이언트입니다. 화면에 표시된 리디렉션 URI(`https://<관리 주소>/api/v1/access/callback`)를 공급자에 등록하면 이 URI 하나로 모든 보호 대상 주소를 처리합니다.

이후 **접근 정책**을 추가합니다. 정책마다 다음을 지정합니다.
- 적용할 주소(`*.도메인` 포함)
- 전체 경로 또는 조건(차단 규칙과 같은 연산자)에 일치하는 경로
- 모든 메서드 또는 선택한 메서드(GET은 HEAD 포함, 대소문자 구분 없음). 다른 메서드의 요청은 아래 정책이 정합니다
- 접근 토큰 조건: 토큰(모든 토큰 또는 선택한 토큰)을 보낸 요청에만 적용. 다른 요청은 아래 정책이 정합니다
- 동작: **허용**(로그인 후 허용 대상만), **우회**(로그인 없이 누구나), **차단**(모두 거부)
- 허용 대상(허용일 때): 접근 보호 SSO 사용자 전체, 또는 이메일 주소·`@도메인`, ID 토큰의 그룹, 둘 다(둘 다 충족 필요), 필요 시 Meerkat 관리자

정책은 위에서부터 순서대로 확인하며, 요청에 처음 일치하는 활성화된 정책 하나가 적용됩니다. 순서는 정책을 드래그하여 변경합니다. 앱 전체를 보호하되 일부 경로만 열려면, 전체 경로를 허용하는 정책 위에 그 경로의 우회 정책을 둡니다. 앞선 정책이 뒤 정책의 주소, 경로, 메서드를 모두 덮으면 뒤의 정책은 적용되지 않으며, 목록에 표시됩니다. 세션이 없는 브라우저는 공급자 로그인 후 요청한 페이지로 돌아가며, 그 외 요청은 `401`을 받습니다. 공급자가 이메일을 인증된 것으로 표시해야 합니다. **관리자 허용**은 이 인증된 이메일을 기준으로 하므로, 접근 보호 SSO가 관리자 이메일로 인증한 사용자는 접근할 수 있습니다.

경로 조건은 Outlet에 도달한 요청에만 적용됩니다. 단일 페이지 앱(SPA)은 화면을 브라우저 안에서 바꾸므로, `/login` 조건은 `/`로 들어와 앱이 이동한 화면을 막지 못합니다. 이런 앱은 전체 경로를 보호하고 외부 클라이언트가 호출하는 경로만 우회로 지정하십시오.

스크립트, CI, 다른 서비스처럼 로그인할 수 없는 클라이언트는 **접근 토큰**으로 통과합니다. 접근 토큰에서 생성하고(값은 한 번만 표시), 우회 정책의 토큰 조건에 지정한 뒤 `X-Meerkat-Access-Token: <값>` 헤더를 보냅니다. 예를 들어 전체 경로 허용 정책 위에 토큰이 필요한 `/v1/` 우회 정책을 두면, 토큰을 보낸 클라이언트는 로그인 없이 API를 호출하고 나머지는 로그인합니다. 허용 정책에 토큰 조건을 두면 토큰과 로그인이 모두 필요합니다. 토큰 없는 요청은 아래 정책이 판단하므로 그 아래에 전체 경로 정책을 두십시오(없으면 목록에 표시됩니다). 다른 출처의 브라우저가 호출하면 preflight `OPTIONS`가 토큰 없이 오므로, 그 경로의 `OPTIONS`도 우회로 두십시오. 토큰은 지정한 주소에서만, IP 대역을 지정하면 그 대역에서만 유효합니다. 이용 범위는 값을 바꾸지 않고 수정할 수 있고, 목록에서 토큰마다 적용 정책과 마지막 사용 시각을 확인할 수 있습니다. 앱에는 토큰 이름이 `X-Meerkat-Access-Service`로 전달되며 토큰 자체는 전달되지 않습니다. 토큰은 기본 90일 후 만료되며, 정책에 지정된 토큰은 정책에서 제외하기 전에는 삭제할 수 없습니다.

우회 조건은 경로를 여는 만큼 앱이 받는 경로와 대소문자까지 그대로, 끝에 `/`를 붙이지 않고 비교합니다. `/v1/` 우회는 `/V1/x`, `/v1./x`, `/v1;x/x`, `/v1%2fx`, `/v1`을 열지 않으며, 앱마다 다르게 읽을 수 있는 경로나 인코딩할 필요 없는 문자를 인코딩한 경로(`*` 대신 `%2A`)는 우회 조건에 일치하지 않습니다. 쿼리 문자열은 비교하지 않으므로, 쿼리로 동작이 달라지는 경로는 우회하지 마십시오.

조건은 차단 규칙과 같은 방식으로 한 번 디코딩한 경로와 비교하므로 디코딩된 형태로 입력합니다(`/files/100%25`가 아닌 `/files/100%`). 인코딩된 `..`처럼 앱마다 다르게 해석할 수 있는 경로는 허용·차단 정책의 조건에는 모두 일치하고 우회 정책의 조건에는 일치하지 않는 것으로 보며, 이중 인코딩된 경로는 두 번 디코딩한 경로로도 비교합니다. 경로를 독자적으로 해석하는 앱은 전체 경로를 보호하십시오. 접근이 거부된 사용자는 거부 페이지의 **다시 로그인**으로 다시 로그인할 수 있으며, 나중에 정책에 추가된 그룹은 이때부터 적용됩니다.

정책이 판정한 요청의 지표와 기록은 **관찰** 페이지(아래)에서 확인합니다.

세션은 발급된 주소에서만 유효하며, 지정한 시간(기본 24시간) 동안 유지됩니다. **모든 세션 종료**로 전체 세션을 종료할 수 있습니다. 정책에 추가한 그룹은 사용자가 다음에 로그인할 때부터 적용됩니다. 그룹 클레임과 범위는 접근 보호 SSO 카드에서 지정하며, 공급자가 그룹을 보내는 데 필요한 범위를 추가하십시오.

앱은 로그인한 사용자의 이메일을 `X-Meerkat-Access-Email` 헤더로 받습니다. 클라이언트가 보낸 헤더 중 이름이 대소문자, `_`·`-` 구분 없이 `X-Meerkat-Access`로 시작하는 헤더는 제거되며, 세션 쿠키는 앱으로 전달되지 않습니다. `/.meerkat/access/` 아래 경로와 `__Host-meerkat_access`로 시작하는 쿠키 이름은 Outlet이 사용하며, `/.meerkat/access/logout`에서 확인 후 해당 주소에서 로그아웃합니다. 접근 보호 SSO가 비활성화된 동안 허용 정책은 이미 로그인한 사용자를 포함하여 아무도 들이지 않으며, 토큰이 필요한 우회 정책은 계속 동작합니다.

### 차단 규칙

Cloudflare의 사용자 지정 규칙처럼, 경로가 규칙에 맞는 공개 주소 요청을 터널로 보내기 전에 `403`으로 거부합니다. 규칙마다 경로 조건(같음, 다음으로 시작, 다음으로 끝남, 포함, 정규식과 일치)을 정하고 모든 공개 주소 또는 지정한 주소에 적용합니다(`*.example.com` 같은 와일드카드 포함). 경로는 디코딩·정리·소문자 변환 후 각 조각 끝의 점과 공백을 지우고 비교하므로 `//.env`, `/a/../.ENV`, `/x.php;jsessionid=1`, `/admin./x` 같은 변형도 막히며, `/.well-known/` 아래는 차단하지 않습니다. 모든 조건은 끝에 `/`를 붙인 경로와도 비교하므로 `^/admin/`은 `/admin`에도 적용됩니다. 기본으로는 루트의 숨김 파일(`/.env`, `/.git/…`)만 막습니다. 흔한 취약점 스캐너 경로 목록(`.php`, `/wp-`, `/phpmyadmin` 등)도 들어 있지만 Nextcloud나 WordPress처럼 그 경로를 쓰는 앱이 있어 꺼져 있으니, 필요 없는 주소에만 활성화하십시오. 비교는 메모리에서 하며 요청당 1µs도 걸리지 않습니다. nginx·Apache처럼 8KiB를 넘는 경로는 규칙을 확인하기 전에 `414`로 거부합니다. 관리 호스트와 커넥터 API에는 적용하지 않습니다.

## 현재 경계와 다음 단계

Outlet의 관리 저장소는 원자적으로 저장하는 JSON 파일이며 SQLite 이전 단계입니다. [api/openapi.json](api/openapi.json)은 현재 구현된 API만 정의합니다. 대상 health는 TCP 연결 확인이고, Outlet의 HTTP tunnel probe는 `/`에서 500 미만 응답을 확인합니다. 대상별 probe 경로·Host 헤더·TCP 서비스 probe는 후속 단계입니다.

자식 프로세스 crash는 지수 백오프와 jitter로 최대 5번 재시작합니다. 1분 이상 안정적으로 실행하면 재시작 예산을 초기화합니다. 네트워크 연결이 끊겼을 때의 rathole 내부 재연결은 v0.5.0의 1초 retry 설정을 사용합니다. 임의의 shell command는 Connector에서 실행하지 않고, child stdout/stderr도 로그로 전송하지 않습니다.

이 단계의 API 인증은 Connector별 bearer credential입니다. 로컬 Ed25519 identity를 생성해 공개키를 등록하지만, 서명 기반 API 인증과 proof-of-possession은 아직 구현하지 않았습니다. Noise NK는 Outlet 서버를 공개키로 인증하며, Connector의 터널 권한은 서비스별 token으로 확인합니다.

API credential 만료는 Meerkat Connector가 터널을 종료하게 하지만 rathole service token 자체에 TTL을 부여하지는 않습니다. 독립 rathole client까지 강제로 폐기하려면 서버 설정의 service token 제거·교체와 연결 drain이 필요합니다. 등록 응답을 수신하지 못한 채 토큰이 소비된 경우의 identity 복구도 후속 작업입니다. 현재 fixture를 공개 운영에 배포하는 기능은 제공하지 않습니다.

실제 서로 다른 서버에서의 연결과 공인 방화벽 검증, 서비스 변경 시 무중단 적용은 남아 있습니다. 설계 기준은 [ARCHITECTURE.md](docs/ARCHITECTURE.md), [IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md), [CONFIG_ROLLOUT.md](docs/CONFIG_ROLLOUT.md)를 참고합니다.
