<script lang="ts">
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';
  import { outletState } from '../outletState.svelte';

  // 공개 주소 하나를 고르는 선택 상자. 등록된 공개 주소와, 그 상위 이름의 와일드카드(*.example.com)를 보여 줍니다.
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
  // 주소마다 위쪽 이름의 와일드카드를 둡니다. 등록 도메인 아래 주소는 그 도메인까지 모든 단계를(app.api.example.com → *.api.example.com, *.example.com),
  // 그 밖의 주소는 이름이 세 조각 이상일 때 바로 위 한 단계만 둡니다(*.com, *.co.kr 같은 것은 만들지 않습니다).
  function wildcardsOf(host: string): string[] {
    const parents: string[] = [];
    for (let rest = host; rest.includes('.'); ) {
      rest = rest.slice(rest.indexOf('.') + 1);
      parents.push(rest);
    }
    const domain = outletState.domains.find((candidate) => host.endsWith(`.${candidate}`));
    const kept = domain ? parents.slice(0, parents.indexOf(domain) + 1) : host.split('.').length > 2 ? parents.slice(0, 1) : [];
    return kept.map((parent) => `*.${parent}`);
  }
  const wildcards = $derived([...new Set(hosts.flatMap(wildcardsOf))].sort());
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
