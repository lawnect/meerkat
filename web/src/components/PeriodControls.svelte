<script lang="ts">
  import { getLocalTimeZone, today } from '@internationalized/date';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
  import { Calendar } from '$lib/components/ui/calendar/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as Popover from '$lib/components/ui/popover/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { intlLocale, translations } from '../i18n.svelte';
  import { CalendarLtr12, ChevronLeft, ChevronRight } from '../icons';
  import { periodUnits, type Period } from '../period.svelte';
  import type { UsageUnit } from '../types';
  import IconButton from './IconButton.svelte';

  // 기간 탐색 줄: 자동 새로고침 스위치, 단위 선택, 이전·다음 버튼, 날짜를 고르는 달력.
  // Card.Header 안에 Card.Title 다음으로 놓으며, 좁은 화면에서 제목 아래로 내려가도록 Card.Header에
  // class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3"를 줍니다. 상태는 createPeriod()가 만든 period가 들고 있습니다.
  let { period }: { period: Period } = $props();

  const t = translations({
    units: { hour: 'Hourly', day: 'Daily', month: 'Monthly', year: 'Yearly' } as Record<UsageUnit, string>,
    unitsAria: 'Chart interval',
    previous: 'Previous period',
    next: 'Next period',
    pickDate: 'Pick a date',
    autoRefresh: 'Auto-refresh every 5 seconds',
  }, {
    units: { hour: '시간별', day: '일별', month: '월별', year: '연별' },
    unitsAria: '그래프 단위',
    previous: '이전 기간',
    next: '다음 기간',
    pickDate: '날짜 선택',
    autoRefresh: '5초마다 자동 새로 고침',
  });

  let calendarOpen = $state(false);

  function pick(value: { year: number; month: number; day: number } | undefined): void {
    if (!value) return;
    period.pickDate(value);
    calendarOpen = false;
  }
</script>

<Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
  <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
    <Tooltip.Root>
      <Tooltip.Trigger>
        {#snippet child({ props })}
          <Switch {...props} size="sm" aria-label={t().autoRefresh} bind:checked={() => period.live, period.setLive} />
        {/snippet}
      </Tooltip.Trigger>
      <Tooltip.Content>{t().autoRefresh}</Tooltip.Content>
    </Tooltip.Root>
    <ButtonGroup.Root>
      <ButtonGroup.Root>
        <NativeSelect.Root size="sm" aria-label={t().unitsAria} bind:value={() => period.unit, (next) => { if (next) period.setUnit(next as UsageUnit); }}>
          {#each periodUnits as option (option)}
            <NativeSelect.Option value={option}>{t().units[option]}</NativeSelect.Option>
          {/each}
        </NativeSelect.Root>
      </ButtonGroup.Root>
      <ButtonGroup.Root>
        <IconButton variant="outline" label={t().previous} icon={ChevronLeft} onclick={() => period.move(-1)} />
        <Popover.Root bind:open={calendarOpen}>
          <Popover.Trigger>
            {#snippet child({ props })}
              <Button {...props} variant="outline" size="sm" class="tabular-nums" aria-label={`${t().pickDate}: ${period.label}`}>
                <CalendarLtr12 aria-hidden="true" /><span aria-live="polite">{period.label}</span>
              </Button>
            {/snippet}
          </Popover.Trigger>
          <Popover.Content class="w-auto p-0" align="end">
            <Calendar type="single" preventDeselect value={period.selectedDate} onValueChange={pick} maxValue={today(getLocalTimeZone())} locale={intlLocale()} captionLayout="dropdown" />
          </Popover.Content>
        </Popover.Root>
        <IconButton variant="outline" label={t().next} icon={ChevronRight} side="left" disabled={period.isCurrent} onclick={() => period.move(1)} />
      </ButtonGroup.Root>
    </ButtonGroup.Root>
  </div>
</Card.Action>
