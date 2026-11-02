<div align="center">

<img src="web/src/assets/meerkat-inverted.svg" width="96" height="96" alt="Meerkat logo">

# Meerkat

**Self-hosted [rathole](https://github.com/rapiz1/rathole) tunnels with an admin UI.**

[![License: MIT](https://img.shields.io/badge/license-MIT-0f172a)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/go-1.26-0f172a)](go.mod)
[![Image](https://img.shields.io/badge/image-ghcr.io%2Flawnect%2Fmeerkat-0f172a)](https://github.com/lawnect/meerkat/pkgs/container/meerkat)
[![Platforms](https://img.shields.io/badge/platforms-linux%2Famd64%20%7C%20arm64-0f172a)](deploy/Dockerfile)

[Operator guide](docs/GUIDE.md) · [API contract](docs/API_CONTRACT.md) · [Architecture](docs/ARCHITECTURE.md) · **English** · [한국어](README.ko.md)

</div>

Meerkat publishes private HTTP services over HTTPS through a public server you operate. It combines rathole tunnels with an admin UI for routing, access policies and request logs: a self-hosted alternative to Cloudflare Tunnel.

Run an **Outlet** on the public server and a **Connector** beside your apps. Connectors establish outbound encrypted tunnels, so private servers need no inbound ports.

<p align="center"><a href="docs/images/overview.png"><img src="docs/images/overview.png" alt="Admin console overview: requests over time, tunnels and their status" width="100%"></a></p>

> **Status:** pre-1.0 (0.x). One Outlet per deployment, with no clustering or high availability. Services are HTTP, including WebSocket. Images for Linux `amd64` and `arm64`. Breaking changes and rollback steps are listed in each release's notes.

## How it works

```mermaid
flowchart LR
    client["Client"]

    subgraph public["Public server"]
        caddy["Caddy: HTTPS"]
        outlet["Outlet: policies and routing"]
        server["rathole server"]
        caddy --> outlet --> server
    end

    subgraph private["Private server"]
        connector["Connector: rathole client"]
        app["Private services"]
        connector --> app
    end

    client -->|"HTTPS :443"| caddy
    server -->|"Requests through the tunnel"| connector
    connector -.->|"Establishes the tunnel: TCP :2333, Noise NK"| server
```

Solid arrows show the request path; the dashed arrow shows the Connector opening the tunnel. Caddy issues a certificate per hostname (HTTP-01) for addresses the Outlet has registered. The Outlet applies limits, block rules and access policies, picks a healthy target, and forwards the request; the app receives the original `Host` and `X-Forwarded-*` headers, and an `X-Real-IP` the client can't set. Administrators use the Outlet's HTTPS hostname, or SSH port forwarding for initial setup and recovery.

## Features

- **Publishing:** multiple services per Connector, automatic HTTPS, and failover or load balancing across healthy targets.
- **Access:** OpenID Connect sign-in in front of addresses or paths, ordered policies, tokens for automation, and an IP allowlist for the admin console.
- **Traffic controls:** rate, connection and upload limits, block rules with a scanner-banning honeypot, admission control on the tunnel port, and HSTS on public addresses.
- **Observability:** request, access, failure and audit logs, per-endpoint statistics, latency split into Outlet and upstream time, and CSV export.
- **Operations:** Connector management with versions, Telegram alerts, administrator SSO, and SSH recovery access.

<table>
  <tr>
    <td width="33%"><a href="docs/images/connectors.png"><img src="docs/images/connectors.png" alt="Tunnel list with connection status, services and Connector versions"></a><p align="center">Tunnels, status and versions</p></td>
    <td width="33%"><a href="docs/images/observe.png"><img src="docs/images/observe.png" alt="Access decisions by hour and latency averages and p95"></a><p align="center">Access decisions and latency</p></td>
    <td width="33%"><a href="docs/images/access.png"><img src="docs/images/access.png" alt="Ordered access policies with bypass and allow actions"></a><p align="center">Access policies</p></td>
  </tr>
</table>

## Enterprise features

For a company running its own tunnels: identity from your provider, every change on record, and data that stays on your server.

- **Single sign-on for administrators:** OpenID Connect with Google Workspace, Microsoft Entra ID, Okta, Keycloak, Authentik and others. Administrators are admitted by email, `@domain` or provider group, the provider must have verified the email, and each administrator stays bound to the provider account they first signed in with. Password sign-in can be turned off on public addresses. Configured from environment variables, a new Outlet needs no initial setup: an allowed person gets an administrator account on first sign-in.
- **Sign-in in front of internal apps:** like Cloudflare Access, public addresses or paths can require a sign-in through a separate OpenID Connect client. Ordered allow, bypass and block policies pick requests by address and by conditions on path, method, client IP and access token, each with a "not" form (all must hold), and admit listed people, `@domain`s, groups or administrators. The app receives the signed-in email in `X-Meerkat-Access-Email`, never the session cookie. Sessions last 1 to 720 hours and can all be ended at once.
- **IP address book:** register office, VPN and home addresses once, with a note and tags such as `office`; policies and the console limit name a tag (autocompleted) to mean every address carrying it, so a move is one edit.
- **Tokens for services and CI:** clients that can't sign in send `X-Meerkat-Access-Token`. Each token works only on its listed addresses and, if set, IP ranges, can expire, and reaches the app as its name in `X-Meerkat-Access-Service`, never its value.
- **Locked-down console:** limit the admin console to office or VPN IP ranges or address book tags; other addresses get `404`. The local admin port over SSH is never limited and serves as the recovery path, and password sign-ins from the internet can require a Telegram code.
- **Audit trail:** administrative changes and sign-in attempts are recorded with the administrator, the client address and whether the request came from the internet or the local port. The log is append-only (database triggers refuse edits and deletes), searchable, and exportable as CSV.
- **Request records:** request, access-decision and failure logs, each with its own size limit (16 to 4,096 MiB) and retention (1 to 365 days); statistics per endpoint and per client; latency split into Outlet and upstream time; CSV export of any filtered view.
- **Alerting:** Telegram alerts for tunnels and services going down, public address, certificate and DNS failures, 5xx spikes, latency increases, request floods, unstable tunnels, low disk or memory, unexpected restarts, and administrator sign-ins.
- **Resilience:** one address can be served by several tunnels on different servers, with failover or load balancing, optionally sticky per client; a target that fails is skipped until it answers again.
- **Data residency:** self-hosted, with logs, policies, credentials and certificates on your server and outbound connections limited to those listed under [Security model](#security-model). The Outlet and Connector run as non-root users on read-only file systems with every capability dropped, images are built for `linux/amd64` and `linux/arm64`, and each release states its upgrade and rollback steps.

## Quick start

**Requirements.** A public Linux server with Docker Engine and the Compose plugin, a domain whose DNS you control, and inbound TCP `80`, `443` and `2333`. Each private server needs Docker and outbound access to the Outlet; no inbound ports.

**1. Outlet.** On your machine, from a checkout of the release you deploy:

```sh
ssh -t user@SERVER 'sudo install -d -o "$(id -u)" -g "$(id -g)" -m 700 /opt/meerkat-outlet'
scp deploy/compose.outlet.yaml user@SERVER:/opt/meerkat-outlet/
```

On the server, generate keys, certificates and the setup code, then start the stack:

```sh
docker run --rm --user "$(id -u):$(id -g)" --entrypoint /usr/local/bin/outlet-init \
  -v /opt/meerkat-outlet:/out ghcr.io/lawnect/meerkat:latest -dir /out

cat > /opt/meerkat-outlet/compose.env <<EOF
MEERKAT_DEPLOY_DIR=/opt/meerkat-outlet
MEERKAT_UID=$(id -u)
MEERKAT_GID=$(id -g)
MEERKAT_BIND_ADDRESS=0.0.0.0
MEERKAT_IMAGE=ghcr.io/lawnect/meerkat:latest
EOF
docker compose --env-file /opt/meerkat-outlet/compose.env -f /opt/meerkat-outlet/compose.outlet.yaml up -d
```

**2. First administrator.** The admin port listens on `127.0.0.1:18443` only. Forward it (`ssh -N -L 18443:127.0.0.1:18443 user@SERVER`) and open `https://localhost:18443`. Its certificate comes from a private CA (`/opt/meerkat-outlet/configs/root-ca.pem`); trust it in a separate browser profile, or check the fingerprint and continue. Create the administrator with the code in `/opt/meerkat-outlet/configs/admin.token`. Keep that file on the server: on this port it also recovers access when every other sign-in fails.

**3. Domain.** Under **Domains**, add your domain and point a wildcard `A` record, or one per host, at the server. The console moves to `https://<name>.<domain>`, where `<name>` is a random 8-character label chosen once. Wildcard DNS only routes names; certificates are issued per hostname.

**4. Connectors.** Create a tunnel in the console and run the command it shows on the private server, then add services and public addresses on the tunnel's page:

```sh
docker run -d --restart unless-stopped --name meerkat-my-server \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp --tmpfs /var/lib/meerkat:uid=65532,gid=65532,mode=0700 \
  ghcr.io/lawnect/meerkat:latest connect <install-code>
```

The install code contains the tunnel's run token; treat it as a secret. For production, pin a release such as `ghcr.io/lawnect/meerkat:v0.6.2`, back up `/opt/meerkat-outlet` before upgrading, and follow the release notes. Database migrations run on start. See [deployment](docs/GUIDE.md#deploy-with-github-actions-and-ghcr) for upgrades and [running locally](docs/GUIDE.md#run-locally) for development.

## Security model

- **Exposure:** Caddy on TCP `80` and `443` and the tunnel on `2333`. The Outlet's admin port is bound to loopback; the console and Connector API are reached through Caddy on the admin hostname. The console there can be limited to IP ranges, answering `404` to other addresses, while the Connector API and access protection's sign-in stay open.
- **HTTP handling:** port 80 redirects only addresses the Outlet serves, and never passes plain HTTP to an app. An HTTPS request must name the host its TLS handshake asked for (`421` otherwise), and a request target that isn't a path gets `400`, so no request is routed by a host its `Host` header doesn't show. Public addresses send HSTS for a year unless it's turned off; an app's own header wins.
- **Tunnels:** Noise NK, authenticated by the Outlet's static key, with a token per service. The Outlet admits tunnel connections before rathole reads them: known Connectors pass, unknown clients are rate-limited or refused.
- **Credentials:** run tokens, Connector credentials and the admin API token are stored as SHA-256 verifiers; the setup code stays in `configs/admin.token` for recovery. Keys and secrets the Outlet must keep (the Noise key, rathole service tokens, the Telegram bot token) live in `0700` directories, key and token files with mode `0600`. The Connector keeps its credentials in tmpfs; restarting it re-enrolls with the install code.
- **Client addresses:** forgeable client-address headers (`X-Real-IP`, `Forwarded`, `CF-Connecting-IP` and the like) are dropped; `X-Real-IP` is set from the address Caddy saw.
- **Audit:** SQLite triggers reject updates and deletes of audit records. This doesn't stop someone with write access to the database file.
- **Outbound:** Caddy reaches ACME (Let's Encrypt) for certificates; the Outlet reaches your OpenID Connect providers when configured, DNS for its checks, and Telegram when alerts are on, sending alert text such as tunnel names, admin emails and client addresses. Logs, policies, credentials and certificates stay on the server.

## Documentation

- [Operator guide](docs/GUIDE.md): deployment, domains, tunnels, every feature and its limits
- [API contract](docs/API_CONTRACT.md) (Korean) and [OpenAPI](api/openapi.json)
- [Architecture](docs/ARCHITECTURE.md), [configuration rollout](docs/CONFIG_ROLLOUT.md) and [product direction](docs/PRODUCT_DIRECTION.md) (Korean)

## License

[MIT](LICENSE) © Lawnect Inc.
