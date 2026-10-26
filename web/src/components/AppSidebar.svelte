<script lang="ts">
  import * as Sidebar from '$lib/components/ui/sidebar/index.js';
  import { useSidebar } from '$lib/components/ui/sidebar/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { translations } from '../i18n.svelte';
  import { ArrowExit, BranchRequestClosed, BranchRequestClosedFilled, Globe, GlobeFilled, LayoutCellFour, LayoutCellFourFilled, PlugConnected, PlugConnectedFilled, Settings, SettingsFilled, type IconComponent } from '../icons';
  import { pageHref } from '../navigation';
  import type { PageKey } from '../types';
  import BrandMark from './BrandMark.svelte';
  import IconButton from './IconButton.svelte';
  import LocaleSwitch from './LocaleSwitch.svelte';
  import Status from './Status.svelte';
  import ThemeToggle from './ThemeToggle.svelte';

  let { view, domain, version, onNavigate, onLogout }: { view: PageKey; domain?: string; version?: string; onNavigate: (page: PageKey) => void; onLogout: () => void } = $props();

  const t = translations({
    healthy: 'Healthy',
    noDomain: 'No domain set',
    menuLabel: 'Operations menu',
    logout: 'Log out',
    overview: 'Overview',
    connectors: 'Tunnels',
    domains: 'Domains',
    diagnostics: 'Path diagnostics',
    settings: 'Settings',
  }, {
    healthy: '정상',
    noDomain: '도메인 미설정',
    menuLabel: '운영 메뉴',
    logout: '로그아웃',
    overview: '개요',
    connectors: '터널 관리',
    domains: '도메인 관리',
    diagnostics: '경로 진단',
    settings: '설정',
  });

  // 메뉴는 기본 상태에 regular, 선택된 상태에 같은 크기의 filled 아이콘을 씁니다.
  const items: { page: PageKey; icon: IconComponent; activeIcon: IconComponent }[] = [
    { page: 'overview', icon: LayoutCellFour, activeIcon: LayoutCellFourFilled },
    { page: 'connectors', icon: PlugConnected, activeIcon: PlugConnectedFilled },
    { page: 'domains', icon: Globe, activeIcon: GlobeFilled },
    { page: 'diagnostics', icon: BranchRequestClosed, activeIcon: BranchRequestClosedFilled },
    { page: 'settings', icon: Settings, activeIcon: SettingsFilled },
  ];

  const sidebar = useSidebar();

  function follow(event: MouseEvent, page: PageKey): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    sidebar.setOpenMobile(false);
    onNavigate(page);
  }
</script>

<Sidebar.Root collapsible="offcanvas">
  <Sidebar.Header class="border-b border-dashed">
    <!-- sidebar-07 블록처럼 브랜드는 큰 메뉴 버튼이며, 누르면 개요로 갑니다. -->
    <Sidebar.Menu>
      <Sidebar.MenuItem>
        <Sidebar.MenuButton size="lg" class="h-auto gap-3 py-2">
          {#snippet child({ props })}
            <a {...props} href={pageHref('overview')} onclick={(event) => follow(event, 'overview')}>
              <BrandMark class="size-9 shrink-0" />
              <span class="grid min-w-0 gap-1">
                <span class="truncate text-sm leading-4 font-semibold">Meerkat <span class="font-medium text-muted-foreground">Outlet</span>{#if version}<span class="ms-1.5 align-top font-mono text-xs leading-4 font-normal text-muted-foreground opacity-0 group-hover/menu-button:opacity-100 group-focus-visible/menu-button:opacity-100">v{version}</span>{/if}</span>
                <span class="flex items-center">
                  <Tooltip.Root>
                    <Tooltip.Trigger>
                      {#snippet child({ props: tipProps })}<span {...tipProps}><Status label={t().healthy} value="connected" /></span>{/snippet}
                    </Tooltip.Trigger>
                    <Tooltip.Content side="bottom">{domain || t().noDomain}</Tooltip.Content>
                  </Tooltip.Root>
                </span>
              </span>
            </a>
          {/snippet}
        </Sidebar.MenuButton>
      </Sidebar.MenuItem>
    </Sidebar.Menu>
  </Sidebar.Header>
  <Sidebar.Content>
    <Sidebar.Group>
      <Sidebar.GroupContent>
        <Sidebar.Menu aria-label={t().menuLabel}>
          {#each items as item (item.page)}
            <Sidebar.MenuItem>
              <Sidebar.MenuButton isActive={view === item.page}>
                {#snippet child({ props })}
                  <a href={pageHref(item.page)} {...props} aria-current={view === item.page ? 'page' : undefined} onclick={(event) => follow(event, item.page)}>
                    {#if view === item.page}<item.activeIcon aria-hidden="true" />{:else}<item.icon aria-hidden="true" />{/if}
                    <span>{t()[item.page]}</span>
                  </a>
                {/snippet}
              </Sidebar.MenuButton>
            </Sidebar.MenuItem>
          {/each}
        </Sidebar.Menu>
      </Sidebar.GroupContent>
    </Sidebar.Group>
  </Sidebar.Content>
  <Sidebar.Footer class="hidden flex-row items-center gap-1 border-t md:flex border-dashed">
    <LocaleSwitch side="top" align="start" />
    <ThemeToggle side="top" align="start" />
    <span class="flex-1"></span>
    <IconButton label={t().logout} icon={ArrowExit} side="top" align="end" onclick={onLogout} />
  </Sidebar.Footer>
</Sidebar.Root>
