<script lang="ts">
  import { Button, type ButtonProps } from '$lib/components/ui/button/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import type { IconComponent } from '../icons';

  // 아이콘만 있는 버튼. label은 접근성 이름과 툴팁으로 함께 쓰입니다. 툴팁에 더 긴 설명이 필요하면 tip을 넘깁니다(접근성 이름은 그대로 label).
  let { label, tip, icon: Icon, variant = 'ghost', size = 'icon-sm', side = 'top', align = 'center', ...rest }: Omit<ButtonProps, 'children'> & {
    label: string;
    tip?: string;
    icon: IconComponent;
    side?: 'top' | 'bottom' | 'left' | 'right';
    // 화면 가장자리의 버튼은 툴팁이 잘리지 않게 start·end로 맞춥니다.
    align?: 'start' | 'center' | 'end';
  } = $props();
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <Button {...props} {...rest} {variant} {size} aria-label={label}><Icon aria-hidden="true" /></Button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content {side} {align}>{tip ?? label}</Tooltip.Content>
</Tooltip.Root>
