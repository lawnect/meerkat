<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import * as Tabs from '$lib/components/ui/tabs/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Alert12, Alert20, ArrowUndo, Clock12, Delete, Key, NumberSymbol, Save, Send, Shield12, Warning } from '../icons';
  import type { AlertRules, AlertSettings } from '../types';
  import AuditLog from '../components/AuditLog.svelte';
  import BlockRuleSettings from '../components/BlockRuleSettings.svelte';
  import ConnectionLimitSettings from '../components/ConnectionLimitSettings.svelte';
  import RateLimitSettings from '../components/RateLimitSettings.svelte';
  import InfoTooltip from '../components/InfoTooltip.svelte';
  import SsoSettings from '../components/SsoSettings.svelte';
  import PageHead from '../components/PageHead.svelte';
  import Status from '../components/Status.svelte';
  import TruncateText from '../components/TruncateText.svelte';
  import { oneOf, setParams } from '../urlState.svelte';

  const t = translations({
    title: 'Settings',
    tabsAria: 'Settings sections',
    tabAlerts: 'Alerts',
    tabSecurity: 'Security',
    tabAudit: 'Audit log',
    loadFailed: (code: string) => `Couldn't load settings: ${code}`,
    telegram: 'Telegram',
    verified: 'Verified',
    unverified: 'Not verified',
    notConnected: 'Not connected',
    botToken: 'Bot token',
    botTokenTip: 'Create a bot with @BotFather and paste the token it gives you. The token is stored on the Outlet and never shown again.',
    botTokenKeep: (hint: string) => `Saved (…${hint}). Enter a new token to replace it.`,
    chatId: 'Chat ID',
    chatIdTip: 'Send any message to the bot, then open api.telegram.org/bot<token>/getUpdates and copy chat.id. Group IDs start with -100. A public channel can use @name.',
    sendAlerts: 'Send alerts',
    sendAlertsHint: 'Turn off to keep the connection without sending alerts.',
    testMessage: 'Send test message',
    testSent: 'Test message sent',
    disconnect: 'Disconnect',
    disconnected: 'Telegram disconnected',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    rules: 'Alert rules',
    rulesNeedTelegram: 'Connect Telegram to receive these alerts.',
    tunnelDown: 'Tunnel disconnected',
    tunnelDownHint: 'After this long disconnected, and again on reconnect.',
    afterMinutes: 'Wait',
    minutes: 'min',
    routeError: 'Public address error',
    dnsFailure: 'DNS failure',
    dnsFailureHint: 'Checked every 15 minutes.',
    errorSpike: '5xx spike',
    errorSpikeHint: 'Hourly 5xx share over the threshold, with at least the minimum requests.',
    threshold: 'Threshold',
    minRequests: 'Minimum requests',
    requestsUnit: 'req',
    login: 'Admin login',
    rateLimit: 'Requests blocked',
    rateLimitHint: 'More requests than this blocked within 10 minutes.',
    minBlocked: 'Blocked requests in 10 minutes',
    security: 'Sign-in security',
    secondFactor: 'Telegram two-step verification',
    secondFactorHint: 'After the password, enter the 6-digit code sent to Telegram.',
    secondFactorTip: 'Sign-ins over the SSH-forwarded local admin port skip this step, so you can still get in if Telegram is unavailable.',
    secondFactorNeedsTest: 'Send a test message first to verify Telegram.',
    history: 'Recent alerts',
    noHistory: 'No alerts yet',
    colTime: 'Time',
    colKind: 'Type',
    colMessage: 'Message',
    colDelivery: 'Delivery',
    delivered: 'Sent',
    notDelivered: 'Failed',
    open: 'Open',
    resolved: 'Resolved',
    errors: {
      invalid_telegram_token: 'Check the bot token format (123456:ABC…).',
      invalid_telegram_chat: 'Check the chat ID (a number, or @channel).',
      telegram_unauthorized: 'Telegram rejected the bot token.',
      telegram_chat_not_found: 'Telegram couldn\'t find that chat. Send the bot a message first.',
      telegram_unreachable: 'Couldn\'t reach Telegram from the Outlet.',
      telegram_not_verified: 'Send a test message first to verify Telegram.',
      invalid_alert_rules: 'Check the alert values.',
    } as Record<string, string>,
    failed: (code: string) => `Request failed: ${code}`,
  }, {
    title: '설정',
    tabsAria: '설정 항목',
    tabAlerts: '알림',
    tabSecurity: '보안',
    tabAudit: '감사 로그',
    loadFailed: (code: string) => `설정을 불러오지 못했습니다: ${code}`,
    telegram: '텔레그램',
    verified: '확인됨',
    unverified: '확인 필요',
    notConnected: '연결 안 됨',
    botToken: '봇 토큰',
    botTokenTip: '@BotFather에서 봇을 만들고 받은 토큰을 붙여 넣으세요. 토큰은 Outlet에 저장되고 다시 표시되지 않습니다.',
    botTokenKeep: (hint: string) => `저장됨(…${hint}). 바꾸려면 새 토큰을 입력하세요.`,
    chatId: '채팅 ID',
    chatIdTip: '봇에게 아무 메시지나 보낸 뒤 api.telegram.org/bot<토큰>/getUpdates에서 chat.id를 복사하세요. 그룹 ID는 -100으로 시작합니다. 공개 채널은 @이름을 쓸 수 있습니다.',
    sendAlerts: '알림 보내기',
    sendAlertsHint: '끄면 연결은 유지하고 알림만 보내지 않습니다.',
    testMessage: '테스트 메시지 보내기',
    testSent: '테스트 메시지를 보냈습니다',
    disconnect: '연결 해제',
    disconnected: '텔레그램 연결을 해제했습니다',
    revert: '되돌리기',
    save: '저장',
    saving: '저장 중…',
    saved: '저장했습니다',
    rules: '알림 규칙',
    rulesNeedTelegram: '텔레그램을 연결하면 아래 알림을 받습니다.',
    tunnelDown: '터널 연결 끊김',
    tunnelDownHint: '설정한 시간 이상 끊기면 알리고, 복구되면 다시 알립니다.',
    afterMinutes: '대기',
    minutes: '분',
    routeError: '공개 주소 오류',
    dnsFailure: 'DNS 연결 오류',
    dnsFailureHint: '15분마다 확인합니다.',
    errorSpike: '5xx 급증',
    errorSpikeHint: '1시간 동안 5xx 비율이 기준을 넘고 요청 수가 최소 건수 이상이면 알립니다.',
    threshold: '기준',
    minRequests: '최소 요청 수',
    requestsUnit: '건',
    login: '관리자 로그인',
    rateLimit: '요청 차단',
    rateLimitHint: '10분 동안 차단된 요청이 설정한 건수를 넘으면 알립니다.',
    minBlocked: '10분간 차단된 요청 수',
    security: '로그인 보안',
    secondFactor: '텔레그램 2단계 인증',
    secondFactorHint: '비밀번호 다음에 텔레그램으로 받은 6자리 코드를 입력합니다.',
    secondFactorTip: 'SSH로 연결한 로컬 관리 포트에서는 이 단계를 건너뛰므로 텔레그램을 쓸 수 없을 때도 들어올 수 있습니다.',
    secondFactorNeedsTest: '먼저 테스트 메시지로 텔레그램을 확인하세요.',
    history: '최근 알림',
    noHistory: '아직 보낸 알림이 없습니다',
    colTime: '시각',
    colKind: '종류',
    colMessage: '내용',
    colDelivery: '전송',
    delivered: '보냄',
    notDelivered: '실패',
    open: '진행 중',
    resolved: '해결됨',
    errors: {
      invalid_telegram_token: '봇 토큰 형식을 확인하세요(123456:ABC…).',
      invalid_telegram_chat: '채팅 ID를 확인하세요(숫자 또는 @채널).',
      telegram_unauthorized: '텔레그램이 봇 토큰을 거부했습니다.',
      telegram_chat_not_found: '텔레그램에서 채팅을 찾지 못했습니다. 먼저 봇에게 메시지를 보내세요.',
      telegram_unreachable: 'Outlet에서 텔레그램에 연결하지 못했습니다.',
      telegram_not_verified: '먼저 테스트 메시지로 텔레그램을 확인하세요.',
      invalid_alert_rules: '알림 값을 확인하세요.',
    },
    failed: (code: string) => `요청하지 못했습니다: ${code}`,
  });

  type RuleKey = keyof AlertRules;
  // 탭은 주소(?tab=security)가 단일 출처입니다. 감사 로그의 검색·분류·페이지는 AuditLog가 같은 방식으로 씁니다.
  const tabs = ['alerts', 'security', 'audit'] as const;
  const tab = $derived(oneOf('tab', tabs, 'alerts'));

  const ruleKeys: RuleKey[] = ['tunnel_down', 'route_error', 'dns_failure', 'error_spike', 'rate_limit', 'login'];

  let settings = $state<AlertSettings | null>(null);
  let loadError = $state('');

  // 카드마다 바꾼 값은 저장을 누를 때까지 초안으로만 둡니다.
  let botToken = $state('');
  let chatId = $state('');
  let telegramEnabled = $state(false);
  let rules = $state<AlertRules | null>(null);
  let secondFactor = $state(false);
  let busy = $state<Record<'telegram' | 'test' | 'disconnect' | 'rules' | 'security', boolean>>({ telegram: false, test: false, disconnect: false, rules: false, security: false });
  let errors = $state<Record<'telegram' | 'rules' | 'security', string>>({ telegram: '', rules: '', security: '' });

  // 저장한 카드만 서버 값으로 되돌리고, 다른 카드에서 고치던 값은 그대로 둡니다.
  function resetTelegram(): void {
    botToken = '';
    chatId = settings?.telegram.chat_id ?? '';
    telegramEnabled = settings?.telegram.enabled ?? false;
  }
  const resetRules = () => { rules = settings ? $state.snapshot(settings.rules) : null; };
  const resetSecurity = () => { secondFactor = settings?.login_second_factor ?? false; };

  function adopt(next: AlertSettings, ...resets: Array<() => void>): void {
    settings = next;
    for (const reset of resets) reset();
  }

  const describe = (cause: unknown) => {
    const code = errorMessage(cause);
    return t().errors[code] ?? t().failed(code);
  };

  onMount(() => {
    api<AlertSettings>('/api/v1/alerts').then((next) => adopt(next, resetTelegram, resetRules, resetSecurity)).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  const telegram = $derived(settings?.telegram);
  const telegramDirty = $derived(Boolean(settings) && (botToken.trim() !== '' || chatId !== telegram?.chat_id || telegramEnabled !== telegram?.enabled));
  const rulesDirty = $derived(Boolean(settings && rules) && JSON.stringify(rules) !== JSON.stringify(settings?.rules));
  const securityDirty = $derived(Boolean(settings) && secondFactor !== settings?.login_second_factor);

  async function run(key: keyof typeof busy, work: () => Promise<void>, errorKey?: keyof typeof errors): Promise<void> {
    if (busy[key]) return;
    busy[key] = true;
    if (errorKey) errors[errorKey] = '';
    try {
      await work();
    } catch (cause) {
      if (errorKey) errors[errorKey] = describe(cause);
      else toast.error(describe(cause));
    } finally {
      busy[key] = false;
    }
  }

  const saveTelegram = (event: SubmitEvent) => {
    event.preventDefault();
    return run('telegram', async () => {
      adopt(await api<AlertSettings>('/api/v1/alerts/telegram', { method: 'PUT', body: JSON.stringify({ bot_token: botToken.trim() || undefined, chat_id: chatId.trim(), enabled: telegramEnabled }) }), resetTelegram, resetSecurity);
      toast.success(t().saved);
    }, 'telegram');
  };
  const sendTest = () => run('test', async () => {
    adopt(await api<AlertSettings>('/api/v1/alerts/telegram/test', { method: 'POST' }));
    toast.success(t().testSent);
  }, 'telegram');
  const disconnect = () => run('disconnect', async () => {
    adopt(await api<AlertSettings>('/api/v1/alerts/telegram', { method: 'DELETE' }), resetTelegram, resetSecurity);
    toast.success(t().disconnected);
  }, 'telegram');
  const saveRules = () => run('rules', async () => {
    adopt(await api<AlertSettings>('/api/v1/alerts/rules', { method: 'PUT', body: JSON.stringify(rules) }), resetRules);
    toast.success(t().saved);
  }, 'rules');
  const saveSecurity = () => run('security', async () => {
    const result = await api<{ login_second_factor: boolean }>('/api/v1/security/second-factor', { method: 'PUT', body: JSON.stringify({ enabled: secondFactor }) });
    if (settings) settings.login_second_factor = result.login_second_factor;
    secondFactor = result.login_second_factor;
    toast.success(t().saved);
  }, 'security');

  const ruleText: Record<RuleKey, () => { title: string; hint: string }> = {
    tunnel_down: () => ({ title: t().tunnelDown, hint: t().tunnelDownHint }),
    route_error: () => ({ title: t().routeError, hint: '' }),
    dns_failure: () => ({ title: t().dnsFailure, hint: t().dnsFailureHint }),
    error_spike: () => ({ title: t().errorSpike, hint: t().errorSpikeHint }),
    login: () => ({ title: t().login, hint: '' }),
    rate_limit: () => ({ title: t().rateLimit, hint: t().rateLimitHint }),
  };
  const kindLabel = (kind: string) => (ruleKeys.includes(kind as RuleKey) ? ruleText[kind as RuleKey]().title : kind);
  const timeLabel = (value: string) => new Date(value).toLocaleString(intlLocale(), { dateStyle: 'medium', timeStyle: 'short' });
</script>

<!-- 고친 값이 있을 때만 되돌리기·저장을 보여줍니다. -->
{#snippet footerActions(dirty: boolean, working: boolean, revert: () => void, submit?: () => void)}
  {#if dirty || working}
    <Card.Footer class="justify-end gap-2">
      <Button variant="outline" disabled={working} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button type={submit ? 'button' : 'submit'} disabled={working} onclick={submit}>{#if working}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    </Card.Footer>
  {/if}
{/snippet}

{#snippet errorAlert(message: string)}
  {#if message}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{message}</Alert.Description></Alert.Root>{/if}
{/snippet}

<PageHead title={t().title} />

{#if loadError}
  {@render errorAlert(loadError)}
{:else if !settings || !rules}
  <div class="grid gap-4"><Skeleton class="h-56 w-full" /><Skeleton class="h-72 w-full" /></div>
{:else}
  <Tabs.Root bind:value={() => tab, (next) => { setParams({ tab: next }, { tab: 'alerts' }); }} class="min-w-0 gap-4">
    <Tabs.List aria-label={t().tabsAria}>
      <Tabs.Trigger value="alerts"><Alert12 class="size-3" aria-hidden="true" />{t().tabAlerts}</Tabs.Trigger>
      <Tabs.Trigger value="security"><Shield12 class="size-3" aria-hidden="true" />{t().tabSecurity}</Tabs.Trigger>
      <Tabs.Trigger value="audit"><Clock12 class="size-3" aria-hidden="true" />{t().tabAudit}</Tabs.Trigger>
    </Tabs.List>
    <Tabs.Content value="alerts" class="grid min-w-0 gap-4">
        <Card.Root size="sm" role="region" aria-labelledby="telegram-title">
          <form class="contents" onsubmit={saveTelegram}>
            <Card.Header>
              <Card.Title id="telegram-title" class="flex items-center gap-2">
                {t().telegram}
                {#if !telegram?.configured}
                  <Badge variant="outline" class="text-muted-foreground select-none">{t().notConnected}</Badge>
                {:else}
                  <Status label={telegram.verified_at ? t().verified : t().unverified} value={telegram.verified_at ? 'connected' : 'connecting'} />
                {/if}
              </Card.Title>
              {#if telegram?.configured}
                <Card.Action class="flex gap-2">
                  <Button variant="outline" disabled={busy.test || telegramDirty} onclick={sendTest}>{#if busy.test}<Spinner aria-hidden="true" />{:else}<Send aria-hidden="true" />{/if}{t().testMessage}</Button>
                  <Button variant="destructive" disabled={busy.disconnect} onclick={disconnect}><Delete aria-hidden="true" />{t().disconnect}</Button>
                </Card.Action>
              {/if}
            </Card.Header>
            <Card.Content class="grid gap-4">
              {@render errorAlert(errors.telegram)}
              <Field.Group class="grid gap-4 md:grid-cols-2">
                <Field.Field>
                  <div class="flex items-center gap-1"><Field.Label for="telegram-token">{t().botToken}</Field.Label><InfoTooltip id="telegram-token-tip" label={t().botToken} description={t().botTokenTip} /></div>
                  <InputGroup.Root>
                    <InputGroup.Addon><Key aria-hidden="true" /></InputGroup.Addon>
                    <InputGroup.Input id="telegram-token" type="password" autocomplete="off" spellcheck={false} bind:value={botToken} placeholder={telegram?.configured ? t().botTokenKeep(telegram.token_hint) : '123456789:AA…'} />
                  </InputGroup.Root>
                </Field.Field>
                <Field.Field>
                  <div class="flex items-center gap-1"><Field.Label for="telegram-chat">{t().chatId}</Field.Label><InfoTooltip id="telegram-chat-tip" label={t().chatId} description={t().chatIdTip} /></div>
                  <InputGroup.Root>
                    <InputGroup.Addon><NumberSymbol aria-hidden="true" /></InputGroup.Addon>
                    <InputGroup.Input id="telegram-chat" bind:value={chatId} autocomplete="off" spellcheck={false} placeholder="-1001234567890" class="font-mono" />
                  </InputGroup.Root>
                </Field.Field>
              </Field.Group>
              <Field.Field orientation="horizontal">
                <Field.Content>
                  <div class="flex items-center gap-1"><Field.Label for="telegram-enabled">{t().sendAlerts}</Field.Label><InfoTooltip id="telegram-enabled-tip" label={t().sendAlerts} description={t().sendAlertsHint} /></div>
                </Field.Content>
                <Switch id="telegram-enabled" bind:checked={telegramEnabled} />
              </Field.Field>
            </Card.Content>
            {@render footerActions(telegramDirty, busy.telegram, resetTelegram)}
          </form>
        </Card.Root>

        <Card.Root size="sm" role="region" aria-labelledby="rules-title">
          <Card.Header>
            <Card.Title id="rules-title">{t().rules}</Card.Title>
            {#if !telegram?.configured || !telegram.enabled}<Card.Description>{t().rulesNeedTelegram}</Card.Description>{/if}
          </Card.Header>
          <Card.Content class="grid gap-4">
            {@render errorAlert(errors.rules)}
            <Field.Group class="gap-0">
              {#each ruleKeys as key, index (key)}
                {@const text = ruleText[key]()}
                {#if index}<Field.Separator class="my-3" />{/if}
                <Field.Field orientation="horizontal" class="flex-wrap items-center gap-y-2">
                  <Field.Content class="min-w-48">
                    <Field.Label for={`rule-${key}`}>{text.title}</Field.Label>
                    {#if text.hint}<Field.Description>{text.hint}</Field.Description>{/if}
                  </Field.Content>
                  {#if key === 'tunnel_down'}
                    <InputGroup.Root class="w-28">
                      <InputGroup.Input type="number" min="1" max="1440" aria-label={t().afterMinutes} bind:value={rules.tunnel_down.after_minutes} disabled={!rules.tunnel_down.enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().minutes}</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
                  {:else if key === 'error_spike'}
                    <InputGroup.Root class="w-24">
                      <InputGroup.Input type="number" min="0.1" max="100" step="0.1" aria-label={t().threshold} bind:value={rules.error_spike.threshold_percent} disabled={!rules.error_spike.enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>%</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
                    <Tooltip.Root>
                      <Tooltip.Trigger>
                        {#snippet child({ props })}
                          <InputGroup.Root {...props} class="w-28">
                            <InputGroup.Input type="number" min="1" max="1000000" aria-label={t().minRequests} bind:value={rules!.error_spike.min_requests} disabled={!rules!.error_spike.enabled} class="tabular-nums" />
                            <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().requestsUnit}</InputGroup.Text></InputGroup.Addon>
                          </InputGroup.Root>
                        {/snippet}
                      </Tooltip.Trigger>
                      <Tooltip.Content>{t().minRequests}</Tooltip.Content>
                    </Tooltip.Root>
                  {:else if key === 'rate_limit'}
                    <Tooltip.Root>
                      <Tooltip.Trigger>
                        {#snippet child({ props })}
                          <InputGroup.Root {...props} class="w-28">
                            <InputGroup.Input type="number" min="1" max="1000000" aria-label={t().minBlocked} bind:value={rules!.rate_limit.min_blocked} disabled={!rules!.rate_limit.enabled} class="tabular-nums" />
                            <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().requestsUnit}</InputGroup.Text></InputGroup.Addon>
                          </InputGroup.Root>
                        {/snippet}
                      </Tooltip.Trigger>
                      <Tooltip.Content>{t().minBlocked}</Tooltip.Content>
                    </Tooltip.Root>
                  {/if}
                  <Switch id={`rule-${key}`} bind:checked={rules[key].enabled} />
                </Field.Field>
              {/each}
            </Field.Group>
          </Card.Content>
          {@render footerActions(rulesDirty, busy.rules, resetRules, saveRules)}
        </Card.Root>

        <Card.Root size="sm" role="region" aria-labelledby="history-title">
          <Card.Header>
            <Card.Title id="history-title">{t().history}</Card.Title>
          </Card.Header>
          <Card.Content>
            {#if settings.events.length}
              <div class="overflow-hidden border">
                <Table.Root>
                  <Table.Header class="bg-muted">
                    <Table.Row class="hover:bg-transparent">
                      <Table.Head class="pl-4">{t().colTime}</Table.Head>
                      <Table.Head>{t().colKind}</Table.Head>
                      <Table.Head>{t().colMessage}</Table.Head>
                      <Table.Head class="pr-4">{t().colDelivery}</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {#each settings.events as event (event.id)}
                      <Table.Row>
                        <Table.Cell class="pl-4 text-muted-foreground tabular-nums">{timeLabel(event.opened_at)}</Table.Cell>
                        <Table.Cell>
                          <span class="flex items-center gap-2">{kindLabel(event.kind)}
                            {#if event.kind !== 'login'}<Status label={event.resolved_at ? t().resolved : t().open} value={event.resolved_at ? 'connected' : 'error'} />{/if}
                          </span>
                        </Table.Cell>
                        <Table.Cell class="max-w-96"><TruncateText text={event.message} /></Table.Cell>
                        <Table.Cell class="pr-4">
                          {#if event.sent}
                            <Status label={t().delivered} value="connected" />
                          {:else}
                            <Tooltip.Root>
                              <Tooltip.Trigger class="inline-flex outline-none focus-visible:ring-3 focus-visible:ring-ring/50"><Status label={t().notDelivered} value="error" /></Tooltip.Trigger>
                              <Tooltip.Content>{t().errors[event.error ?? ''] ?? event.error ?? t().notDelivered}</Tooltip.Content>
                            </Tooltip.Root>
                          {/if}
                        </Table.Cell>
                      </Table.Row>
                    {/each}
                  </Table.Body>
                </Table.Root>
              </div>
            {:else}
              <Empty.Root class="border border-dashed">
                <Empty.Header>
                  <Empty.Media variant="icon"><Alert20 class="size-5" aria-hidden="true" /></Empty.Media>
                  <Empty.Title>{t().noHistory}</Empty.Title>
                </Empty.Header>
              </Empty.Root>
            {/if}
          </Card.Content>
        </Card.Root>
    </Tabs.Content>
    <Tabs.Content value="security" class="grid min-w-0 gap-4">
        <Card.Root size="sm" role="region" aria-labelledby="security-title">
          <Card.Header>
            <Card.Title id="security-title">{t().security}</Card.Title>
          </Card.Header>
          <Card.Content class="grid gap-4">
            {@render errorAlert(errors.security)}
            <Field.Field orientation="horizontal">
              <Field.Content>
                <div class="flex items-center gap-1"><Field.Label for="second-factor">{t().secondFactor}</Field.Label><InfoTooltip id="second-factor-tip" label={t().secondFactor} description={`${t().secondFactorHint} ${t().secondFactorTip}`} /></div>
                {#if !telegram?.verified_at}<Field.Description>{t().secondFactorNeedsTest}</Field.Description>{/if}
              </Field.Content>
              <Switch id="second-factor" bind:checked={secondFactor} disabled={!telegram?.verified_at && !settings.login_second_factor} />
            </Field.Field>
          </Card.Content>
          {@render footerActions(securityDirty, busy.security, resetSecurity, saveSecurity)}
        </Card.Root>

      <SsoSettings />
      <RateLimitSettings />
      <ConnectionLimitSettings />
      <BlockRuleSettings />
    </Tabs.Content>
    <Tabs.Content value="audit" class="grid min-w-0 gap-4">
      <AuditLog />
    </Tabs.Content>
  </Tabs.Root>
{/if}
