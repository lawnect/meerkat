# Meerkat

서비스 서버의 Connector가 Outlet으로 아웃바운드 터널을 만드는 self-hosted gateway입니다. 하나의 터널에 여러 서비스를 연결하고 각각 HTTPS 주소로 공개할 수 있습니다. 초기 구현 이력은 [구현 계획](docs/IMPLEMENTATION_PLAN.md)에 남아 있습니다.

제품의 최종 목표와 현재 범위는 [제품 방향 문서](docs/PRODUCT_DIRECTION.md)에서 확인할 수 있습니다.

## 관리 UI 개발

관리 UI는 Svelte와 TypeScript, Vite로 빌드하고 Outlet Go 바이너리에 포함합니다. Node.js 22.18+가 필요합니다.

```sh
npm ci
npm run dev
```

Vite는 기본적으로 로컬 PoC API `https://localhost:18443`에 `/api` 요청을 전달합니다.
다른 Outlet에 연결하려면 `MEERKAT_DEV_API=https://outlet.example.com npm run dev`를 사용합니다.
화면 작업만 할 때는 `VITE_MOCK_API=true npm run dev`로 브라우저 내 가짜 API를 사용할 수 있습니다. 예제 Connector와 서비스를 제공하며, 변경 사항은 페이지를 새로고침하면 초기화됩니다. 상단의 `MOCK API` 표시가 켜져 있는지 확인하세요.

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

`main` 브랜치에 push하면 GitHub Actions가 `linux/amd64`와 `linux/arm64` 이미지를 빌드해
`ghcr.io/lawnect/meerkat:latest`와 커밋 SHA 태그로 게시합니다. 원격 Outlet/Connector VM에서는
소스 코드를 빌드하지 않고 GHCR 이미지를 가져와 Compose로 실행합니다.

새 서버의 첫 실행은 아래 **Outlet 초기 접속** 순서대로 진행합니다. 기존 설치를 업데이트할 때는
서버의 `compose.env`와 `compose.outlet.yaml`을 확인한 뒤 이미지를 가져와 Compose를 다시 실행합니다.
GHCR 패키지가 private이면 VM에서 `read:packages` 권한이 있는 GitHub 토큰으로 `docker login ghcr.io`를 실행합니다.
운영 배포에서는 `latest` 대신 검증한 커밋 SHA 태그를 `MEERKAT_IMAGE`로 지정하는 것을 권장합니다.

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
등록하기 전에는 Caddyfile을 만들지 않습니다. 실행 중인 Outlet이 관리하는
`outlet-data/rathole/server.toml`과 `outlet-data/caddy/Caddyfile`은 직접 수정하지 않습니다.

요청 진단 화면의 Outlet 지역은 위 `-region` 값으로 표시됩니다. 기존 설치에서는
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
`compose.env`는 서버 전용 파일이며 Git에 올리지 않습니다. 먼저 Outlet과 rathole만 시작합니다.

```sh
docker compose --env-file /opt/meerkat-outlet/compose.env \
  -f /opt/meerkat-outlet/compose.outlet.yaml up -d outlet rathole
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
등록합니다. HTTPS를 유지해야 세션 쿠키가 정상적으로 동작합니다.

### 공개 도메인으로 전환

1. 로컬 관리 화면의 **도메인 관리**에서 대표 도메인을 등록합니다. Outlet이 Caddyfile을 생성합니다.
2. Cloudflare DNS에서 대표 도메인의 루트(`@`)와 와일드카드(`*`) A 레코드를 Outlet의 공인
   IPv4 주소로 지정하고 DNS-only로 둡니다. IPv6를 사용한다면 AAAA 레코드도 설정합니다.
3. Cloudflare API token에 해당 zone의 `Zone:DNS:Edit`과 `Zone:Zone:Read` 권한을 줍니다.
   서버의 `compose.env`에 `MEERKAT_CLOUDFLARE_API_TOKEN` 값을 설정합니다. 토큰은
   대시보드 DB에 저장하지 않으며, 명령행 인자나 Git에 넣지 않습니다.
4. TCP 80·443을 외부에 열고 Caddy를 시작합니다.

```sh
docker compose --env-file /opt/meerkat-outlet/compose.env \
  -f /opt/meerkat-outlet/compose.outlet.yaml up -d
