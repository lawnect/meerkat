<script lang="ts">
  import { onMount } from 'svelte';
  import * as Tabs from '$lib/components/ui/tabs/index.js';
  import { api } from '../api';
  import AccessPolicySettings from '../components/AccessPolicySettings.svelte';
  import AccessProviderSettings from '../components/AccessProviderSettings.svelte';
  import AccessTokenSettings from '../components/AccessTokenSettings.svelte';
  import IpAddressSettings from '../components/IpAddressSettings.svelte';
  import PageHead from '../components/PageHead.svelte';
  import { translations } from '../i18n.svelte';
  import { Globe12, PersonCircle12, Shield12 } from '../icons';
  import type { AccessProviderView, AccessToken, IpTagUse } from '../types';
  import { oneOf, setParams } from '../urlState.svelte';

  // 접근 보호 화면. 자주 고치는 정책과 정책이 쓰는 접근 토큰을 첫 탭에, 정책과 관리 콘솔 접근 제한이 태그로 쓰는 IP 주소록을
  // 다음 탭에, 한 번 설정하는 접근 보호 SSO를 마지막 탭에 둡니다. 정책이 판정한 요청의 지표와 기록은 관찰 화면에 있습니다.
  // 탭은 주소(?tab=ips, ?tab=sso)가 단일 출처입니다. 주소록 카드는 숨겨진 탭에서도 그려져 정책 편집기의 태그 제안을 채웁니다.
  const t = translations({
    title: 'Access protection',
    tabsAria: 'Access protection sections',
    tabPolicies: 'Access policies',
    tabIps: 'IP address book',
    tabSso: 'Access SSO',
  }, {
    title: '접근 보호',
    tabsAria: '접근 보호 항목',
    tabPolicies: '접근 정책',
    tabIps: 'IP 주소록',
    tabSso: '접근 보호 SSO',
  });

  const tabs = ['policies', 'ips', 'sso'] as const;
  const tab = $derived(oneOf('tab', tabs, 'policies'));

  // 정책 카드는 SSO가 켜져 있는지(불러오기 전에는 null)로 안내를 띄웁니다. SSO 탭을 열지 않아도 알 수 있게 여기서 불러오고,
  // SSO 카드가 저장하면 그 값으로 바뀝니다. 토큰 목록은 토큰 카드가, 주소록의 태그는 주소록 카드가 채웁니다.
  // 정책 카드가 저장하면 태그의 사용처가 달라지므로 ipRefresh를 올려 주소록 카드가 다시 불러오게 합니다.
  let accessEnabled = $state<boolean | null>(null);
  let accessTokens = $state<AccessToken[] | null>(null);
  let ipTags = $state<IpTagUse[] | null>(null);
  let ipRefresh = $state(0);

  onMount(() => {
    api<AccessProviderView>('/api/v1/security/access-provider').then((view) => { accessEnabled ??= view.enabled; }).catch(() => {});
  });
</script>

<PageHead title={t().title} />

<Tabs.Root bind:value={() => tab, (next) => { setParams({ tab: next }, { tab: 'policies' }, { push: true }); }} class="min-w-0 gap-4">
  <Tabs.List aria-label={t().tabsAria}>
    <Tabs.Trigger value="policies"><Shield12 class="size-3" aria-hidden="true" />{t().tabPolicies}</Tabs.Trigger>
    <Tabs.Trigger value="ips"><Globe12 class="size-3" aria-hidden="true" />{t().tabIps}</Tabs.Trigger>
    <Tabs.Trigger value="sso"><PersonCircle12 class="size-3" aria-hidden="true" />{t().tabSso}</Tabs.Trigger>
  </Tabs.List>
  <Tabs.Content value="policies" class="grid min-w-0 gap-4">
    <AccessPolicySettings providerEnabled={accessEnabled} tokens={accessTokens} ipTags={ipTags} onsaved={() => { ipRefresh += 1; }} />
    <AccessTokenSettings bind:tokens={accessTokens} />
  </Tabs.Content>
  <Tabs.Content value="ips" class="grid min-w-0 gap-4">
    <IpAddressSettings bind:tags={ipTags} refresh={ipRefresh} />
  </Tabs.Content>
  <Tabs.Content value="sso" class="grid min-w-0 gap-4">
    <AccessProviderSettings bind:enabled={accessEnabled} />
  </Tabs.Content>
</Tabs.Root>
