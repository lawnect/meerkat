# Project instructions

## Frontend UI work

Before changing UI in `web/src`, read `web/src/components/README.md`.

Build screens with shadcn-svelte components from `web/src/lib/components/ui/` (managed by the shadcn-svelte CLI; add new ones with `npx shadcn-svelte@latest add <name>` from the repo root and swap their Tabler icons for Fluent icons from `web/src/icons.ts`). Follow the documented shadcn-svelte usage for each component instead of hand-styling equivalents. The style is lyra with `--radius: 0`: don't add `rounded-*` classes in app code. App-level composites (`IconButton`, `CopyText`, `TruncateText`, `InfoTooltip`, `ConfirmNameDialog`, `Status`, `PageHead`, `StatCard`, `TrafficChart`, `TrendChart`, `ShareChart`, `TagInput`, `AddressSelect`, `PeriodControls`, `LogCard`, `LogSettingsDialog`, `CsvDownload`, `CauseBadge`, and the `data-table/` parts) live in `web/src/components/`.

Use theme tokens only (`bg-background`, `text-muted-foreground`, `border`, …) so the light and dark themes both work. The Outlet sends a strict CSP (`style-src 'self'`, `script-src 'self'`): never add static `style="…"` attributes, inline `<style>` or `<script>`.

Every user-visible string goes through `translations(en, ko)` from `web/src/i18n.svelte.ts`. English is the default at `/`; Korean pages live under `/ko`, so build in-app links with `pageHref()` or `connectorDetailPath()`.

Preserve HTML semantics and accessibility: actions are buttons, URL navigation is a link, and disabled/loading states remain clear.