```

Caddy는 Cloudflare DNS-01로 대표 도메인과 `*.대표도메인` 인증서를 발급·갱신합니다.
인증서 발급 후 `https://대표도메인`에 접속해 다시 로그인합니다. SSH 터널은 DNS 또는
인증서 장애 시 복구 경로로 유지합니다. 추가 도메인은 **도메인 관리**에서 등록하고,
각 zone의 DNS 레코드와 Cloudflare token 권한도 같은 방식으로 설정합니다.

기존 설치는 `outlet-init -domain` 방식을 계속 사용할 수 있습니다. 새 Compose 파일로
업데이트할 때는 기존 Cloudflare token을 `compose.env`의
`MEERKAT_CLOUDFLARE_API_TOKEN`으로 옮긴 후 위 Compose 명령으로 재실행합니다.

구형 초기화 디렉터리에 도메인·터널 설정이 없다면 새 키를 만들지 말고 아래 업그레이드를
한 번 실행합니다. 이미 업그레이드한 디렉터리에는 다시 실행하지 않습니다.

```sh
docker run --rm --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -upgrade -dir /out -domain saeoo.com
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

**터널 관리** 목록에는 터널 이름, 연결 상태, 연결 서비스, 최근 응답, 생성일이 표시됩니다. 터널 이름을 누르면 상세 화면에서 현황, 연결 서비스, 공개 주소와 요청 경로를 확인할 수 있습니다. 터널 ID는 목록 대신 상세 정보에 표시됩니다. 터널을 만들면 상세 화면에 설치 명령이 바로 표시됩니다. Outlet은 이 명령 안에 일회용 등록 토큰을 포함시켜 새 터널만 등록할 수 있게 합니다. 화면에서 토큰을 따로 입력하거나 복사할 필요는 없습니다. 이 명령으로 터널을 실행하면 Outlet이 서비스가 없는 터널의 인증 연결 상태를 확인합니다. 화면에 **터널 연결됨**이 표시되면 같은 상세 화면에서 서비스 이름, 앱 호스트, 내부 포트를 등록하고 공개 주소를 추가할 수 있습니다. 실행 중인 Connector가 새 설정을 받아 서비스 터널을 시작합니다. 별도 **연결 서비스** 메뉴에서도 등록된 서비스를 관리할 수 있습니다.

연결 서비스 화면의 **서비스 이름**은 대시보드에서 구별할 이름이고, **앱 호스트**와 **내부 포트**는 터널이 실제 앱에 접속할 주소입니다. 하나의 터널에 여러 서비스를 등록할 수 있으며, 서비스마다 별도의 내부 전달 포트와 공개 주소를 사용합니다. 공개 서브도메인은 이후 **공개 주소**에서 별도로 입력합니다.
터널 상세 화면의 **개요** 탭에서는 연결 상태와 서비스별 내부 연결 대상을 확인하고 서비스를 등록·수정할 수 있습니다. 서비스의 **경로 보기**를 누르거나 **경로** 탭을 선택하면 요청 흐름과 서비스별 공개 주소를 확인·관리할 수 있습니다. 연결 대상의 **수정**을 누르면 앱 호스트와 내부 포트를 변경할 수 있습니다. 서비스 삭제 전에는 해당 서비스의 공개 주소를 먼저 삭제해야 합니다. 서비스를 추가·수정·삭제하면 Connector가 변경된 전체 서비스 설정을 다시 받아 연결하므로 같은 터널의 다른 서비스도 잠시 중단될 수 있습니다.

| 앱 실행 방식 | 앱 호스트 | 내부 포트 | 앱 연결 시 작업 |
| --- | --- | --- | --- |
| Docker 컨테이너의 앱 | `app` (Compose 서비스 이름) | 앱이 컨테이너 안에서 듣는 포트, 예: `3000` | 서비스 등록 시 터널을 앱 네트워크에 연결 |
| 같은 Linux 서버에서 직접 실행 중인 앱 | `127.0.0.1` | 앱이 서버에서 듣는 포트, 예: `3000` | 터널을 호스트 네트워크로 재생성 |

터널 설치 단계에는 앱 네트워크를 입력하지 않습니다. Docker 명령으로 설치한 터널은 Docker 기본 브리지에서 Outlet에 연결되고, Compose 터널은 해당 프로젝트의 기본 네트워크에서 연결됩니다. 앱 등록 시 터널이 Docker 앱과 다른 네트워크에 있다면 앱 서버에서 `docker network connect APP_NETWORK meerkat-TUNNEL_NAME`을 실행합니다. 같은 Compose 파일의 기본 네트워크를 이미 공유한다면 추가 연결은 필요 없습니다. Compose가 컨테이너를 재생성해도 연결이 유지되게 하려면 터널 서비스의 `networks:`에도 앱 네트워크를 추가하세요. 앱 네트워크가 `internal: true`이면 터널의 기존 외부 통신 네트워크도 유지해야 합니다.

Linux 서버의 앱이 `127.0.0.1:3000`에서만 수신한다면 터널 컨테이너도 Linux 호스트 네트워크에서 실행해야 합니다. 터널 연결 후 기존 데이터 볼륨을 유지하며 컨테이너를 `--network host`로 다시 만들거나 Compose 서비스에 `network_mode: host`를 지정하고 다시 생성하세요. 연결 서비스에는 `127.0.0.1`과 `3000`을 입력합니다. 앱 포트를 인터넷에 공개할 필요는 없습니다.

설치 안내에서는 **Docker 명령** 또는 **Docker Compose**를 선택할 수 있습니다. Compose 안에서는 기존 파일에 터널 서비스를 추가하거나 새 터널 전용 파일을 만들 수 있습니다. 어느 방법도 터널 설치 시 앱 네트워크를 요구하지 않습니다. 전용 파일은 `meerkat-tunnel.yaml`로 저장하고 `docker compose -f meerkat-tunnel.yaml up -d`로 실행합니다.

관리 화면은 Outlet 주소, 터널 이름, 일회용 등록 토큰을 초대 코드 하나로 묶어 `docker run ... ghcr.io/lawnect/meerkat:latest connect <초대코드>` 형태의 명령을 만듭니다. 초대 코드에는 비밀 토큰이 포함되므로 공유하지 마세요. 설치 안내는 같은 브라우저 탭에서 새로고침해도 토큰 만료 전까지 유지되고, 터널 연결이 확인되면 사라집니다. 터널은 등록 후 자격 증명을 Docker 볼륨에 저장합니다. Compose에서는 연결이 끝난 후 일회용 초대 코드가 들어간 `command` 줄을 제거하고 터널 컨테이너를 다시 시작할 수 있습니다. 볼륨의 저장된 identity를 사용해 재접속합니다. 기존 PoC와 수동 설정에서는 `MEERKAT_ENROLLMENT_TOKEN` 또는 `MEERKAT_ENROLLMENT_TOKEN_FILE` 방식도 계속 사용할 수 있습니다.

초기 기본 수명은 등록 토큰 1시간, Connector API 자격 증명 30일입니다. PoC에서는 `poc-init -enrollment-ttl 2h -credential-ttl 720h`로 바꿀 수 있습니다. 운영 중인 Outlet의 등록 토큰 수명은 `configs/outlet.json`의 `enrollment_ttl_seconds`를 변경하고 Outlet을 재시작하면 이후 발급하는 토큰부터 적용됩니다. 만료된 등록 토큰은 관리자 API `POST /api/v1/connectors/{id}/enrollment-tokens`에서 교체할 수 있습니다. API 자격 증명 만료 시 Connector는 터널을 종료하고, 새 토큰과 새 Connector data directory로 재등록합니다. 자동 갱신·mTLS·강제 폐기는 후속 단계입니다.

관리 API의 토큰과 등록 토큰은 Outlet 상태 파일에 SHA-256 해시로 저장합니다. Connector API 자격 증명도 Outlet에는 해시만 남깁니다. rathole service token과 서버 Noise 개인키는 rathole 설정에 필요한 별도의 비밀정보입니다. 파일은 0600, state directory는 0700으로 저장하며 `.local/`은 Git 및 Docker 빌드 컨텍스트에서 제외합니다. 같은 data directory를 두 프로세스가 동시에 사용하지 못하도록 잠급니다.

수동 설정에서 `MEERKAT_TARGET`을 지정한 경우 Connector는 등록된 모든 서비스 대상이 해당 host/port와 일치하는지 확인합니다. 여러 서비스를 연결하려면 이 설정을 생략하고 관리 화면에서 각 서비스 대상을 등록하세요. 대상은 loopback 또는 명시한 Compose 서비스 이름만 허용하고, DNS 결과가 local/private IP인지 확인해 실제 rathole 대상 IP를 고정합니다. Compose 서비스가 재생성되어 IP가 바뀌면 Connector를 재시작해야 합니다. 사설 IP라는 사실만으로 네트워크 격리가 보장되지는 않으므로 실제 배포에도 필요한 Docker 네트워크만 터널에 연결해야 합니다.

## 현재 경계와 다음 단계

Outlet의 관리 저장소는 원자적으로 저장하는 JSON 파일이며 SQLite 이전 단계입니다. [api/openapi.json](api/openapi.json)은 현재 구현된 API만 정의합니다. 대상 health는 TCP 연결 확인이고, Outlet의 HTTP tunnel probe는 `/`에서 500 미만 응답을 확인합니다. 대상별 probe 경로·Host 헤더·TCP 서비스 probe는 후속 단계입니다.

자식 프로세스 crash는 지수 백오프와 jitter로 최대 5번 재시작합니다. 1분 이상 안정적으로 실행하면 재시작 예산을 초기화합니다. 네트워크 연결이 끊겼을 때의 rathole 내부 재연결은 v0.5.0의 1초 retry 설정을 사용합니다. 임의의 shell command는 Connector에서 실행하지 않고, child stdout/stderr도 로그로 전송하지 않습니다.

이 단계의 API 인증은 Connector별 bearer credential입니다. 로컬 Ed25519 identity를 생성해 공개키를 등록하지만, 서명 기반 API 인증과 proof-of-possession은 아직 구현하지 않았습니다. Noise NK는 Outlet 서버를 공개키로 인증하며, Connector의 터널 권한은 서비스별 token으로 확인합니다.

API credential 만료는 Meerkat Connector가 터널을 종료하게 하지만 rathole service token 자체에 TTL을 부여하지는 않습니다. 독립 rathole client까지 강제로 폐기하려면 서버 설정의 service token 제거·교체와 연결 drain이 필요합니다. 등록 응답을 수신하지 못한 채 토큰이 소비된 경우의 identity 복구도 후속 작업입니다. 현재 fixture를 공개 운영에 배포하는 기능은 제공하지 않습니다.

실제 서로 다른 서버에서의 연결과 공인 방화벽 검증, 서비스 변경 시 무중단 적용은 남아 있습니다. 설계 기준은 [ARCHITECTURE.md](docs/ARCHITECTURE.md), [IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md), [CONFIG_ROLLOUT.md](docs/CONFIG_ROLLOUT.md)를 참고합니다.
