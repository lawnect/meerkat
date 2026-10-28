<p align="center"><b>English</b> · <a href="GUIDE.ko.md">한국어</a></p>

# Meerkat operator guide

Meerkat is a self-hosted gateway in which a Connector on your service server opens an outbound tunnel to an Outlet. You can connect multiple services over a single tunnel and expose each one at its own HTTPS address. The history of the initial implementation is kept in the [implementation plan](IMPLEMENTATION_PLAN.md) (Korean).

The product's end goal and current scope are described in the [product direction document](PRODUCT_DIRECTION.md) (Korean).

## Admin UI development

The admin UI is built with Svelte, TypeScript, and Vite, and embedded in the Outlet Go binary. Node.js 22.18+ is required.

The UI is built with [shadcn-svelte](https://www.shadcn-svelte.com/) (lyra style, no rounded corners) and Tailwind CSS 4. Icons come from Fluent UI System Icons, and the typeface is SUIT, which is bundled in the repository. Light and dark theme colors are defined in `web/src/app.css`. The theme follows the operating system setting at first, and a choice made with the theme button below the sidebar is saved in the browser. The default language is English; the Korean UI opens by adding `/ko` before the same path (for example, `/ko/connectors`). An address without `/ko` follows the language chosen with the language button, which is saved in the browser, or before any choice, whichever of English and Korean comes first in the browser's language settings. UI state, such as list search terms, filters, sorting, and pages, and the active tab on a detail page, is kept in the URL query, so it reopens as it was after a refresh or when you share the URL. The Outlet serves the UI with a CSP of `style-src 'self'` and `script-src 'self'`, so don't use inline styles or scripts. For component conventions, see [web/src/components/README.md](../web/src/components/README.md) (Korean).

```sh
npm ci
npm run dev
```

By default, Vite proxies `/api` requests to the local PoC API at `https://localhost:18443`.
To connect to a different Outlet, use `MEERKAT_DEV_API=https://outlet.example.com npm run dev`.
For UI-only work, `VITE_MOCK_API=true npm run dev` uses an in-browser mock API. It provides 32 tunnels (a mix of connected, disconnected, and not-yet-installed states), 5 domains (wildcard and individual-host), and sample usage, alerts, and audit logs. Changes are reset when you reload the page. If you turn on two-step verification for login in the mock API, the verification code is `123456`.

To preview the production bundle, generate the static files with the following command:

```sh
npm run build
```

Vite writes the assets to `web/dist/`. The Docker build embeds them in the Go binary, and Node.js isn't included in the final image.

## Run locally

Go 1.26+, Docker Engine, and Docker Compose are required. Run the commands from the repository root.

The configuration verified for the current work is preserved in `.local/poc-verified/`. To rerun it, use that path instead of `.local/poc/` in the examples below and skip `poc-init`. For E2E tests, also set `MEERKAT_POC_DIR` to its absolute path.

First, build the image and generate a new test configuration. Registration tokens expire one hour after they're generated, so generate the configuration after the build. `poc-init` doesn't overwrite an existing directory.

```sh
docker build -t meerkat-poc:local -f deploy/Dockerfile .
go run ./cmd/poc-init -dir .local/poc
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml up -d --no-build
```

## Deploy with GitHub Actions and GHCR

Images are built from release tags, one image per tag, for `linux/amd64` and `linux/arm64`:

| Tag | Image | Contents |
|---|---|---|
| `meerkat-v1.0.0` | `ghcr.io/lawnect/meerkat:v1.0.0` | Outlet, Connector, and rathole. The version shown in the admin console comes from the tag. |
| `meerkat-caddy-v0.1.1` | `ghcr.io/lawnect/meerkat-caddy:v0.1.1` | The pinned Caddy image. Release it only when Caddy changes. |

```sh
git tag meerkat-v1.0.0 && git push origin meerkat-v1.0.0
```

Each architecture builds on its own native runner. The build cache is kept in the same GHCR package under `buildcache-amd64` and `buildcache-arm64`; those tags aren't images to run.

rathole is built from the v0.5.0 source rather than taken from its release, because that release pins crates with known advisories, snow 0.9.3 among them (RUSTSEC-2024-0011). The build uses [deploy/rathole/Cargo.lock](../deploy/rathole/Cargo.lock), the v0.5.0 lockfile after `cargo update`, and only the server, client, Noise and hot-reload features, so no TLS code or OpenSSL. It speaks the same protocol as the official v0.5.0 binary in either direction.

A release without a pre-release suffix also moves `latest`; `meerkat-v1.1.0-rc.1` doesn't. Pushing to `main` builds nothing. To rebuild a release, run the workflow by hand from its tag. Remote Outlet and Connector VMs don't build the source code; they pull the GHCR image and run it with Compose.

For the first run on a new server, follow the **Initial Outlet access** steps below. To update an existing installation,
check the server's `compose.env` and `compose.outlet.yaml`, pull the image, and run Compose again.
If the GHCR package is private, run `docker login ghcr.io` on the VM with a GitHub token that has the `read:packages` scope.
For production deployments, we recommend pinning a release such as `ghcr.io/lawnect/meerkat:v1.0.0` in `MEERKAT_IMAGE` (and `MEERKAT_CADDY_IMAGE`) instead of `latest`.

### Initial Outlet access

A production Outlet doesn't use the local `poc-init`. Instead, it generates keys and a setup code with
`outlet-init` from the published Meerkat image. A domain isn't required for the first run.
Create a writable deployment directory on the server.

```sh
sudo install -d -o "$(id -u)" -g "$(id -g)" -m 700 /opt/meerkat-outlet
```

On the operator's computer, copy the repository's Compose file to the server.

```sh
scp deploy/compose.outlet.yaml user@SERVER_IP:/opt/meerkat-outlet/compose.outlet.yaml
```

Then run the following on the server.

```sh
docker pull ghcr.io/lawnect/meerkat:latest
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -dir /out -region 'Chuncheon, KR'
```

`outlet-init` generates the internal Outlet certificate, a one-time admin setup code (`admin.token`),
and the Noise server key in `/opt/meerkat-outlet/configs/`. Until you register a domain, it creates a Caddyfile
with no public hosts. Don't edit `outlet-data/rathole/server.toml` or `outlet-data/caddy/Caddyfile` directly;
the running Outlet manages them.

The Outlet region on the path diagnostics page comes from the `-region` value above. For an existing installation, add
`"region": "Chuncheon, KR"` to `/opt/meerkat-outlet/configs/outlet.json` and restart
the Outlet container.
Then create `/opt/meerkat-outlet/compose.env` as follows:

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

Replace `MEERKAT_UID` and `MEERKAT_GID` with the actual output of `id -u` and `id -g`.
Set `MEERKAT_BIND_ADDRESS` to the server interface that receives tunnel and public HTTP traffic.
To listen on all interfaces, use `0.0.0.0` as in the example.
`compose.env` is a server-only file; don't commit it to Git. Start all services together.

```sh
docker compose --env-file /opt/meerkat-outlet/compose.env \
  -f /opt/meerkat-outlet/compose.outlet.yaml up -d
```

Compose maps the Outlet's HTTPS admin port `8443` only to `127.0.0.1:18443` on the server.
Port `18443` isn't reachable on the public IP. From the operator's computer, copy only the public certificate and
open an SSH tunnel. Don't copy private keys or the `admin.token` file.

```sh
scp user@SERVER_IP:/opt/meerkat-outlet/configs/root-ca.pem ./meerkat-root-ca.pem
ssh -N -L 18443:127.0.0.1:18443 user@SERVER_IP
```

Open `https://localhost:18443` in a browser. Read the value of
`/opt/meerkat-outlet/configs/admin.token` on the server only, enter it as the setup code, create an admin account, and then
sign in. This code can be used only once, to create the admin.
Until you register your first domain, you manage the Outlet over this SSH path. If the browser doesn't
trust the self-signed certificate, add the copied `meerkat-root-ca.pem` to the browser's or operating system's trust store.
Keep using HTTPS so that session cookies work correctly. Even with Telegram login verification codes turned on, sign-ins over this SSH path don't require a code, so it serves as a recovery path when you can't receive codes ([details](#telegram-alerts-and-login-security)).

### Switch to a public domain

1. In **Domains** in the local admin console, choose a primary domain and a DNS record mode. Caddy then switches to a fixed public proxy configuration,
   and the admin hostname `<8-character random name>.PRIMARY_DOMAIN` is set (for example, `k7m2x9qa.example.com`). This name stays the same even if you change domains.
   An Outlet that registered a domain before this feature existed keeps using `meerkat.PRIMARY_DOMAIN`, so existing Connectors and bookmarks don't break.
2. In Cloudflare DNS, point an A record to the Outlet's public IPv4 address and set it to DNS only.
   - **Wildcard:** A single `*` record sends the admin hostname and all public addresses to the Outlet.
   - **Individual hosts:** Create a record for the admin hostname and one for each public address. Choose this when the root or other subdomains run on another server.

   The root (`@`) is needed only if you set the tunnel address directly to the root. If you use IPv6, also set AAAA records.
3. Open TCP ports 80 and 443 and the tunnel port (default 2333) to the outside. For HTTP-01 validation, requests on port 80 must
   reach Caddy.

Besides the admin API (`https://ADMIN_HOSTNAME`), a Connector connects over TCP to the **tunnel address** to open the data
tunnel. The default is `ADMIN_HOSTNAME:2333`, so you don't need to create a separate DNS record, and the root can stay on another server.
To use a different host, such as `tunnel.PRIMARY_DOMAIN:2333`, change it under **Domains → Tunnel address** and verify it with
**Test connection**. After you save, registered Connectors receive the new
configuration at their next status report and reconnect. The value set in the UI takes precedence over `tunnel_remote_address` in `outlet.json`.
If you leave it empty, `tunnel_remote_address` or `ADMIN_HOSTNAME:2333` is used. The Outlet also aligns
all Connector configurations with the current tunnel address at startup, so after changing `tunnel_remote_address` you only need to restart it.
Loopback addresses such as `127.0.0.1` and `localhost` can't be used. The connection test only checks DNS resolution and the TCP connection
from the Outlet server, so check the Connector server's firewall separately, on that server, with
`nc -vz TUNNEL_HOST 2333`. This test closes the connection without a handshake. The Outlet drops it
without involving rathole, unless it comes from the address of a known Connector, in which case a failed handshake may appear in the rathole log.

On the first HTTPS connection to each host, Caddy asks the Outlet whether the host is registered, then issues and renews certificates via HTTP-01. Adding or deleting a public address doesn't update the Caddy configuration. The wildcard DNS record
keeps pointing public addresses at the server, so keep it, but no wildcard certificate is issued.
After the certificate is issued, open `https://ADMIN_HOSTNAME` and sign in again. The root address of the primary domain only responds to API connections from existing Connectors that registered with the root, and it doesn't serve the admin console. Keep the SSH tunnel as a recovery path for DNS or
certificate failures. Register additional domains in **Domains**, and
choose wildcard or individual-host mode for each domain. **Check DNS** looks up only the names that fit the mode (an arbitrary subdomain for wildcard, each public address for individual hosts, the admin hostname for the primary domain, and the tunnel addresses that belong to that domain).

Public requests are terminated as HTTPS at Caddy and then forwarded over HTTP through the Outlet and the tunnel to the internal app. The internal app receives the original public `Host` along with `X-Forwarded-Host`, `X-Forwarded-Proto: https`, `X-Forwarded-Port: 443`, `X-Forwarded-For`, and `X-Real-IP`. `X-Real-IP` is the single client IP Caddy saw (no port or list), set the same way for WebSocket upgrades; a value the client sent is replaced, and the header is left out when the address is unknown. Use it as the trusted client address. Client-address headers a client could forge (`CF-Connecting-IP`, `CF-Connecting-IPv6`, `True-Client-IP`, `X-Client-IP`, `Forwarded`) are removed, so an app moved from Cloudflare must switch to `X-Real-IP`. For the app to detect HTTPS, enable trust for proxy headers in the app's proxy settings. If the app requires TLS even on the internal connection, it isn't compatible with the current HTTP connection target.

Several services, on one tunnel or on different ones, can share a public address. Adding an address another service already uses offers **Add as target**; the address then shows **Failover · N** or **Load balancing · N**, which opens its settings. **Failover** sends every request to the first healthy target in the order you set. **Load balancing** spreads requests over the healthy targets in turn, and **Keep clients on one target** sends a client IP to the same target while it's healthy, for apps that keep sessions in memory; when a target goes down, only its clients move. A target is healthy while its tunnel is connected and the app answers both the Outlet's and the Connector's checks, as its status in the console shows; a target that just failed a request is passed over for 10 seconds while another one is healthy. A request without a body that can't reach its target is tried on the next healthy one: on any method if it was never sent, and on GET, HEAD, and OPTIONS if the connection dropped or timed out before an answer; never once the target answered. Requests with a body are never sent twice. Failover isn't instant: a target counts as down about 15 seconds after its tunnel stops reporting, and requests already sent to it wait for it. Usage for a shared address is counted on its first target, because Caddy counts per address. Deleting one target's address removes only that target.

Existing installations can keep using `outlet-init -domain`. When migrating from an earlier DNS-01
installation, apply the new Outlet image and Compose file and restart the services.
You can delete any remaining `MEERKAT_CLOUDFLARE_API_TOKEN` in `compose.env`.

The first time you migrate an existing installation to this structure, the Caddy configuration changes once, so existing WebSocket connections may drop. After that, adding services and public addresses no longer makes Caddy reload its configuration, so WebSocket connections on other addresses stay up.

If an older init directory has no domain or tunnel settings, don't generate new keys; run the upgrade below
once. Don't run it again on a directory that has already been upgraded.

```sh
docker run --rm --user "$(id -u):$(id -g)" \
  --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out \
  ghcr.io/lawnect/meerkat:latest \
  -upgrade -dir /out -domain example.com
```

### Verify the local PoC

The running containers are the Outlet API, the rathole server, 2 Connectors, and 2 HTTP test services. The API uses a local test HTTPS certificate, and the data tunnel uses Noise. No host-published ports are created for the apps or the rathole service ports. The rathole service ports are reachable only from the Outlet container network. The two apps are on different Compose networks.

```sh
curl --cacert .local/poc/configs/root-ca.pem https://localhost:18443/healthz
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml exec -T outlet /usr/local/bin/poc-probe
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml exec -T outlet /usr/local/bin/poc-probe -url http://127.0.0.1:31002/ -expect meerkat:app-b
```

The tunnel responses are `meerkat:app-a` and `meerkat:app-b`, respectively. localhost:18443 is the admin HTTPS API, and localhost:2333 is the tunnel port, where the Outlet admits connections and passes them to rathole on 127.0.0.1:2334 inside its container. `poc-probe` sends real requests to the loopback service ports inside the Outlet container.

To check the admin API status, you can pass the curl configuration on standard input instead of putting the token in a command-line argument. Don't print the token value to the terminal.

```sh
IFS= read -r meerkat_admin_token < .local/poc/configs/admin.token
printf 'header = "Authorization: Bearer %s"\n' "$meerkat_admin_token" |
  curl --config - --cacert .local/poc/configs/root-ca.pem https://localhost:18443/api/v1/connectors
unset meerkat_admin_token
```

`status` is the admin API heartbeat state, and `tunnel_status` is the result of the Outlet's HTTP probe. `heartbeat.process_state` and `heartbeat.service_health` are the state of the child process and the target service, respectively. With no heartbeat for 15 seconds the status is `stale`, and after 60 seconds it's `disconnected`. When the Outlet restarts, it resets the previous connection states and waits for new heartbeats.

## Verification

```sh
go test -race ./...
go vet ./...
npx --yes @redocly/cli@2 lint api/openapi.json --extends=spec
MEERKAT_E2E=1 go test -v ./test/e2e -count=1
```

The E2E tests target only the running `meerkat-poc` project. They check two HTTP tunnels, invalid and reused registration tokens, service authorization, restart after a rathole crash, isolation of service failures, Connector restart, and recovery of stored state after an Outlet restart. These tests stop and restart that PoC's apps and Connectors, so don't run them in a real production environment.

Verified on 2026-10-09: on macOS/ARM64 Docker, the 6 E2E items, `go test -race ./...`, `go vet ./...`, and the OpenAPI syntax check passed. Linux ARM64 and AMD64 image builds and rathole execution on both platforms were also confirmed. Connections between real servers and the full E2E run on AMD64 haven't been done yet.

If you used a different configuration directory, change the Compose `--env-file` path and pass its absolute path to the E2E tests.

```sh
MEERKAT_E2E=1 MEERKAT_POC_DIR=/absolute/path/to/fixture go test -v ./test/e2e -count=1
```

Stopping and cleaning up preserves files and credentials. When you start it again, the Connector uses its stored identity.

```sh
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml down
docker compose --env-file .local/poc/compose.env -f deploy/compose.poc.yaml up -d --no-build
```

## Configuration and authentication

A Connector JSON example is in [configs/connector.example.json](../configs/connector.example.json). You can override it with the environment variables `MEERKAT_OUTLET`, `MEERKAT_NAME`, `MEERKAT_DATA_DIR`, `MEERKAT_TARGET`, `MEERKAT_RATHOLE_BINARY`, `MEERKAT_ROOT_CA`, and `MEERKAT_HEARTBEAT_INTERVAL`.

### Install a tunnel and register services

In the admin console, the thing you install and whose connection status you check is called a **tunnel**. The executable and the API call it `Connector`.

The **Tunnels** list shows each tunnel's name, connection status, services, last response, and creation date. Select a tunnel name to see its status, services, public addresses, and request paths on the detail page. The tunnel ID appears in the details rather than in the list. When you create a tunnel, the detail page immediately shows the install command, which includes that tunnel's run token. When you run the tunnel with this command, the Outlet checks the authenticated connection of a tunnel that has no services yet. When the console shows **Tunnel connected**, you can register the service name, app host, and internal port on the same detail page and add public addresses. The running Connector receives the new configuration and starts the service tunnel. You can also manage registered services from a separate **Services** menu.

While the tunnel detail page waits for a connection, it checks the connection status every 2 seconds. You can also check again immediately with the **Check connection** button. Once connected, it returns to the normal 10-second dashboard refresh interval.

A tunnel's **connection status** indicates whether the Connector has recently sent an authenticated response to the Outlet. Right after you add or edit a service, that service's status changes to **Pending check**, and once access to the internal app and the tunnel path are confirmed, it shows **Connected**. While a service is being checked, the tunnel's own connection status is tracked separately.

If the app host name can't be resolved from the tunnel container, the Connector keeps the connection, reports that service as **Error**, and rechecks every 5 seconds. Fix the service's app host and internal port or the Docker network, and the Connector receives the new configuration and retries the connection. If you run `traefik/whoami` as the `app` service in the same Compose file, the connection target is `app:80`.

On the services screen, **Service name** is the name that distinguishes the service in the dashboard, and **App host** and **Internal port** are the address the tunnel actually uses to reach the app. You can register multiple services on one tunnel, but the same combination of app host and internal port can't appear twice in the same tunnel. To publish the same app at multiple addresses, add public addresses to the existing service. You enter public subdomains separately afterward, under **Public addresses**.
On the tunnel detail page's **Overview** tab, you can check the connection status and each service's internal target, and register or edit services. Select a service's **View routes**, or select the **Routes** tab, to see and manage the request flow and each service's public addresses. Select **Edit** on a target to change the app host and internal port. Before deleting a service, delete its public addresses first. Adding, editing, or deleting a service makes the Connector fetch and reconnect with the entire changed service configuration, so other services on the same tunnel may be briefly interrupted.

The tunnel detail page's **Usage** tab overlays the request count, data received (estimated from request size), data sent (response body size), and 5xx responses over 24 hours, 7 days, or 30 days on one chart, and when there are two or more services it shows each service's share as a donut chart. A service filter lets you view a single service. The Outlet collects Caddy's per-host metrics every 15 seconds, stores them hourly in SQLite, and deletes aggregates older than 30 days. The UI also refreshes every 15 seconds. Totals for the whole Outlet are kept by hour for 10 years and by minute for 7 days, and used for the per-minute (the last 60 minutes, or any hour of the past week), hourly, daily, monthly, and yearly request charts on the Overview page. In the per-minute view, the tunnels' shares come from the hours the window touches, applied to the window's own totals. The Overview traffic card reloads every 5 seconds by default (you can turn this off with the switch on the card) and shows each tunnel's share of requests for the same period beside it. Only public HTTP paths after this feature was applied are counted, and because Caddy is restarted along with it, requests that weren't collected can't be recovered. Requests served from the Cloudflare edge cache that never reached the Outlet aren't counted. Request records by URL, client IP, or header aren't stored.

| How the app runs | App host | Internal port | Action when connecting the app |
| --- | --- | --- | --- |
| App in a Docker container | `app` (Compose service name) | The port the app listens on inside the container, for example `3000` | When you register the service, connect the tunnel to the app's network |
| App running directly on the same Linux server | `127.0.0.1` | The port the app listens on on the server, for example `3000` | Recreate the tunnel on the host network |

The tunnel install step doesn't ask for an app network. A tunnel installed with a Docker command connects to the Outlet over the default Docker bridge, and a Compose tunnel connects over its project's default network. If the tunnel is on a different network than the Docker app when you register the app, run `docker network connect APP_NETWORK meerkat-TUNNEL_NAME` on the app server. If they already share the default network of the same Compose file, no extra connection is needed. To keep the connection when Compose recreates the container, also add the app network to the tunnel service's `networks:`. If the app network is `internal: true`, also keep the tunnel's existing external-facing network.

If a Linux server app listens only on `127.0.0.1:3000`, the tunnel container must also run on the Linux host network. Keeping the same run token, recreate the container with `--network host`, or set `network_mode: host` on the Compose service and recreate it. For the service, enter `127.0.0.1` and `3000`. The app port doesn't need to be exposed to the internet.

The install guide lets you choose **Docker command** or **Docker Compose**. With Compose, you can add the tunnel service to an existing file or create a new tunnel-only file. Neither method requires an app network when you install the tunnel. Save the dedicated file as `meerkat-tunnel.yaml` and run it with `docker compose -f meerkat-tunnel.yaml up -d`.

The admin console bundles the Outlet address, the tunnel name, and a reusable run token into a single install code, and generates a command of the form `docker run ... ghcr.io/lawnect/meerkat:latest connect <install-code>`. The install code contains a secret token, so don't share it. Use the same command when you recreate the container. Services and their public addresses can be added before the tunnel first connects; the Connector receives them when it does, and the addresses go live once the app answers. The executable creates its runtime files in temporary storage and doesn't need a persistent Docker volume. The install command runs the container with a read-only root filesystem (`--read-only`), all capabilities dropped (`--cap-drop ALL`) and `no-new-privileges`; `/var/lib/meerkat`, which holds the credentials, and `/tmp` live only in memory (tmpfs), so nothing is left on the host disk. If you lose the install code, reset the connection in the admin console to issue a new token, and replace the Compose `command`. The previous token and connection become invalid. Connectors that use an older one-time registration token and stored credentials are still supported.

New run tokens don't expire automatically, and only a hash is stored on the server. They become invalid when you reset them in the admin console or delete the tunnel. The existing expiration settings still apply to the earlier PoC's one-time registration tokens and Connector API credentials.

Admin API tokens and registration tokens are stored in the Outlet state file as SHA-256 hashes. For Connector API credentials, too, the Outlet keeps only the hash. The rathole service token and the server Noise private key are separate secrets that the rathole configuration requires. Files are stored with mode 0600 and the state directory with mode 0700, and `.local/` is excluded from Git and the Docker build context. The data directory is locked so that two processes can't use the same one at the same time.

If `MEERKAT_TARGET` is set in a manual configuration, the Connector checks that every registered service target matches that host and port. To connect multiple services, omit this setting and register each service target in the admin console. Only loopback addresses and explicitly specified Compose service names are allowed as targets, and the Connector checks that the DNS result is a local or private IP and pins the actual rathole target IP. Even if a new service's name can't be resolved yet, existing reachable services keep their tunnels, and once the new target resolves, the Connector updates the Rathole configuration and adds only that service. During this, the Outlet and Connector keep the existing services' Rathole processes and public routes. If a Compose service is recreated and an existing target's IP changes, restart the Connector. A private IP alone doesn't guarantee network isolation, so even in a real deployment, connect only the Docker networks that are actually needed to the tunnel.

### Telegram alerts and login security

**Telegram alerts.** Create a bot with BotFather to get a token, then send the bot a message first from the chat that should receive alerts, or invite the bot to a group or channel. In the admin console's alert settings, enter the bot token and the chat ID (a number for a personal chat, usually a negative number for a group, and `@channel_name` for a public channel), then send a **test message** to verify. The bot token is stored only in the Outlet's SQLite database (`control.db`, mode 0600), and the API and UI show only its last four characters. It's also kept out of logs and error messages. Alerts are sent in English, and all types share the same format (an emoji and a bold title, `name: value` lines, and a link to the admin console).

| Rule | Default | When it alerts |
| --- | --- | --- |
| Tunnel disconnected | On, 5 minutes | Alerts when a tunnel that has finished registration isn't `connected` for longer than the configured time, and sends a recovery notice when it reconnects. |
| Service down | On, 5 minutes | Alerts when a service of a connected tunnel isn't serving for longer than the configured time (the Connector can't reach the app, the check through the tunnel fails, or it stays unchecked), and sends a recovery notice when it serves again. While the tunnel is down, only the tunnel alert is sent and the service's stays as it is; once the tunnel is back, the wait starts over. |
| Unstable tunnel | On, 3 times | Alerts when a tunnel reconnects (after no heartbeat for 15 seconds or two and a half of its heartbeat intervals, whichever is longer) or its tunnel process restarts at least the configured number of times within an hour, at most once an hour per tunnel. Reconnects a majority of the connected tunnels share within 30 seconds, such as when Caddy restarts, aren't counted. |
| Public address error | On | Every 5 minutes, makes a TLS handshake to each public address and the admin address through Caddy. Alerts after 2 consecutive failures (the handshake or certificate verification fails, or the certificate has less than 14 days left, or a sixth of its lifetime for shorter-lived certificates) and sends a recovery notice when the check passes. An address alerted while Caddy answered is checked every 30 minutes instead, to stay within Let's Encrypt's limit on failed validations, and the check also starts certificate issuance for addresses nobody has visited yet. When Caddy answers none of the addresses on two checks in a row, that is alerted once rather than for each address. Also alerts when a public address reports a route error. |
| DNS failure | On | Every 15 minutes, looks up the same names as **Check DNS** and alerts if the lookup fails. |
| 5xx spike | On, 5% and 50 requests | Alerts once per hour window if the current or previous UTC hour has 50 or more requests and a 5xx rate of 5% or higher. The hour under way is judged as if no more requests failed in it, so failures bunched at its top don't alert on their own. |
| Latency increase | On, +50% and 100 requests | Checks the Outlet's own processing time and the services' response time separately for the current and previous UTC hour, judging the hour under way as if the rest of it went as usual. Alerts once per hour per kind when the hour's p95 is at least 50% above the p95 of the previous 7 days, the hour has at least 100 requests (and the 7 days at least 10 times that), and the increase is at least 20 ms (Outlet) or 100 ms (services). |
| Admin login | On | On every login, reports the email, the access address (shown as such for the local admin port), and the browser and OS. |
| Rate limit | On, 500 per 10 minutes | When many requests are rejected, alerts once every 30 minutes with which limit triggered and the top IPs, subnets, and endpoints. |
| Low disk space | On, below 10% free | Alerts when free space on the Outlet's data disk falls below the threshold, and sends a recovery notice at the threshold plus 2 percentage points. |
| Low memory | On, below 10% free | Alerts when free memory, measured every minute, is below the threshold three times in a row (about 2 minutes) (measured against the container's memory limit if there is one, otherwise the host's), and sends a recovery notice at the threshold plus 2 percentage points. |
| Unexpected Outlet restart | On | When the Outlet starts after the previous run didn't stop cleanly (a crash, OOM kill, or power loss), alerts once in the first alert round. Repeated restarts alert at most once an hour. |

The same problem isn't alerted again while it stays open, even across an Outlet restart, and the same target isn't alerted again if the problem recurs within an hour. If delivery of an alert fails, the event records an error code, and ongoing problems (tunnels, services, public addresses, DNS, disk, memory) are retried for an hour. Login alerts are sent separately from the login response, so even if Telegram is slow or down, login isn't delayed. All rules are evaluated only after Telegram is configured and enabled. The Outlet can't report its own complete outage (the process or the server being down), so check the admin address's `/healthz` from an external uptime monitor.

**Login verification code.** You can turn this on after you verify Telegram. When it's on, a login over the internet (a public address) with the correct email and password sends a 6-digit code to Telegram, and you must enter it within 5 minutes for a session to be created. The code is discarded after 5 wrong attempts. Login attempts are limited to 10 per minute per client address, and internet logins as a whole to 30 wrong passwords per 15 minutes. A browser that has signed in over the internet before gets a trusted-device cookie and isn't locked out by that shared limit, so wrong passwords from strangers can't lock you out. Changing the bot token or chat ID clears the verification, which also turns this feature off.

On the **local admin port** (`https://localhost:18443`) opened through the SSH tunnel, you sign in without a code. Even if you lose your Telegram account or the bot is misconfigured, you can always recover as long as you can SSH into the server. Whether a request came over a public path is determined by a marker the Outlet attaches internally, not by request headers, so it can't be spoofed with headers such as `X-Forwarded-For`. For the access address, only the last entry of the `X-Forwarded-For` header that Caddy appends is trusted.

### Audit log

Every action an admin takes (successful and failed logins, verification code checks, logouts, and changes to tunnels, services, public addresses, domains, tunnel addresses, and Telegram, alert, and rate limit settings) is recorded in the SQLite `audit_events` table with the time, account, access path (`public`/`local`), access address, browser, target, and non-secret details. Tokens, passwords, verification codes, and bot tokens aren't recorded. The log is append-only. SQLite triggers block updates and deletes, and there is no delete API or retention period, so entries can't be erased from the UI. This doesn't stop a server administrator who can edit the database file directly, so it assumes that access to the server is controlled. Recording never holds up a request: entries are queued in memory and written in the background. If the database can't take them they wait in memory, then in files under the data directory's `spool/audit/`, and are written once the writer is idle. If even those limits are exceeded, the entry is dropped and counted in the log, and the request still goes through.

### Observe

The **Observe** page gathers what the Outlet sees, in five tabs:
- **Metrics**: what access policies decided (passed, sent to sign in, denied), by hour, day, month or year, moving through periods as the dashboard's traffic card does, in total and by policy; and how long forwarded requests took, split into the Outlet's own time (its checks and opening a tunnel connection, until the request is on its way) and the upstream's (from the request fully sent to the answer's headers, tunnel round trip and time lost on failed services before a retry included; a client's slow upload counts as neither), as averages and p95, always measured and kept hourly for 400 days. The tunnel list shows the version each Connector reports.
- **Requests**: every public request to a routed address, whatever became of it (status, the Outlet's or the service's reason for a failure, the service, duration, response size, User-Agent), for finding what attackers aim at and for statistics. Besides the log itself it tallies the requests it holds by endpoint (host and path) or by client over the last hour, day, week, 30 days or all of it, sortable by requests, 4xx, 5xx, distinct clients or endpoints, or the latest request; a row opens its requests in the log.
- **Access log**: every request access policies refused or sent to sign in, and those let in by a signed-in person or a token.
- **Failed requests**: public requests that failed (the app answered 5xx, couldn't be reached or timed out, or no service was live) or that the Outlet refused before an app (rate limits, block rules, path or upload size, connection limits).
- **Audit log**: what administrators did (above).

Each log is off until turned on, and keeps its entries in a SQLite file of its own (`request-log.db`, `access-log.db`, `failure-log.db`) so it never grows the control database. Its cap (16 MiB to 4 GiB; 256 MiB by default, 512 MiB for the request log, which fills fastest) is enforced by SQLite itself on the file; its write-ahead log is emptied past 64 MiB, and new reads wait for it past 256 MiB, and entries go after the days you choose (30 by default) or, oldest first, when the file is full. Requests never wait on a log: they hand it an entry, and entries it can't keep up with are given up and counted.

Anyone on the internet can make some entries at will: a request refused by a rate limit or a block rule, or an anonymous request sent to sign in. These *routine* entries get a budget of 60 a minute per client (an IPv4 address, or an IPv6 /56) in each log, and all other entries one of 600, since anyone can make an app answer 5xx or ask for a service that is down too; the rest are not kept and are counted as suppressed, so a flood can't fill the queue or the file. When a file is full, routine entries are deleted first, oldest first, and entries that name a person or a token, or show a service in trouble, only once no routine entry is left, so a flood can't push out real failures. The request log, which keeps every request, gives each client more: 600 a minute for 4xx (its routine entries) and 6,000 for the rest. Since anyone can make a 2xx as easily as a 4xx, it deletes strictly the oldest entries when full, so a flood of cheap requests can't push out a scanner's traces first, and it counts an IPv6 client by its /56 in its statistics, so rotating addresses doesn't hide one. The logs can be searched (host, path and client, plus email and token, service, or User-Agent) and filtered; the request log by status class, address, path or client, the access log by *named* (a person or token) or *anonymous*, and failed requests by cause or by group: *service* problems, or *refused* requests.

Every log tab, the audit log and the request tally download as CSV: every entry the current filters match (up to 10,000 groups for the tally), newest first, in UTF-8 with a byte order mark, with times in the browser's time zone (named in the column header). Cells starting with `=`, `+`, `-` or `@` are prefixed with `'`, so a path or User-Agent a client sent can't run as a spreadsheet formula. Files are written as they are read, a thousand rows at a time, and logs are read on connections of their own beside the writes, so a large download or tally neither fills the Outlet's memory nor holds up logging. Searches, statistics and downloads answer only the console's own page, at most two downloads run at once, and a statistic runs at most a minute. Sorting a statistic uses SQLite's temporary space in `/tmp`, memory in the Compose deployment: about 130 MB per million entries when sorting endpoints by their clients. A download that fails partway is cut off, so the browser reports it failed rather than keeping a short file.

### Rate limiting

Public requests are limited with token buckets before they're proxied. There are limits per client IP, per subnet (IPv4 /24, IPv6 /56), and per public hostname, plus admin-defined rules per endpoint (a host and path prefix, applied to the overall total or per IP). If any one of them is exceeded, the request is rejected with `429 Too Many Requests` and `Retry-After: 1`, and the response says which limit triggered. The defaults are generous enough not to trigger during normal use (300 requests per second per IP with a burst of 1000, 1500 and 5000 per subnet, and no per-host limit). The admin host's login path (`/api/v1/session`) has its own rule of 3 requests per second per IP with a burst of 30, which you can edit in the list. Settings take effect immediately without a restart, and rejected requests are aggregated hourly and shown in the usage chart. Certificate issuance checks (`cert-ask`) and ACME paths aren't limited, and the local admin port opened over SSH doesn't go through these limits.

This feature provides protection at the application layer (L7). It rejects requests after they've reached the Outlet, so it can't stop large-scale (volumetric) DDoS attacks that saturate the server's link and firewall. For such attacks, also use network-layer protection, such as the Cloudflare proxy (orange cloud) or your hosting provider's DDoS protection. Putting a proxy in front can change the access address, so first verify that the per-IP limits still work as intended. Keep the admin hostname, which Connectors send their API requests to, DNS-only: the tunnel port protection learns each Connector's address from those requests, and behind a proxy it would see the proxy's address instead, so the Connectors' tunnel connections would count as unknown clients (see [Tunnel port protection](#tunnel-port-protection)). Proxying the public addresses of your services doesn't affect it.

### Connection limits

Separate limits stop one request or one client from holding the Outlet's resources. Request bodies are capped at 100 MB by default, with per-hostname overrides; a declared length over the cap gets `413` before any of the body is read. One client address (IPv6 by /56) can have at most 512 requests in progress (`429` beyond that), and at most 4,096 requests are being passed to services at once (each costs the Outlet about 170 KB, so about 700 MB at most) (`503` beyond that; the admin host and Connector API don't count, so administrators and tunnels keep working). An upload or download that makes no progress for 30 seconds is cut, and so is an upload averaging under 1 KB/s; waiting for the service, pauses in a stream, and WebSocket connections don't count. Caddy refuses request headers over 16 KB. All values can be changed under Settings → Security, and 0 turns a limit off.

### Tunnel port protection

rathole's server, behind the tunnel port (2333 by default), starts a Noise handshake (an X25519 key exchange) for anyone who opens a connection, before it checks any token, and it has no limits of its own. A flood of anonymous connections would exhaust it and drop every tunnel, and the HTTP limits above don't reach it. So the Outlet listens on the tunnel port itself and decides before rathole sees a byte; rathole listens on `127.0.0.1:2334` inside the Outlet's network namespace (`tunnel_internal_address` in `outlet.json`, loopback only; unless you set it, it is the port after the tunnel port, so 2334 for 2333 and 2335 if you move the tunnel port to 2334), and the Outlet passes on only the connections it admits.

- **Known clients** pass with no limit but a sanity cap of 65,536 connections at once (a Connector opens a tunnel connection for every request it serves, so a busy site holds many). A client is known when its address made an authenticated Connector API request in the last 15 minutes (enrolling, fetching the configuration, or the heartbeat a Connector sends every few seconds), or when it is in the allowed ranges. A client is an IPv4 address or an IPv6 /64. The Outlet remembers 4,096 of them, dropping the one seen longest ago; a request that fails authentication teaches it nothing.
- **Unknown clients** get at most 120 new connections a minute and 32 at once each, and 256 at once and 50 new a second (burst of 100) all together. They must send the whole first message of the handshake (a 2-byte length, then up to 1,024 bytes; a real one is 50 bytes) within 10 seconds of connecting, or they're closed before rathole is dialled and counted as idle; a length outside 1 to 1,024 is closed at once. rathole does the handshakes of new connections one at a time and waits up to 5 seconds for one that stops halfway, so a few connections that send a byte and go quiet would otherwise stop every tunnel. Over any limit, the connection is closed at once.
- **Strict** mode refuses unknown clients outright: only known Connectors and the allowed ranges reach rathole.
- **Hang-ups.** Once rathole has closed or half-closed a connection, the client has 5 seconds to finish before the Outlet closes it too, so a client that never closes can't keep its place. A connection rathole keeps open is never timed out.

The Outlet learns a Connector's address from its authenticated Connector API requests, so a Connector is known only if its tunnel connections come from the address those requests came from. If the Outlet's domain is behind a proxy such as Cloudflare's, the Outlet sees the proxy's address instead, and if a Connector leaves through several addresses (IPv6 for one and IPv4 for the other, or a pool of NAT addresses), its tunnel connections can come from one the Outlet hasn't seen. Such a Connector is an unknown client: held to the unknown limits in limited mode ("Apply limits"), refused in strict mode ("Connector IPs only") (every public request to its services opens a tunnel connection, so a busy site can reach the limits). Add the Connector's address range to the allowed ranges, or keep the admin hostname, where the Connector API is served, DNS-only (not proxied).

Under Settings → Security you choose the mode, list up to 50 allowed IP addresses or CIDR ranges, and see the counters (active, accepted, refused by cause) and the Connector addresses the Outlet knows. A change applies to new connections at once. A port check such as `nc -vz TUNNEL_HOST 2333` isn't counted as refused.

SYN floods are left to the kernel's SYN cookies, and volumetric floods that saturate the link, to your hosting provider.

**When upgrading**, the Outlet rewrites `server.toml`, so rathole moves to `127.0.0.1:2334` with the new image, and the Compose files need no change. If the Outlet starts while the old rathole still holds port 2333, it logs a warning and retries with growing pauses (1 to 30 seconds), taking over once rathole has moved; it never stops for this. rathole picks up its new address from `server.toml` by itself. Take the rathole service from the current `deploy/compose.outlet.yaml` too: rathole shares the Outlet's network namespace, so when only the Outlet's container restarted (a crash, `docker restart`), rathole used to be left in the old namespace with every tunnel down, while looking healthy. The Outlet now rewrites `rathole/outlet-started` at every start; the service watches for that, exits once the Outlet is back so Docker starts it in the new namespace, fails its health check meanwhile, and restarts with the Outlet under Compose (`restart: true`). A health check of the rathole container (or in your orchestrator, such as Coolify) that looks for port 2333 in `/proc/net/tcp` as `:091D` would now pass on the Outlet's own listener even with rathole down. Check the port `server.toml` gives instead, which works before and after the upgrade:

```yaml
    healthcheck:
      test:
        - CMD-SHELL
        - >-
          port=$$(sed -n 's/^bind_addr = ".*:\([0-9]*\)"$$/\1/p' /state/rathole/server.toml | head -n 1);
          [ -n "$$port" ] && grep -qs ":$$(printf '%04X' "$$port") " /proc/net/tcp /proc/net/tcp6 &&
          grep -qs ':1F90 ' /proc/net/tcp /proc/net/tcp6
```

**Rolling back to 0.5.0** works on the same data: 0.5.0 ignores the new settings, the log files and the database's new tables. Remove `tunnel_internal_address` from `outlet.json` if you set it, as 0.5.0 refuses fields it doesn't know, and recreate both the Outlet and rathole containers so rathole listens on 2333 again. 0.5.0 doesn't know a block rule's *ban*: the honeypot only refuses there, and saving the block rules in 0.5.0 drops the option, so turn it on again after the next upgrade.

### Single sign-on

The admin console can sign in with an OpenID Connect provider (Google Workspace, Microsoft Entra ID, Okta, Keycloak, Authentik, and others). Under Settings → Security, enter the provider's issuer URL, client ID, and secret, and register the redirect URI shown there with the provider. Only people you allow get in: list email addresses or `@domain` entries, groups the provider puts in the ID token, or both. When both lists are set, a person must match both (Google doesn't include groups; Entra ID needs a tenant-specific issuer). The provider must mark the email as verified (`email_verified`). An allowed person without an admin account gets one that can't sign in with a password, and each administrator stays bound to the provider account they first signed in with. You can turn off password sign-in on public addresses; the local admin port over SSH always accepts passwords, so you can still get in if the provider is down. An administrator created by single sign-on has no password: on the local admin port, **Recover admin access** sets one with the server's `admin.token`. Keep the Outlet off networks shared with other stacks, such as Coolify's predefined network: containers on the same network can reach the local admin port inside the container. Tunnel ports listen on loopback only.

### Settings from environment variables

The Outlet also reads these settings from its environment, so a deployment can set them without the admin console. For Telegram, a value saved in the console overrides the environment; the console marks it **Set by environment** or **Overrides environment**, and **Revert to environment** drops the saved value. Single sign-on, the primary domain and the admin address set in the environment can't be edited in the console, so a wrong save can't lock administrators out.

| Variable | Setting |
|---|---|
| `MEERKAT_DOMAIN`, `MEERKAT_ADMIN_HOSTNAME` | Primary domain, such as `example.com`, and the admin address directly under it, such as `console.example.com` (without it, a random 8-character name). Applied when the Outlet starts; the console can't change them, but can add more domains and switch this one's DNS records mode. The primary domain starts with one record per host: point the admin address at the server, which serves the console on 443 and tunnel connections on 2333, and add a record for each public address, or switch to a wildcard. Caddy listens on 80 and 443 only once a domain exists. The log prints the console's address and the single sign-on redirect URI to register with the provider. Changing the admin address later moves the console and the Connector API: enroll existing Connectors again and register the new redirect URI. A domain change waits while public addresses exist. |
| `MEERKAT_OIDC_ISSUER`, `MEERKAT_OIDC_CLIENT_ID`, `MEERKAT_OIDC_CLIENT_SECRET` | OpenID Connect provider. Setting the issuer turns single sign-on on. |
| `MEERKAT_OIDC_LABEL`, `MEERKAT_OIDC_SCOPES`, `MEERKAT_OIDC_GROUPS_CLAIM` | Provider name for administrators (the sign-in page shows "Sign in with SSO"), scopes (default `openid email profile`), groups claim (default `groups`). |
| `MEERKAT_ADMIN_EMAILS`, `MEERKAT_ADMIN_GROUPS` | Who signs in as administrator: emails or `@domain`, and groups. When both are set, a person must match both. |
| `MEERKAT_PASSWORD_LOGIN` | `false` allows only single sign-on on public addresses (the local admin port over SSH still takes passwords). |
| `MEERKAT_TELEGRAM_BOT_TOKEN`, `MEERKAT_TELEGRAM_CHAT_ID`, `MEERKAT_TELEGRAM_ENABLED` | Telegram alerts (enabled unless `false`). |
| `MEERKAT_CADDY_UPSTREAM`, `MEERKAT_CADDY_METRICS` | Where Caddy reaches the Outlet (default `outlet:8080`) and where the Outlet reads Caddy's metrics (default `caddy:9090`). The public address check also connects to that host, on port 443. Set them when the services have other names, such as `outlet-k3x9:8080` and `caddy-k3x9:9090`, so Compose stacks don't collide. An invalid value stops the Outlet rather than fall back to a name another stack may use. Not shown in the console. |

Lists are separated by commas. With single sign-on from the environment, the public console needs no initial setup: the first listed administrator to sign in gets an account. Until an administrator exists, the local admin port over SSH still offers setup with `admin.token`. A missing or invalid value never stops the Outlet, so tunnels keep running: that setting is left out and the log names the variable.

### Access protection

Like Cloudflare Access, public addresses or paths under them can require a sign-in, and the people let in don't have to be Meerkat administrators. On the **Access SSO** tab of the **Access protection** page in the sidebar, set up the provider. It is an OpenID Connect client of its own, separate from the administrators' SSO. Register the redirect URI shown there (`https://<admin address>/api/v1/access/callback`) with the provider; that one URI serves every protected address.

Then add **access policies**. Each one names:
- the addresses it covers, including `*.domain`;
- whether it covers all paths or only paths matching conditions (the same operators as block rules);
- whether it covers all methods or only some (GET includes HEAD, and methods compare in any case), leaving the others to the policies below;
- whether it covers only requests that send an access token, any or chosen ones, leaving the others to the policies below;
- its action: **Allow** (sign in, then only who the policy allows), **Bypass** (anyone, without signing in), or **Block** (nobody);
- for Allow, who may pass: everyone access SSO signs in, or email addresses or `@domain`, groups from the ID token, or both (a person must match both), and optionally Meerkat administrators.

Policies are checked from the top, and the first enabled one that matches a request decides it; drag policies to change the order. To protect a whole app except a few paths, put Bypass policies for those paths above an Allow policy for all paths. A policy below one that covers every address, path and method of it never applies, and the list marks it. A browser without a session goes to the provider and comes back to the page it asked for; other requests get `401`. The provider must mark the email as verified. **Allow administrators** trusts that verified email: anyone the access provider vouches for with an administrator's email gets in.

Path conditions see only requests that reach the Outlet. A single-page app changes screens in the browser without one, so a condition on `/login` doesn't stop someone who opens `/` and lets the app navigate; protect all of such an app's paths and bypass the ones its clients call.

**Access tokens** let scripts, CI and other services that can't sign in through: create one under Access tokens (its value is shown once), require it in a Bypass policy, and send `X-Meerkat-Access-Token: <value>`. For example, a Bypass policy for `/v1/` that requires a token, above an Allow policy for all paths, lets clients with the token call the API without signing in while everyone else signs in. With Allow, a policy requires the token and a sign-in both. Requests without the token go to the policies below, so keep an all-paths policy under it (the list marks one that has none); a browser calling from another origin sends its preflight `OPTIONS` without the token, so bypass `OPTIONS` there too. A token counts only on the addresses it is limited to and, if set, from the IP ranges it names; its scope can be changed later without a new value. The list shows the policies that require each token and when it was last used. The app receives the token's name in `X-Meerkat-Access-Service`, never the token. Tokens expire after 90 days unless chosen otherwise, and one a policy names can't be deleted until the policy no longer names it.

Bypass conditions open paths, so they are compared exactly, as the app gets the path, case included and without adding a trailing slash: bypassing `/v1/` doesn't open `/V1/x`, `/v1./x`, `/v1;x/x`, `/v1%2fx`, or `/v1`. A request whose path an app could read more than one way, or that escapes a character it needn't (`%2A` for `*`), never matches a bypass condition. The query string isn't compared, so don't bypass a path whose query selects what the app does.

Conditions work as in block rules and see the path decoded once, so write them decoded (`/files/100%`, not `/files/100%25`). A path an app could read another way, such as an encoded `..`, matches every Allow and Block condition and no Bypass condition; a double-encoded one is also compared decoded twice. For apps that interpret paths in unusual ways, protect all paths. Someone denied can **Sign in again** from the denied page, which is how groups added to a policy later take effect.

What the policies decide, and the requests behind it, are on the **Observe** page (see below).

A session lasts the configured hours (24 by default), only on the address it was issued for. **Sign everyone out** ends all of them. A group added to a policy counts from a person's next sign-in. The groups claim and scopes are set on the Access SSO card; add the scope your provider needs to send groups.

The app receives the person's email in `X-Meerkat-Access-Email`; headers a client sends whose names start with `X-Meerkat-Access`, in any case and with `_` for `-`, are removed, and the session cookie never reaches the app. Paths under `/.meerkat/access/` and cookie names starting with `__Host-meerkat_access` belong to the Outlet; `/.meerkat/access/logout` signs out of the address after a confirmation. While Access SSO is off, Allow policies let no person in, even people already signed in; Bypass policies that require a token still let it through.

### Block rules

Like Cloudflare's custom rules, block rules answer `403` to requests for public addresses whose path matches, before they reach a tunnel. Each rule has conditions on the path (equals, starts with, ends with, contains, or matches an RE2 regular expression) and applies to all public addresses or only the ones you list, including wildcards such as `*.example.com`. Paths are decoded, cleaned, and lowercased, with trailing dots and spaces dropped from each segment, before matching, so `//.env`, `/a/../.ENV`, `/x.php;jsessionid=1`, or `/admin./x` don't slip past, and nothing under `/.well-known/` is ever blocked. Every condition also matches the path with a trailing slash, so `^/admin/` covers `/admin`. Out of the box, the honeypot (below) and a rule for hidden files at the root (`/.env`, `/.git/…`) are on. A list of common vulnerability-scanner paths (`.php`, `/wp-`, `/phpmyadmin`, …) is included but off, because some apps (Nextcloud, WordPress) use those paths; turn it on for the addresses that don't. Matching is done in memory and adds well under a microsecond per request; a path longer than 8 KiB, which nginx and Apache don't take either, is refused with `414` before any rule reads it. Rules don't apply to the admin host or the Connector API.

**Honeypot.** A rule can also *ban the client*: besides the `403`, every request from that client to any public address is refused with `403` for a while (60 minutes by default, from 1 minute to 7 days). The first default rule, `Honeypot`, does this for paths only scanners ask for: `/.env` and `/.env.production` at any depth, `/.git/…`, `/.aws/…`, `/.ssh/…`, `/.svn/…`, `/.hg/…`, `/.docker/…`, `/.kube/…`, `/.htpasswd`, `/.ds_store`, `/.git/config` and `/.git/HEAD` at any depth, and any path containing `/wp-config.php` (copies such as `.bak` or `~` too). A client is an IPv4 address or an IPv6 /64. A request a browser sends (one with `Sec-Fetch-Site` or a `Referer`) is refused but bans nobody: a browser asks for a trap path only when someone clicks a link pasted in a chat, types the address, or visits a page that embeds it, and a ban would then lock out that person, and everyone behind their address, instead of a scanner. Current browsers send `Sec-Fetch-Site` with every request; scanners send neither header. Clients on the exempt list (up to 50 addresses or CIDR ranges) are never banned; put your own addresses there if you test trap paths with curl or the like. The admin host, the Connector API, and `/.well-known/` are never affected, and a client whose address isn't known is refused but not banned. Rules that ban are tried before the others, so the honeypot wins over the broader hidden-files rule wherever it sits in the list. Bans are kept in memory only (a restart clears them), at most 65,536 of them, the oldest going first, and a request costs one atomic load while there are none, and one lookup once there are. The Banned clients card under the block rules lists them and releases one or all; the failed-request log tells a `Honeypot hit` from a `Banned client`. Updating an installation that has saved rules puts the honeypot in front of them once; delete it if you don't want it, and it stays deleted.

## Current limits and next steps

The Outlet's admin store is a JSON file written atomically, and it's an interim step before SQLite. [api/openapi.json](../api/openapi.json) defines only the APIs implemented today. Target health is a TCP connection check, and the Outlet's HTTP tunnel probe checks for a response below 500 at `/`. Per-target probe paths and Host headers, and TCP service probes, are follow-up work.

A child process crash is followed by up to 5 restarts with exponential backoff and jitter. After the process runs stably for 1 minute or more, the restart budget resets. When the network connection drops, rathole's internal reconnection uses the v0.5.0 1-second retry setting. The Connector doesn't run arbitrary shell commands, and it doesn't send child stdout/stderr as logs.

API authentication at this stage is a per-Connector bearer credential. A local Ed25519 identity is generated and its public key is registered, but signature-based API authentication and proof of possession aren't implemented yet. Noise NK authenticates the Outlet server by its public key, and a Connector's tunnel permission is verified with a per-service token.

Expiring an API credential makes the Meerkat Connector shut down the tunnel, but it doesn't give the rathole service token itself a TTL. Forcibly revoking even a standalone rathole client requires removing or rotating the service token in the server configuration and draining connections. Identity recovery when a token is consumed but the registration response is never received is also follow-up work. There is no feature for deploying the current fixture to public production.

Connections between genuinely separate servers, validation against a public firewall, and zero-downtime application of service changes remain to be done. For design references, see [ARCHITECTURE.md](ARCHITECTURE.md) (Korean), [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) (Korean), and [CONFIG_ROLLOUT.md](CONFIG_ROLLOUT.md) (Korean).
