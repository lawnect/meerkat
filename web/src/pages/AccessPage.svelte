<script lang="ts">
  import AccessPolicySettings from '../components/AccessPolicySettings.svelte';
  import AccessProviderSettings from '../components/AccessProviderSettings.svelte';
  import AccessTokenSettings from '../components/AccessTokenSettings.svelte';
  import PageHead from '../components/PageHead.svelte';
  import { translations } from '../i18n.svelte';
  import type { AccessToken } from '../types';

  // 접근 보호 화면. 자주 고치는 정책을 위에, 정책이 쓰는 접근 토큰과 한 번 설정하는 접근 보호 SSO를 아래에 둡니다.
  // 정책 카드는 SSO가 켜져 있는지와 토큰 목록을 다른 두 카드에서 받습니다(불러오기 전에는 null).
  const t = translations({ title: 'Access protection' }, { title: '접근 보호' });

  let accessEnabled = $state<boolean | null>(null);
  let accessTokens = $state<AccessToken[] | null>(null);
</script>

<PageHead title={t().title} />

<div class="grid min-w-0 gap-4">
  <AccessPolicySettings providerEnabled={accessEnabled} tokens={accessTokens} />
  <AccessTokenSettings bind:tokens={accessTokens} />
  <AccessProviderSettings bind:enabled={accessEnabled} />
</div>
