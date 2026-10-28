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
  import { Alert12, Alert20, ArrowUndo, Delete, Key, NumberSymbol, Save, Send, Shield12, Warning } from '../icons';
  import type { AlertRules, AlertSettings } from '../types';
  import BannedClients from '../components/BannedClients.svelte';
  import BlockRuleSettings from '../components/BlockRuleSettings.svelte';
  import ConnectionLimitSettings from '../components/ConnectionLimitSettings.svelte';
  import RateLimitSettings from '../components/RateLimitSettings.svelte';
  import TunnelGuardSettings from '../components/TunnelGuardSettings.svelte';
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
    fromEnvironment: 'Set by environment',
    overridesEnvironment: 'Overrides environment',
    revertToEnvironment: 'Revert to environment',
    reverted: 'Reverted to the environment settings',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    rules: 'Alert rules',
    rulesNeedTelegram: 'Connect Telegram to receive these alerts.',
    tunnelDown: 'Tunnel disconnected',
    tunnelDownHint: 'After this long disconnected, and again on reconnect.',
    serviceDown: 'Service down',
    serviceDownHint: 'After this long not serving, and again on recovery.',
    afterMinutes: (rule: string) => `${rule} wait`,
    minutes: 'min',
    tunnelUnstable: 'Unstable tunnel',
    tunnelUnstableHint: 'Reconnects and tunnel process restarts within an hour at or above the threshold.',
    timesUnit: 'times',
    routeError: 'Public address error',
    routeErrorHint: 'Checked over TLS every 5 minutes, including certificate expiry.',
    dnsFailure: 'DNS failure',
    dnsFailureHint: 'Checked every 15 minutes.',
    errorSpike: '5xx spike',
    errorSpikeHint: 'Hourly 5xx share over the threshold, with at least the minimum requests.',
    thresholdOf: (rule: string) => `${rule} threshold`,
    minRequests: 'Minimum requests',
    minRequestsOf: (rule: string) => `${rule} minimum requests`,
    requestsUnit: 'req',
    latency: 'Latency increase',
    latencyHint: 'Hourly p95 above the past 7 days by the threshold, with at least the minimum requests.',
    diskLow: 'Low disk space',
    diskLowHint: 'Free space on the Outlet data disk below the threshold.',
    memoryLow: 'Low memory',
    memoryLowHint: 'Free memory below the threshold, against the container limit if set.',
    minFreeOf: (rule: string) => `${rule} minimum free`,
    outletRestart: 'Unexpected Outlet restart',
    outletRestartHint: 'On start after a crash or forced stop.',
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
      telegram_changed: 'The Telegram settings changed during the test. Send a test message again.',
      invalid_alert_rules: 'Check the alert values.',
    } as Record<string, string>,
    failed: (code: string) => `Request failed: ${code}`,
  }, {
    title: '설정',
    tabsAria: '설정 항목',
    tabAlerts: '알림',
    tabSecurity: '보안',
    loadFailed: (code: string) => `설정을 불러오는 중 오류가 발생했습니다: ${code}`,
    telegram: 'Telegram',
    verified: '인증됨',
    unverified: '인증 필요',
    notConnected: '미연결',
    botToken: '봇 토큰',
    botTokenTip: '@BotFather에서 봇을 생성한 후 발급된 토큰을 입력하십시오. 토큰은 Outlet에 저장되며 다시 표시되지 않습니다.',
    botTokenKeep: (hint: string) => `저장됨(…${hint}). 변경하려면 새 토큰을 입력하십시오.`,
    chatId: '채팅 ID',
    chatIdTip: '봇에 메시지를 전송한 후 api.telegram.org/bot<토큰>/getUpdates에서 chat.id 값을 복사하십시오. 그룹 ID는 -100으로 시작하며, 공개 채널은 @채널명을 사용할 수 있습니다.',
    sendAlerts: '알림 전송',
    sendAlertsHint: '비활성화 시 연결은 유지되며 알림 전송만 중지됩니다.',
    testMessage: '테스트 메시지 전송',
    testSent: '테스트 메시지가 전송되었습니다',
    disconnect: '연결 해제',
    disconnected: 'Telegram 연결이 해제되었습니다',
    fromEnvironment: '환경 변수로 구성됨',
    overridesEnvironment: '환경 변수 재정의됨',
    revertToEnvironment: '환경 변수 값으로 복원',
    reverted: '환경 변수 값으로 복원되었습니다',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    rules: '알림 규칙',
    rulesNeedTelegram: 'Telegram 연결 후 알림을 수신할 수 있습니다.',
    tunnelDown: '터널 연결 끊김',
    tunnelDownHint: '설정 시간 이상 연결 끊김 및 복구 시 알림',
    serviceDown: '서비스 장애',
    serviceDownHint: '설정 시간 이상 서비스 비정상 및 복구 시 알림',
    afterMinutes: (rule: string) => `${rule} 대기 시간`,
    minutes: '분',
    tunnelUnstable: '터널 불안정',
    tunnelUnstableHint: '1시간 내 재연결·프로세스 재시작 횟수가 임계값 이상인 경우 알림',
    timesUnit: '회',
    routeError: '공개 주소 오류',
    routeErrorHint: '5분 간격으로 TLS 연결 및 인증서 만료 점검',
    dnsFailure: 'DNS 조회 오류',
    dnsFailureHint: '15분 간격으로 점검',
    errorSpike: '5xx 급증',
    errorSpikeHint: '1시간 기준 5xx 응답 비율이 임계값을 초과하고 요청 수가 최소 요청 수 이상인 경우 알림',
    thresholdOf: (rule: string) => `${rule} 임계값`,
    minRequests: '최소 요청 수',
    minRequestsOf: (rule: string) => `${rule} 최소 요청 수`,
    requestsUnit: '건',
    latency: '지연 증가',
    latencyHint: '1시간 기준 p95 지연이 직전 7일 대비 임계값 이상 증가하고 요청 수가 최소 요청 수 이상인 경우 알림',
    diskLow: '디스크 여유 공간 부족',
    diskLowHint: 'Outlet 데이터 디스크 여유 공간이 임계값 미달 시 알림',
    memoryLow: '메모리 여유 부족',
    memoryLowHint: '여유 메모리가 임계값 미달 시 알림(컨테이너 제한 기준)',
    minFreeOf: (rule: string) => `${rule} 최소 여유 비율`,
    outletRestart: 'Outlet 비정상 재시작',
    outletRestartHint: '비정상 종료 후 재시작 시 알림',
    login: '관리자 로그인',
    rateLimit: '요청 차단',
    rateLimitHint: '10분간 차단 요청 수가 설정값을 초과한 경우 알림',
    minBlocked: '10분간 차단 요청 수',
    security: '로그인 보안',
    secondFactor: 'Telegram 2단계 인증',
    secondFactorHint: '비밀번호 입력 후 Telegram으로 전송된 6자리 코드를 추가로 확인합니다.',
    secondFactorTip: 'SSH로 연결한 로컬 관리 포트에서는 이 단계가 생략되므로, Telegram을 사용할 수 없는 경우에도 접속할 수 있습니다.',
    secondFactorNeedsTest: '먼저 테스트 메시지를 전송하여 Telegram 연결을 확인하십시오.',
    history: '최근 알림',
    noHistory: '전송된 알림이 없습니다',
    colTime: '시각',
    colKind: '유형',
    colMessage: '내용',
    colDelivery: '전송 상태',
    delivered: '전송됨',
    notDelivered: '실패',
    open: '진행 중',
    resolved: '해결됨',
    errors: {
      invalid_telegram_token: '봇 토큰 형식이 올바르지 않습니다(예: 123456:ABC…).',
      invalid_telegram_chat: '채팅 ID 형식이 올바르지 않습니다(숫자 또는 @채널명).',
      telegram_unauthorized: 'Telegram에서 봇 토큰이 거부되었습니다.',
      telegram_chat_not_found: 'Telegram에서 채팅을 찾을 수 없습니다. 먼저 봇에 메시지를 전송하십시오.',
      telegram_unreachable: 'Outlet에서 Telegram에 연결할 수 없습니다.',
      telegram_not_verified: '먼저 테스트 메시지를 전송하여 Telegram 연결을 확인하십시오.',
      telegram_changed: '테스트 중 Telegram 설정이 변경되었습니다. 테스트 메시지를 다시 전송하십시오.',
      invalid_alert_rules: '알림 규칙 입력값이 올바르지 않습니다.',
    },
    failed: (code: string) => `요청 처리 중 오류가 발생했습니다: ${code}`,
  });

  type RuleKey = keyof AlertRules;
  // 탭은 주소(?tab=security)가 단일 출처입니다. 감사 로그는 관찰 화면에 있습니다.
  const tabs = ['alerts', 'security'] as const;
  const tab = $derived(oneOf('tab', tabs, 'alerts'));

  const ruleKeys: RuleKey[] = ['tunnel_down', 'service_down', 'tunnel_unstable', 'route_error', 'dns_failure', 'error_spike', 'latency', 'rate_limit', 'disk_low', 'memory_low', 'outlet_restart', 'login'];
  // 한 번 알리고 끝나는 규칙은 복구 알림이 없어 기록에 열림·해결됨 표시를 달지 않습니다.
  const oneShotKinds = ['login', 'error_spike', 'rate_limit', 'latency', 'outlet_restart', 'tunnel_unstable'];

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
    const toEnvironment = telegram?.environment;
    adopt(await api<AlertSettings>('/api/v1/alerts/telegram', { method: 'DELETE' }), resetTelegram, resetSecurity);
    toast.success(toEnvironment ? t().reverted : t().disconnected);
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
    service_down: () => ({ title: t().serviceDown, hint: t().serviceDownHint }),
    tunnel_unstable: () => ({ title: t().tunnelUnstable, hint: t().tunnelUnstableHint }),
    route_error: () => ({ title: t().routeError, hint: t().routeErrorHint }),
    dns_failure: () => ({ title: t().dnsFailure, hint: t().dnsFailureHint }),
    error_spike: () => ({ title: t().errorSpike, hint: t().errorSpikeHint }),
    latency: () => ({ title: t().latency, hint: t().latencyHint }),
    rate_limit: () => ({ title: t().rateLimit, hint: t().rateLimitHint }),
    disk_low: () => ({ title: t().diskLow, hint: t().diskLowHint }),
    memory_low: () => ({ title: t().memoryLow, hint: t().memoryLowHint }),
    outlet_restart: () => ({ title: t().outletRestart, hint: t().outletRestartHint }),
    login: () => ({ title: t().login, hint: '' }),
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
  <Tabs.Root bind:value={() => tab, (next) => { setParams({ tab: next, bans_q: '', bans_page: '' }, { tab: 'alerts' }, { push: true }); }} class="min-w-0 gap-4">
    <Tabs.List aria-label={t().tabsAria}>
      <Tabs.Trigger value="alerts"><Alert12 class="size-3" aria-hidden="true" />{t().tabAlerts}</Tabs.Trigger>
      <Tabs.Trigger value="security"><Shield12 class="size-3" aria-hidden="true" />{t().tabSecurity}</Tabs.Trigger>
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
                {#if telegram?.source === 'environment'}
                  <Badge variant="outline" class="select-none">{t().fromEnvironment}</Badge>
                {:else if telegram?.source === 'database' && telegram.environment}
                  <Badge variant="outline" class="select-none">{t().overridesEnvironment}</Badge>
                {/if}
              </Card.Title>
              {#if telegram?.configured}
                <Card.Action class="flex gap-2">
                  <Button variant="outline" disabled={busy.test || telegramDirty} onclick={sendTest}>{#if busy.test}<Spinner aria-hidden="true" />{:else}<Send aria-hidden="true" />{/if}{t().testMessage}</Button>
                  {#if telegram.source === 'database' && telegram.environment}
                    <!-- 저장한 값을 지우면 환경 변수의 봇으로 돌아갑니다. -->
                    <Button variant="outline" disabled={busy.disconnect} onclick={disconnect}><ArrowUndo aria-hidden="true" />{t().revertToEnvironment}</Button>
                  {:else if telegram.source === 'database'}
                    <Button variant="destructive" disabled={busy.disconnect} onclick={disconnect}><Delete aria-hidden="true" />{t().disconnect}</Button>
                  {/if}
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
                  {#if key === 'tunnel_down' || key === 'service_down'}
                    <InputGroup.Root class="w-28">
                      <InputGroup.Input type="number" min="1" max="1440" aria-label={t().afterMinutes(text.title)} bind:value={rules[key].after_minutes} disabled={!rules[key].enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().minutes}</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
                  {:else if key === 'tunnel_unstable'}
                    <InputGroup.Root class="w-28">
                      <InputGroup.Input type="number" min="2" max="100" aria-label={t().thresholdOf(text.title)} bind:value={rules.tunnel_unstable.min_reconnects} disabled={!rules.tunnel_unstable.enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().timesUnit}</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
                  {:else if key === 'error_spike'}
                    <InputGroup.Root class="w-24">
                      <InputGroup.Input type="number" min="0.1" max="100" step="0.1" aria-label={t().thresholdOf(text.title)} bind:value={rules.error_spike.threshold_percent} disabled={!rules.error_spike.enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>%</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
                    <Tooltip.Root>
                      <Tooltip.Trigger>
                        {#snippet child({ props })}
                          <InputGroup.Root {...props} class="w-28">
                            <InputGroup.Input type="number" min="1" max="1000000" aria-label={t().minRequestsOf(text.title)} bind:value={rules!.error_spike.min_requests} disabled={!rules!.error_spike.enabled} class="tabular-nums" />
                            <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().requestsUnit}</InputGroup.Text></InputGroup.Addon>
                          </InputGroup.Root>
                        {/snippet}
                      </Tooltip.Trigger>
                      <Tooltip.Content>{t().minRequests}</Tooltip.Content>
                    </Tooltip.Root>
                  {:else if key === 'latency'}
                    <InputGroup.Root class="w-24">
                      <InputGroup.Input type="number" min="10" max="1000" aria-label={t().thresholdOf(text.title)} bind:value={rules.latency.increase_percent} disabled={!rules.latency.enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>%</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
                    <Tooltip.Root>
                      <Tooltip.Trigger>
                        {#snippet child({ props })}
                          <InputGroup.Root {...props} class="w-28">
                            <InputGroup.Input type="number" min="1" max="1000000" aria-label={t().minRequestsOf(text.title)} bind:value={rules!.latency.min_requests} disabled={!rules!.latency.enabled} class="tabular-nums" />
                            <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().requestsUnit}</InputGroup.Text></InputGroup.Addon>
                          </InputGroup.Root>
                        {/snippet}
                      </Tooltip.Trigger>
                      <Tooltip.Content>{t().minRequests}</Tooltip.Content>
                    </Tooltip.Root>
                  {:else if key === 'disk_low' || key === 'memory_low'}
                    <InputGroup.Root class="w-24">
                      <InputGroup.Input type="number" min="1" max="50" aria-label={t().minFreeOf(text.title)} bind:value={rules[key].min_free_percent} disabled={!rules[key].enabled} class="tabular-nums" />
                      <InputGroup.Addon align="inline-end"><InputGroup.Text>%</InputGroup.Text></InputGroup.Addon>
                    </InputGroup.Root>
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
                            {#if !oneShotKinds.includes(event.kind)}<Status label={event.resolved_at ? t().resolved : t().open} value={event.resolved_at ? 'connected' : 'error'} />{/if}
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
      <TunnelGuardSettings />
      <BlockRuleSettings />
      <!-- 차단된 클라이언트 표는 검색어·페이지를 주소 query(bans_q, bans_page)에 담으므로 고른 탭에서만 그립니다. -->
      {#if tab === 'security'}<BannedClients />{/if}
    </Tabs.Content>
  </Tabs.Root>
{/if}
