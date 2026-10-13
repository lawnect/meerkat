# Meerkat

서비스 서버가 Outlet으로 아웃바운드 터널을 만드는 self-hosted gateway입니다. 현재 구현 범위는 [첫 vertical slice](docs/IMPLEMENTATION_PLAN.md): Go Connector 등록, rathole v0.5.0 Noise 터널, HTTP 전달, heartbeat, 자식 프로세스 재시작입니다.

제품의 최종 목표와 현재 범위는 [제품 방향 문서](docs/PRODUCT_DIRECTION.md)에서 확인할 수 있습니다.

## 로컬에서 실행

Go 1.26+, Docker Engine, Docker Compose가 필요합니다. 명령은 저장소 루트에서 실행합니다.

현재 작업에서 검증한 설정은 `.local/poc-verified/`에 보존되어 있습니다. 재실행하려면 아래 예시의 `.local/poc/` 대신 그 경로를 사용하고 `poc-init`은 생략합니다. E2E에도 `MEERKAT_POC_DIR`로 그 절대 경로를 지정합니다.

먼저 이미지를 빌드하고 새 테스트 설정을 생성합니다. 등록 토큰은 생성 후 10분에 만료되므로 빌드 후 생성합니다. `poc-init`은 기존 디렉터리를 덮어쓰지 않습니다.

```sh
docker build -t meerkat-poc:local -f deploy/Dockerfile .
go run ./cmd/poc-init -dir .local/poc
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml up -d --no-build
```

## GitHub Actions와 GHCR 배포

`main` 브랜치에 push하면 GitHub Actions가 `linux/amd64`와 `linux/arm64` 이미지를 빌드해
`ghcr.io/lawnect/meerkat:latest`와 커밋 SHA 태그로 게시합니다. 원격 Outlet/Connector VM에서는
소스 코드를 빌드하지 않고 GHCR 이미지를 가져와 Compose로 실행합니다.

```sh
docker login ghcr.io
MEERKAT_IMAGE=ghcr.io/lawnect/meerkat:latest \
  docker compose --env-file compose.env -f deploy/compose.outlet.yaml pull
MEERKAT_IMAGE=ghcr.io/lawnect/meerkat:latest \
  docker compose --env-file compose.env -f deploy/compose.outlet.yaml up -d
```

GHCR 패키지가 private이면 VM에서 `read:packages` 권한이 있는 GitHub 토큰으로 로그인해야 합니다.
운영 배포에서는 `latest` 대신 검증한 커밋 SHA 태그를 `MEERKAT_IMAGE`로 지정하는 것을 권장합니다.

### Oracle Outlet 초기화

운영용 Outlet은 로컬 `poc-init`을 사용하지 않고, 게시된 Meerkat 이미지의
`outlet-init`으로 새 키와 설정을 생성합니다. Oracle VM에서 다음을 실행합니다.

```sh
docker pull ghcr.io/lawnect/meerkat:latest
docker pull ghcr.io/lawnect/meerkat-caddy:latest
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -dir /out -domain saeoo.com
```

`outlet-init`은 `/opt/meerkat-outlet/configs/`에 내부 Outlet 인증서,
관리자 토큰과 Noise 서버 키를 생성합니다. 실행 중인 Outlet은 이 키로
`outlet-data/rathole/server.toml`과 `outlet-data/caddy/Caddyfile`을 자동 생성합니다.
두 파일은 직접 수정하지 않습니다.
그 다음 `/opt/meerkat-outlet/compose.env`를 다음처럼 생성합니다.

```sh
cat >/opt/meerkat-outlet/compose.env <<'EOF'
MEERKAT_DEPLOY_DIR=/opt/meerkat-outlet
MEERKAT_UID=1000
MEERKAT_GID=1000
MEERKAT_BIND_ADDRESS=0.0.0.0
MEERKAT_IMAGE=ghcr.io/lawnect/meerkat:latest
MEERKAT_CADDY_IMAGE=ghcr.io/lawnect/meerkat-caddy:latest
EOF
chmod 600 /opt/meerkat-outlet/compose.env
```

`MEERKAT_UID`와 `MEERKAT_GID`는 실제 `id -u`, `id -g` 결과로 바꿉니다.
Caddy는 Cloudflare DNS-01 검증으로 `*.saeoo.com` 와일드카드 인증서를 발급·갱신합니다.
Cloudflare API token은 대상 zone의 `Zone:DNS:Edit`과 `Zone:Zone:Read` 권한으로 제한합니다.
서버에 내려받은 `compose.outlet.yaml`의 Caddy `environment` 항목에 직접 입력하세요.
이 서버 전용 파일은 Git에 올리지 마세요.
`saeoo.com`과 `*.saeoo.com`의 DNS가 Outlet IP를 가리켜야 하며, 공개 HTTPS와 HTTP redirect를
위해 TCP 80·443을 외부에서 열어야 합니다. 마지막으로 아래처럼 실행합니다.

