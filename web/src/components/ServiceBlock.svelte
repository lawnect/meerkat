<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { Add, ArrowRight, ArrowRight12, Cube, Delete, Edit, Globe, Globe20, NumberSymbol, Open, Open12, Save, Storage, Warning } from '../icons';
  import { api, errorMessage, statusLabel } from '../api';
  import type { Outlet, ServiceRecord } from '../types';
  import ConfirmNameDialog from './ConfirmNameDialog.svelte';
  import CopyText from './CopyText.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import Status from './Status.svelte';
  import TruncateText from './TruncateText.svelte';
  import { translations } from '../i18n.svelte';
  import { pageHref } from '../navigation';

  let { record, outlet, onChanged, view, onViewRoutes }: { record: ServiceRecord; outlet: Outlet; onChanged: () => Promise<void>; view: 'service' | 'routes'; onViewRoutes?: () => void } = $props();

  const t = translations({
    viewRoutes: (n: number) => `${n} public ${n === 1 ? 'address' : 'addresses'}`,
    editTarget: 'Edit target',
    editDesc: (name: string) => `Edit the internal address of ${name}.`,
    deleteRoutesFirst: 'Delete the public addresses first',
    healthNote: 'Check the app host, internal port, and Docker network.',
    appHost: 'App host',
    internalPort: 'Internal port',
    editNote: 'For a Compose app, the app host is the name under services: and the internal port is the port the container listens on. The tunnel and the app must be on the same Docker network.',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    invalidService: 'Check the app host and internal port.',
    serviceTargetTaken: 'Another service on the same tunnel already uses this app host and port.',
    saveFailed: (code: string) => `Couldn't save the target: ${code}`,
    deleteServiceFailed: (code: string) => `Couldn't delete the service: ${code}`,
    deleteRouteFailed: (code: string) => `Couldn't delete the public address: ${code}`,
    hostnameTaken: 'That address is already in use. Enter a different subdomain.',
    reservedHostname: 'This address is reserved for the Outlet admin.',
    invalidSubdomain: 'Check the subdomain. Only letters, numbers, and hyphens are allowed.',
    addRouteFailed: (code: string) => `Couldn't create the public address: ${code}`,
    routeDisabled: 'Disabled',
    routeConfigError: 'Configuration error',
    routeApplying: 'Applying',
    routeServiceDisabled: 'Service disabled',
    routeWaitingTunnel: 'Waiting for tunnel',
    routeWaitingService: 'Waiting for service',
    routeLive: 'Live',
    routeDisabledTip: 'This address is turned off.',
    routeConfigErrorTip: 'The Outlet couldn\'t apply this address.',
    routeApplyingTip: 'The Outlet is still applying this address.',
    routeServiceDisabledTip: 'The service is turned off, so this address isn\'t serving traffic.',
    routeWaitingTunnelTip: 'The tunnel isn\'t connected yet. Once it connects, this address goes live.',
    routeWaitingServiceTip: 'The tunnel is connected, but the app isn\'t responding at the target address.',
    routeLiveTip: 'Requests to this address reach the service.',
    addAddress: 'Add address',
    openInNewTab: (hostname: string) => `Open https://${hostname} in a new tab`,
    deleteAddress: (hostname: string) => `Delete ${hostname}`,
    noRoutes: 'No public addresses yet',
    noDomain: 'No domain set',
    needDomain: 'Add a domain to create public addresses.',
    domains: 'Domains',
    addTitle: 'Add a public address',
    addDesc: 'Choose a domain and enter a subdomain.',
    domain: 'Domain',
    subdomain: 'Subdomain',
    preview: 'Address to be created:',
    adding: 'Adding…',
    addSubmit: 'Add address',
    deleteServiceTitle: 'Delete service',
    deleteServiceWarning: 'This can\'t be undone. Other services on the same tunnel may briefly reconnect. Your actual app isn\'t deleted.',
    deleteServiceSubmit: 'Delete service',
    deleteRouteTitle: 'Delete public address',
    deleteRouteWarning: 'This can\'t be undone. Access through this address will stop. Your actual service isn\'t deleted.',
    deleteRouteSubmit: 'Delete public address',
  }, {
    viewRoutes: (n: number) => `공개 주소 ${n}개`,
    editTarget: '연결 대상 수정',
    editDesc: (name: string) => `${name}의 내부 주소를 수정합니다.`,
    deleteRoutesFirst: '공개 주소를 먼저 삭제하세요',
    healthNote: '앱 호스트·내부 포트·Docker 네트워크를 확인하세요.',
    appHost: '앱 호스트',
    internalPort: '내부 포트',
    editNote: 'Compose 앱이라면 앱 호스트는 services: 아래 이름, 내부 포트는 컨테이너에서 듣는 포트입니다. 터널과 앱이 같은 Docker 네트워크에 있어야 합니다.',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    invalidService: '앱 호스트와 내부 포트를 확인하세요.',
    serviceTargetTaken: '같은 터널의 다른 서비스가 이 앱 호스트와 포트를 사용 중입니다.',
    saveFailed: (code: string) => `연결 대상을 저장하지 못했습니다: ${code}`,
    deleteServiceFailed: (code: string) => `서비스를 삭제하지 못했습니다: ${code}`,
    deleteRouteFailed: (code: string) => `공개 주소를 삭제하지 못했습니다: ${code}`,
    hostnameTaken: '이미 사용 중인 주소입니다. 다른 하위 도메인을 입력하세요.',
    reservedHostname: '이 주소는 Outlet 관리 화면에 예약되어 있습니다.',
    invalidSubdomain: '하위 도메인을 확인하세요. 영문자, 숫자, 하이픈만 사용할 수 있습니다.',
    addRouteFailed: (code: string) => `공개 주소를 만들지 못했습니다: ${code}`,
    routeDisabled: '사용 안 함',
    routeConfigError: '설정 오류',
    routeApplying: '반영 중',
    routeServiceDisabled: '서비스 사용 안 함',
    routeWaitingTunnel: '터널 연결 대기',
    routeWaitingService: '서비스 응답 대기',
    routeLive: '공개 중',
    routeDisabledTip: '이 주소는 꺼져 있습니다.',
    routeConfigErrorTip: 'Outlet이 이 주소를 반영하지 못했습니다.',
    routeApplyingTip: 'Outlet이 이 주소를 아직 반영하는 중입니다.',
    routeServiceDisabledTip: '서비스가 꺼져 있어 이 주소로 요청을 전달하지 않습니다.',
    routeWaitingTunnelTip: '터널이 아직 연결되지 않았습니다. 연결되면 이 주소가 공개됩니다.',
    routeWaitingServiceTip: '터널은 연결됐지만 연결 대상에서 앱이 응답하지 않습니다.',
    routeLiveTip: '이 주소로 들어온 요청이 서비스에 전달됩니다.',
    addAddress: '주소 추가',
    openInNewTab: (hostname: string) => `https://${hostname} 새 탭에서 열기`,
    deleteAddress: (hostname: string) => `${hostname} 주소 삭제`,
    noRoutes: '아직 공개 주소가 없습니다',
    noDomain: '도메인 미설정',
    needDomain: '도메인을 추가하면 공개 주소를 만들 수 있습니다.',
    domains: '도메인 관리',
    addTitle: '공개 주소 추가',
    addDesc: '도메인을 선택하고 하위 도메인을 입력하세요.',
    domain: '도메인',
    subdomain: '하위 도메인',
    preview: '생성될 주소:',
    adding: '추가 중…',
    addSubmit: '주소 추가',
    deleteServiceTitle: '서비스 삭제',
    deleteServiceWarning: '삭제 후에는 되돌릴 수 없습니다. 같은 터널의 다른 서비스도 잠시 다시 연결될 수 있습니다. 실제 앱은 삭제되지 않습니다.',
    deleteServiceSubmit: '서비스 삭제',
    deleteRouteTitle: '공개 주소 삭제',
    deleteRouteWarning: '삭제 후에는 되돌릴 수 없습니다. 이 주소로의 접속이 중단됩니다. 실제 서비스는 삭제되지 않습니다.',
    deleteRouteSubmit: '공개 주소 삭제',
  });

  let subdomain = $state('');
  let selectedDomain = $state('');
  let addError = $state('');
  let routeError = $state('');
  let busy = $state(false);
  let deletingRoute = $state('');
  let deleteRouteTarget = $state<ServiceRecord['routes'][number] | null>(null);
  let deleteRouteOpen = $state(false);
  let editingTarget = $state(false);
  let editHost = $state('');
  let editPort = $state('');
  let editBusy = $state(false);
  let editError = $state('');
  let deletingService = $state(false);
  let deleteServiceOpen = $state(false);
  let serviceError = $state('');
  let routeOpen = $state(false);
  const domains = $derived(outlet.domains?.length ? outlet.domains : outlet.domain ? [outlet.domain] : []);
  const baseDomain = $derived(domains.includes(selectedDomain) ? selectedDomain : domains[0] || '');
  const routeHostname = $derived(subdomain.trim().toLowerCase() && baseDomain ? `${subdomain.trim().toLowerCase()}.${baseDomain}` : '');
  const hasRoutes = $derived(record.routes.length > 0);
  const targetAddress = $derived(`${record.service.target.host.includes(':') ? `[${record.service.target.host}]` : record.service.target.host}:${record.service.target.port}`);

  function setEditOpen(next: boolean): void {
    if (!next && editBusy) return;
    editingTarget = next;
  }

  function setRouteOpen(next: boolean): void {
    if (!next && busy) return;
    routeOpen = next;
  }

  function startTargetEdit(): void {
    editHost = record.service.target.host;
    editPort = String(record.service.target.port);
    editError = '';
    editingTarget = true;
  }

  async function saveTarget(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (editBusy) return;
    editBusy = true;
    editError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ target: { host: editHost.trim(), port: Number(editPort) } }),
      });
      editingTarget = false;
      await onChanged();
    } catch (cause) {
      const code = errorMessage(cause);
      editError = code === 'invalid_service'
        ? t().invalidService
        : code === 'service_target_taken'
          ? t().serviceTargetTaken
          : t().saveFailed(code);
    } finally {
      editBusy = false;
    }
  }

  function openDeleteService(): void {
    if (deletingService || record.routes.length) return;
    serviceError = '';
    deleteServiceOpen = true;
  }

  async function deleteService(): Promise<void> {
    if (deletingService || record.routes.length) return;
    deletingService = true;
    serviceError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, { method: 'DELETE' });
      await onChanged();
      deleteServiceOpen = false;
    } catch (cause) {
      serviceError = t().deleteServiceFailed(errorMessage(cause));
    } finally {
      deletingService = false;
    }
  }

  function openRouteDialog(): void {
    subdomain = '';
    selectedDomain = '';
    addError = '';
    routeOpen = true;
  }

  async function addRoute(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!baseDomain || !routeHostname) return;
    addError = '';
    busy = true;
    try {
      await api(`/api/v1/services/${encodeURIComponent(record.service.id)}/routes`, {
        method: 'POST',
        body: JSON.stringify({ hostname: routeHostname }),
      });
      await onChanged();
      routeOpen = false;
    } catch (cause) {
      const code = errorMessage(cause);
      addError = code === 'hostname_taken' ? t().hostnameTaken : code === 'invalid_route' && outlet.admin_hostname && routeHostname === outlet.admin_hostname ? t().reservedHostname : code === 'invalid_route' ? t().invalidSubdomain : t().addRouteFailed(code);
    } finally {
      busy = false;
    }
  }

  function openDeleteRoute(route: ServiceRecord['routes'][number]): void {
    if (deletingRoute) return;
    deleteRouteTarget = route;
    routeError = '';
    deleteRouteOpen = true;
  }

  async function deleteRoute(): Promise<void> {
    if (deletingRoute || !deleteRouteTarget) return;
    const route = deleteRouteTarget;
    routeError = '';
    deletingRoute = route.id;
    try {
      await api(`/api/v1/services/${encodeURIComponent(record.service.id)}/routes/${encodeURIComponent(route.id)}`, { method: 'DELETE' });
      await onChanged();
      deleteRouteOpen = false;
    } catch (cause) {
      routeError = t().deleteRouteFailed(errorMessage(cause));
    } finally {
      deletingRoute = '';
    }
  }

  // value는 Status 배지의 색을 정하는 상태 값이고, 꺼진 주소(off)는 색 없는 배지로 보여줍니다.
  function routeStatus(route: ServiceRecord['routes'][number]): { label: string; tip: string; value: string; off: boolean } {
    if (!route.enabled) return { label: t().routeDisabled, tip: t().routeDisabledTip, value: 'disabled', off: true };
    if (route.last_error) return { label: t().routeConfigError, tip: route.last_error || t().routeConfigErrorTip, value: 'error', off: false };
    if (route.applied_revision < route.desired_revision) return { label: t().routeApplying, tip: t().routeApplyingTip, value: 'connecting', off: false };
    if (!record.service.enabled) return { label: t().routeServiceDisabled, tip: t().routeServiceDisabledTip, value: 'disabled', off: true };
    if (record.connector.status !== 'connected') return { label: t().routeWaitingTunnel, tip: t().routeWaitingTunnelTip, value: 'connecting', off: false };
    if (record.service.observed_status !== 'connected') return { label: t().routeWaitingService, tip: t().routeWaitingServiceTip, value: 'connecting', off: false };
    return { label: t().routeLive, tip: t().routeLiveTip, value: 'connected', off: false };
  }
