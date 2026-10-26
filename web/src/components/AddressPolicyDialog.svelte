<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import { toast } from 'svelte-sonner';
  import { api, connectorLabel, errorMessage, statusLabel } from '../api';
  import { translations } from '../i18n.svelte';
  import { ArrowDown, ArrowUp, Warning } from '../icons';
  import type { AddressMode, ServiceRecord } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import Status from './Status.svelte';

  // 여러 서비스가 함께 쓰는 공개 주소의 분산 방식과 대상 순서를 정합니다. 저장하면 바로 서버에 반영합니다.
  let { open = $bindable(false), hostname, records, onChanged }: { open?: boolean; hostname: string; records: ServiceRecord[]; onChanged: () => Promise<void> } = $props();

  const t = translations({
    title: 'Shared address',
    mode: 'Distribution',
    modes: { failover: 'Failover', balance: 'Load balancing' } as Record<AddressMode, string>,
    modeHints: {
      failover: 'Sends requests to the first healthy target in order.',
      balance: 'Spreads requests over healthy targets in turn.',
    } as Record<AddressMode, string>,
    sticky: 'Keep clients on one target',
    stickyHint: 'Same client IP to the same target while it is healthy',
    targets: 'Targets',
    targetsTip: 'Only healthy targets receive requests. A request without a body that can\'t reach its target is retried on the next one.',
    order: 'Priority',
    moveUp: (name: string) => `Move ${name} up`,
    moveDown: (name: string) => `Move ${name} down`,
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    changed: 'Another administrator changed this address. Review the current settings and save again.',
    failed: (code: string) => `Couldn't save the shared address: ${code}`,
  }, {
    title: '주소 공유 설정',
    mode: '분산 방식',
    modes: { failover: '장애 조치', balance: '부하 분산' },
    modeHints: {
      failover: '우선순위가 가장 높은 정상 대상으로 전달',
      balance: '정상 대상에 순서대로 분배',
    },
    sticky: '클라이언트 IP 고정',
    stickyHint: '동일 클라이언트 IP를 동일 대상으로 전달',
    targets: '대상',
    targetsTip: '정상 대상만 요청을 받습니다. 대상에 연결하지 못한 요청은 본문이 없으면 다음 대상으로 재시도합니다.',
    order: '우선순위',
    moveUp: (name: string) => `${name} 위로 이동`,
    moveDown: (name: string) => `${name} 아래로 이동`,
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    changed: '다른 관리자가 이 주소의 설정을 변경했습니다. 현재 설정을 확인한 후 다시 저장하십시오.',
    failed: (code: string) => `주소 공유 설정 저장 중 오류가 발생했습니다: ${code}`,
  });

  type Target = { routeId: string; record: ServiceRecord };
  const targetsOf = (): Target[] => records
    .flatMap((record) => record.routes.filter((route) => route.hostname === hostname).map((route) => ({ routeId: route.id, record, priority: route.priority })))
    .sort((a, b) => a.priority - b.priority)
    .map(({ routeId, record }) => ({ routeId, record }));

  let mode = $state<AddressMode>('failover');
  let sticky = $state(false);
  let targets = $state<Target[]>([]);
  let busy = $state(false);
  let error = $state('');
  // 창을 열 때 읽은 상태입니다. 저장할 때 함께 보내, 그 사이 다른 관리자가 바꿨으면 서버가 거부합니다.
  let read = $state<{ mode: AddressMode; sticky: boolean; order: string[] } | null>(null);

  function load(): void {
    const current = records.flatMap((record) => record.routes).find((route) => route.hostname === hostname);
    mode = current?.address_mode ?? 'failover';
    sticky = current?.address_sticky ?? false;
    targets = targetsOf();
    read = { mode, sticky, order: targets.map((target) => target.routeId) };
    error = '';
  }

  // 열릴 때만 서버 값으로 채웁니다. 열려 있는 동안 목록이 새로 고쳐져도 고친 값을 지우지 않습니다.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) load();
    wasOpen = open;
  });

  function move(index: number, by: number): void {
    const next = [...targets];
    [next[index], next[index + by]] = [next[index + by], next[index]];
    targets = next;
  }

  function targetStatus(record: ServiceRecord): string {
    if (record.connector.status !== 'connected') return record.connector.status;
    return record.service.observed_status;
  }

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = '';
    try {
      await api(`/api/v1/addresses/${encodeURIComponent(hostname)}`, {
        method: 'PUT',
        body: JSON.stringify({ mode, sticky: mode === 'balance' && sticky, order: targets.map((target) => target.routeId), current: read }),
      });
      await onChanged();
      toast.success(t().saved);
      open = false;
    } catch (cause) {
      const code = errorMessage(cause);
      if (code === 'address_changed') {
        await onChanged();
        load();
        error = t().changed;
      } else {
        error = t().failed(code);
      }
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open={() => open, (next) => { if (!busy) open = next; }}>
  <Dialog.Content class="sm:max-w-lg">
    <form class="grid gap-4" onsubmit={save}>
      <Dialog.Header>
        <Dialog.Title class="wrap-anywhere">{t().title}</Dialog.Title>
        <Dialog.Description class="wrap-anywhere">{hostname}</Dialog.Description>
      </Dialog.Header>
      <Field.Group>
        <Field.Set>
          <Field.Legend variant="label">{t().mode}</Field.Legend>
          <RadioGroup.Root bind:value={() => mode, (next) => { mode = next as AddressMode; }}>
            {#each ['failover', 'balance'] as const as option (option)}
              <Field.Label for={`address-mode-${option}`}>
                <Field.Field orientation="horizontal">
                  <Field.Content>
                    <Field.Title>{t().modes[option]}</Field.Title>
                    <Field.Description>{t().modeHints[option]}</Field.Description>
                  </Field.Content>
                  <RadioGroup.Item value={option} id={`address-mode-${option}`} />
                </Field.Field>
              </Field.Label>
            {/each}
          </RadioGroup.Root>
        </Field.Set>
        {#if mode === 'balance'}
          <Field.Field orientation="horizontal">
            <Field.Content>
              <Field.Label for="address-sticky">{t().sticky}</Field.Label>
              <Field.Description>{t().stickyHint}</Field.Description>
            </Field.Content>
            <Switch id="address-sticky" bind:checked={sticky} />
          </Field.Field>
        {/if}
        <Field.Set>
          <Field.Legend variant="label" class="flex items-center gap-1">{t().targets}<InfoTooltip id="address-targets-tip" label={t().targets} description={t().targetsTip} /></Field.Legend>
          <Item.Group class="gap-2" aria-label={t().order}>
            {#each targets as target, index (target.routeId)}
              {@const name = `${connectorLabel(target.record.connector)} / ${target.record.service.name}`}
              <Item.Root variant="outline" size="sm" role="listitem">
                <Item.Media class="size-6 bg-muted text-xs font-medium tabular-nums" aria-label={`${t().order} ${index + 1}`}>{index + 1}</Item.Media>
                <Item.Content class="min-w-0">
                  <Item.Title class="max-w-full min-w-0 text-sm"><span class="truncate">{name}</span></Item.Title>
                  <Item.Description class="font-mono text-xs">{target.record.service.target.host}:{target.record.service.target.port}</Item.Description>
                </Item.Content>
                <Item.Actions class="ms-auto gap-1">
                  <Status label={statusLabel(targetStatus(target.record))} value={targetStatus(target.record)} />
                  <IconButton icon={ArrowUp} label={t().moveUp(name)} disabled={index === 0 || busy} onclick={() => move(index, -1)} />
                  <IconButton icon={ArrowDown} label={t().moveDown(name)} disabled={index === targets.length - 1 || busy} onclick={() => move(index, 1)} />
                </Item.Actions>
              </Item.Root>
            {/each}
          </Item.Group>
        </Field.Set>
      </Field.Group>
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        <Button variant="outline" disabled={busy} onclick={() => { open = false; }}>{t().cancel}</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