```sh
docker compose --env-file /opt/meerkat-outlet/compose.env \
  -f deploy/compose.outlet.yaml up -d
```

기존 초기화 디렉터리에는 새 키를 만들지 말고, 새 이미지로 아래 업그레이드를 한 번 실행합니다.
기존 설치는 Compose 명령을 실행하기 전에 서버의 Compose 파일에 Cloudflare API token을
입력해야 합니다.

```sh
docker run --rm --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -upgrade -dir /out -domain saeoo.com
```

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

등록 토큰은 `MEERKAT_ENROLLMENT_TOKEN` 또는 `MEERKAT_ENROLLMENT_TOKEN_FILE` 중 하나로 전달합니다. 성공 후에는 저장된 API 자격 증명을 사용하고 등록 토큰을 재사용하지 않습니다. Connector가 시작할 때 토큰 환경변수를 제거하며 자식 rathole 프로세스에도 전달하지 않습니다. Compose의 원래 환경·token 파일은 운영자가 별도로 제거할 수 있습니다.

초기 기본 수명은 등록 토큰 10분, Connector API 자격 증명 30일입니다. `poc-init -enrollment-ttl 10m -credential-ttl 720h`로 바꿀 수 있습니다. 만료된 등록 토큰은 관리자 API `POST /api/v1/connectors/{id}/enrollment-tokens`에서 교체할 수 있습니다. API 자격 증명 만료 시 Connector는 터널을 종료하고, 새 토큰과 새 Connector data directory로 재등록합니다. 자동 갱신·mTLS·강제 폐기는 후속 단계입니다.

관리 API의 토큰과 등록 토큰은 Outlet 상태 파일에 SHA-256 해시로 저장합니다. Connector API 자격 증명도 Outlet에는 해시만 남깁니다. rathole service token과 서버 Noise 개인키는 rathole 설정에 필요한 별도의 비밀정보입니다. 파일은 0600, state directory는 0700으로 저장하며 `.local/`은 Git 및 Docker 빌드 컨텍스트에서 제외합니다. 같은 data directory를 두 프로세스가 동시에 사용하지 못하도록 잠급니다.

Connector는 로컬 설정의 host/port와 Outlet에서 허용한 host/port가 일치하는지 확인합니다. 대상은 loopback 또는 명시한 단일 Compose 서비스 이름만 허용하고, DNS 결과가 local/private IP인지 확인해 실제 rathole 대상 IP를 고정합니다. Compose 서비스가 재생성되어 IP가 바뀌면 Connector를 재시작해야 합니다. 사설 IP라는 사실만으로 네트워크 격리가 보장되지는 않으므로 실제 배포에도 서비스별 Compose 네트워크 분리를 유지해야 합니다.

## 현재 경계와 다음 단계

Outlet의 관리 저장소는 원자적으로 저장하는 JSON 파일이며 SQLite 이전 단계입니다. [api/openapi.json](api/openapi.json)은 현재 구현된 API만 정의합니다. 대상 health는 TCP 연결 확인이고, Outlet의 HTTP tunnel probe는 `/`에서 500 미만 응답을 확인합니다. 대상별 probe 경로·Host 헤더·TCP 서비스 probe는 후속 단계입니다.

자식 프로세스 crash는 지수 백오프와 jitter로 최대 5번 재시작합니다. 1분 이상 안정적으로 실행하면 재시작 예산을 초기화합니다. 네트워크 연결이 끊겼을 때의 rathole 내부 재연결은 v0.5.0의 1초 retry 설정을 사용합니다. 임의의 shell command는 Connector에서 실행하지 않고, child stdout/stderr도 로그로 전송하지 않습니다.

이 단계의 API 인증은 Connector별 bearer credential입니다. 로컬 Ed25519 identity를 생성해 공개키를 등록하지만, 서명 기반 API 인증과 proof-of-possession은 아직 구현하지 않았습니다. Noise NK는 Outlet 서버를 공개키로 인증하며, Connector의 터널 권한은 서비스별 token으로 확인합니다.

API credential 만료는 Meerkat Connector가 터널을 종료하게 하지만 rathole service token 자체에 TTL을 부여하지는 않습니다. 독립 rathole client까지 강제로 폐기하려면 서버 설정의 service token 제거·교체와 연결 drain이 필요합니다. 등록 응답을 수신하지 못한 채 토큰이 소비된 경우의 identity 복구도 후속 작업입니다. 현재 fixture를 공개 운영에 배포하는 기능은 제공하지 않습니다.

다음 구현 순서는 SQLite, Connector/Service CRUD, rathole server 설정 적용, Caddy, HAProxy/ACL, SvelteKit UI입니다. 실제 서로 다른 서버에서의 연결과 공인 방화벽 검증은 아직 남아 있습니다. 설계 기준은 [ARCHITECTURE.md](docs/ARCHITECTURE.md), [IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md), [CONFIG_ROLLOUT.md](docs/CONFIG_ROLLOUT.md)를 참고합니다.