</script>

{#snippet serviceStatus()}
  {#if record.service.observed_status === 'error'}
    <Tooltip.Root>
      <Tooltip.Trigger>
        {#snippet child({ props })}
          <span {...props} class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50" tabindex="0"><Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} /></span>
        {/snippet}
      </Tooltip.Trigger>
      <Tooltip.Content>{t().healthNote}</Tooltip.Content>
    </Tooltip.Root>
  {:else}
    <Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} />
  {/if}
{/snippet}

{#snippet routeBadge(route: ServiceRecord['routes'][number])}
  {@const status = routeStatus(route)}
  <Tooltip.Root>
    <Tooltip.Trigger>
      {#snippet child({ props })}
        <span {...props} class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50" tabindex="0" aria-describedby={`route-status-${route.id}`}>
          {#if status.off}
            <Badge variant="outline" class="text-muted-foreground select-none">{status.label}</Badge>
          {:else}
            <Status label={status.label} value={status.value} />
          {/if}
        </span>
      {/snippet}
    </Tooltip.Trigger>
    <Tooltip.Content>{status.tip}</Tooltip.Content>
  </Tooltip.Root>
  <span id={`route-status-${route.id}`} class="sr-only">{status.tip}</span>
{/snippet}

{#if view === 'service'}
  <Item.Root variant="outline" size="sm" role="listitem">
    <Item.Media variant="icon"><Cube class="text-muted-foreground" aria-hidden="true" /></Item.Media>
    <Item.Content class="min-w-0 basis-48">
      <Item.Title class="max-w-full min-w-0 text-sm">
        <TruncateText text={record.service.name} />
        {@render serviceStatus()}
      </Item.Title>
      <Item.Description><CopyText value={targetAddress} /></Item.Description>
    </Item.Content>
    <Item.Actions class="ms-auto gap-1">
      {#if onViewRoutes}
        <Button variant="outline" size="sm" onclick={onViewRoutes}>{t().viewRoutes(record.routes.length)}<ArrowRight12 aria-hidden="true" /></Button>
      {/if}
      <IconButton icon={Edit} label={t().editTarget} onclick={startTargetEdit} />
      <IconButton
        icon={Delete}
        label={hasRoutes ? t().deleteRoutesFirst : t().deleteServiceTitle}
        disabled={deletingService}
        aria-disabled={hasRoutes ? true : undefined}
        class="aria-disabled:cursor-not-allowed aria-disabled:opacity-50 aria-disabled:hover:bg-transparent"
        onclick={openDeleteService}
      />
    </Item.Actions>
  </Item.Root>
{:else}
  <Card.Root size="sm">
    <Card.Header>
      <Card.Title><TruncateText text={record.service.name} /></Card.Title>
      <Card.Description><CopyText value={targetAddress} /></Card.Description>
      {#if baseDomain}
        <Card.Action><Button variant="outline" onclick={openRouteDialog}><Add aria-hidden="true" />{t().addAddress}</Button></Card.Action>
      {/if}
    </Card.Header>
    <Card.Content class="grid min-w-0 gap-4">
      {#if record.routes.length}
        <Item.Group class="gap-2">
          {#each record.routes as route (route.id)}
            <Item.Root variant="outline" size="sm" role="listitem">
              <Item.Media variant="icon"><Globe class="text-muted-foreground" aria-hidden="true" /></Item.Media>
              <Item.Content class="min-w-0">
                <Item.Title class="max-w-full min-w-0 text-sm">
                  <a
                    href={`https://${route.hostname}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    class="inline-flex min-w-0 items-center gap-1.5 outline-none text-link focus-visible:ring-3 focus-visible:ring-ring/50"
                    aria-label={t().openInNewTab(route.hostname)}
                  >
                    <span class="wrap-anywhere">{route.hostname}</span>
                    <Open12 class="size-3 shrink-0 text-muted-foreground" aria-hidden="true" />
                  </a>
                </Item.Title>
              </Item.Content>
              <Item.Actions class="ms-auto gap-1">
                {@render routeBadge(route)}
                <IconButton icon={Delete} label={t().deleteAddress(route.hostname)} disabled={Boolean(deletingRoute)} onclick={() => openDeleteRoute(route)} />
              </Item.Actions>
            </Item.Root>
          {/each}
        </Item.Group>
      {:else if baseDomain}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{t().noRoutes}</Empty.Title>
          </Empty.Header>
        </Empty.Root>
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{t().noDomain}</Empty.Title>
            <Empty.Description>{t().needDomain}</Empty.Description>
          </Empty.Header>
          <Empty.Content><Button variant="outline" href={pageHref('domains')}>{t().domains}<ArrowRight aria-hidden="true" /></Button></Empty.Content>
        </Empty.Root>
      {/if}
      {#if routeError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{routeError}</Alert.Description></Alert.Root>{/if}
    </Card.Content>
  </Card.Root>
{/if}

<ConfirmNameDialog id={`delete-service-${view}-${record.service.id}`} open={deleteServiceOpen} title={t().deleteServiceTitle} warning={t().deleteServiceWarning} targetName={record.service.name} submitLabel={t().deleteServiceSubmit} busy={deletingService} error={serviceError} onConfirm={deleteService} onClose={() => { deleteServiceOpen = false; serviceError = ''; }} />
<ConfirmNameDialog id={`delete-route-${view}-${record.service.id}`} open={deleteRouteOpen} title={t().deleteRouteTitle} warning={t().deleteRouteWarning} targetName={deleteRouteTarget?.hostname || ''} submitLabel={t().deleteRouteSubmit} busy={Boolean(deletingRoute)} error={routeError} onConfirm={deleteRoute} onClose={() => { deleteRouteOpen = false; deleteRouteTarget = null; routeError = ''; }} />

{#if view === 'service'}
  <Dialog.Root bind:open={() => editingTarget, setEditOpen}>
    <Dialog.Content class="sm:max-w-md">
      <form class="grid gap-4" onsubmit={saveTarget}>
        <Dialog.Header>
          <Dialog.Title>{t().editTarget}</Dialog.Title>
          <Dialog.Description class="sr-only">{t().editDesc(record.service.name)}</Dialog.Description>
        </Dialog.Header>
        <Field.Group>
          <div class="grid gap-4 sm:grid-cols-[minmax(0,1fr)_8rem]">
            <Field.Field>
              <div class="flex items-center gap-1">
                <Field.Label for={`edit-host-${record.service.id}`}>{t().appHost}</Field.Label>
                <InfoTooltip id={`edit-help-${record.service.id}`} label={t().appHost} description={t().editNote} />
              </div>
              <InputGroup.Root>
                <InputGroup.Addon><Storage aria-hidden="true" /></InputGroup.Addon>
                <InputGroup.Input id={`edit-host-${record.service.id}`} aria-describedby={`edit-help-${record.service.id}`} bind:value={editHost} autocapitalize="none" spellcheck={false} required />
              </InputGroup.Root>
            </Field.Field>
            <Field.Field>
              <div class="flex h-5 items-center"><Field.Label for={`edit-port-${record.service.id}`}>{t().internalPort}</Field.Label></div>
              <InputGroup.Root>
                <InputGroup.Addon><NumberSymbol aria-hidden="true" /></InputGroup.Addon>
                <InputGroup.Input id={`edit-port-${record.service.id}`} type="number" min="1" max="65535" bind:value={editPort} required />
              </InputGroup.Root>
            </Field.Field>
          </div>
        </Field.Group>
        {#if editError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{editError}</Alert.Description></Alert.Root>{/if}
        <Dialog.Footer>
          <Button variant="outline" disabled={editBusy} onclick={() => setEditOpen(false)}>{t().cancel}</Button>
          <Button type="submit" disabled={editBusy}>{#if editBusy}<Spinner />{t().saving}{:else}{t().save}{/if}</Button>
        </Dialog.Footer>
      </form>
    </Dialog.Content>
  </Dialog.Root>
{:else if baseDomain}
  <Dialog.Root bind:open={() => routeOpen, setRouteOpen}>
    <Dialog.Content class="sm:max-w-md">
      <form class="grid gap-4" onsubmit={addRoute}>
        <Dialog.Header>
          <Dialog.Title>{t().addTitle}</Dialog.Title>
          <Dialog.Description class="sr-only">{t().addDesc}</Dialog.Description>
        </Dialog.Header>
        <Field.Group>
          <Field.Field>
            <Field.Label for={`domain-${record.service.id}`}>{t().domain}</Field.Label>
            <NativeSelect.Root id={`domain-${record.service.id}`} class="w-full" bind:value={() => baseDomain, (next) => { selectedDomain = next; }}>
              {#each domains as domain (domain)}
                <NativeSelect.Option value={domain}>{domain}</NativeSelect.Option>
              {/each}
            </NativeSelect.Root>
          </Field.Field>
          <Field.Field>
            <Field.Label for={`route-${record.service.id}`}>{t().subdomain}</Field.Label>
            <InputGroup.Root>
              <InputGroup.Addon><Globe aria-hidden="true" /></InputGroup.Addon>
              <InputGroup.Input id={`route-${record.service.id}`} aria-describedby={`domain-suffix-${record.service.id}`} bind:value={subdomain} placeholder="app" pattern={'[a-zA-Z0-9](?:[a-zA-Z0-9\\-]{0,61}[a-zA-Z0-9])?'} maxlength={63} autocapitalize="none" spellcheck={false} required />
              <InputGroup.Addon align="inline-end"><InputGroup.Text id={`domain-suffix-${record.service.id}`}>.{baseDomain}</InputGroup.Text></InputGroup.Addon>
            </InputGroup.Root>
            {#if routeHostname}
              <Field.Description><span class="sr-only">{t().preview}</span><span class="break-all text-foreground">https://{routeHostname}</span></Field.Description>
            {/if}
          </Field.Field>
        </Field.Group>
        {#if addError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{addError}</Alert.Description></Alert.Root>{/if}
        <Dialog.Footer>
          <Button variant="outline" disabled={busy} onclick={() => setRouteOpen(false)}>{t().cancel}</Button>
          <Button type="submit" disabled={busy}>{#if busy}<Spinner />{t().adding}{:else}{t().addSubmit}{/if}</Button>
        </Dialog.Footer>
      </form>
    </Dialog.Content>
  </Dialog.Root>
{/if}
