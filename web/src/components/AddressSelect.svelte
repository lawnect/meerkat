<script lang="ts">
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';

  // 공개 주소 하나를 고르는 선택 상자. 등록된 공개 주소와, 그 상위 도메인의 와일드카드(*.example.com)를 보여 줍니다.
  // taken은 같은 목록의 다른 줄이 이미 고른 주소로, 고를 수 없게 합니다. 지금 값이 목록에 없으면(지워진 주소 등) 그 값도 남겨 둡니다.
  let { value = $bindable(''), addresses, taken = [], id, ariaLabel, class: className }: {
    value?: string;
    addresses: string[];
    taken?: string[];
    id?: string;
    ariaLabel: string;
    class?: string;
  } = $props();

  const t = translations(
    { choose: 'Choose an address', addresses: 'Public addresses', wildcards: 'Wildcards', other: 'Saved' },
    { choose: '주소 선택', addresses: '공개 주소', wildcards: '와일드카드', other: '저장된 값' },
  );

  const hosts = $derived([...new Set(addresses)].sort());
  // 이름이 세 조각 이상인 주소마다 바로 위 도메인의 와일드카드를 둡니다(*.com 같은 것은 만들지 않습니다).
  const wildcards = $derived([...new Set(hosts.filter((host) => host.split('.').length > 2).map((host) => `*.${host.slice(host.indexOf('.') + 1)}`))].sort());
  const unknown = $derived(value && !hosts.includes(value) && !wildcards.includes(value) ? value : '');
</script>

<NativeSelect.Root {id} bind:value aria-label={ariaLabel} class={cn('w-full font-mono', className)} required>
  <NativeSelect.Option value="" disabled>{t().choose}</NativeSelect.Option>
  {#if unknown}
    <NativeSelect.OptGroup label={t().other}>
      <NativeSelect.Option value={unknown}>{unknown}</NativeSelect.Option>
    </NativeSelect.OptGroup>
  {/if}
  {#if hosts.length}
    <NativeSelect.OptGroup label={t().addresses}>
      {#each hosts as host (host)}
        <NativeSelect.Option value={host} disabled={host !== value && taken.includes(host)}>{host}</NativeSelect.Option>
      {/each}
    </NativeSelect.OptGroup>
  {/if}
  {#if wildcards.length}
    <NativeSelect.OptGroup label={t().wildcards}>
      {#each wildcards as host (host)}
        <NativeSelect.Option value={host} disabled={host !== value && taken.includes(host)}>{host}</NativeSelect.Option>
      {/each}
    </NativeSelect.OptGroup>
  {/if}
</NativeSelect.Root>
