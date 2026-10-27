# UI 구성

화면은 [shadcn-svelte](https://www.shadcn-svelte.com/)(lyra 스타일, 모서리 둥글림 없음 `--radius: 0`)와 Tailwind CSS 4로 만듭니다. 화면 구성은 공식 블록(dashboard-01, login-03, chart-bar-interactive)을 기준으로 합니다.

## 기본 컴포넌트 (`web/src/lib/components/ui/`)

shadcn-svelte CLI가 관리하는 코드입니다. 새 컴포넌트는 저장소 루트에서 `npx shadcn-svelte@latest add <이름>`으로 추가하고, 사용법은 공식 문서의 예제 구조를 그대로 따릅니다. 컴포넌트 안의 아이콘은 추가한 뒤 Tabler 대신 `web/src/icons.ts`의 Fluent 아이콘으로 바꿉니다.

- 동작은 `Button`(`href`가 있으면 링크), 표면은 `Card`, 상태 표시는 `Badge`, 목록은 `Item`·`Table`, 입력은 `Field`·`Input`·`InputGroup`·`Select`, 선택지는 `ToggleGroup`·`Tabs`, 대화상자는 `Dialog`·`AlertDialog`, 안내는 `Alert`, 날짜는 `Popover`+`Calendar`, 그래프는 `Chart`, 알림은 `toast`(svelte-sonner)를 씁니다.
- 앱 코드에 `rounded-*` 클래스를 넣지 않습니다(점·숫자 표시도 사각형).
- 대시보드 카드는 `Card.Root size="sm"`을 씁니다. 고친 값은 바로 적용하지 않고 "되돌리기·저장"이 나타난 뒤 저장할 때 반영합니다.
- 목록은 `data-table/*`로 검색·필터·정렬·페이지를 맞추고, 항목을 누르면 상세 페이지로 갑니다.
- 색은 테마 변수(`bg-background`, `text-muted-foreground`, `border` 등)만 씁니다. 다크 테마는 `html.dark` 클래스로 바뀝니다.

## 앱 공통 컴포넌트 (`web/src/components/`)

| 컴포넌트 | 용도 |
| --- | --- |
| `IconButton` | 아이콘만 있는 버튼. `label`이 접근성 이름과 툴팁으로 함께 쓰입니다. 툴팁에 더 긴 설명이 필요하면 `tip`을 넘깁니다(접근성 이름은 그대로 `label`). `href`를 주면 링크가 됩니다 |
| `CopyText` | 값을 눌러 복사하고 결과를 토스트로 알립니다 |
| `TruncateText` | 한 줄로 자르고, 잘렸을 때만 전체 문구를 툴팁으로 보여줍니다 |
| `InfoTooltip` | 제목·입력 항목 옆 도움말 |
| `ConfirmNameDialog` | 삭제·토큰 교체를 대상 이름 입력으로 확인 (`AlertDialog`) |
| `Status` | 연결 상태 배지 |
| `VersionBadge` | 터널(Connector)이 마지막 하트비트로 알린 버전을 이름 옆에 붙이는 작은 `Badge`(outline, 모노스페이스, 예: v0.5.1). 버전이 없으면(첫 연결 전) 그리지 않습니다. 터널 목록(표 이름 칸, 카드 제목)은 보조 기술용 이름만 두고, 상세 머리글(`PageHead`의 `badge`)은 `tip`으로 "Connector 버전" 툴팁을 답니다 |
| `PageHead` | 페이지 제목과 브레드크럼. `badge`(`Snippet`)를 주면 제목 바로 뒤에 작은 표시를 붙입니다(터널 상세의 `VersionBadge`) |
| `StatCard` | 지표 카드 (dashboard-01의 섹션 카드). `label`, `value` 또는 `children`, `sub`, `hint`+`hintId`, `action` |
| `TrafficChart` | 트래픽 본문. `Card.Header` 다음에 넣으면 지표 선택(`ToggleGroup`)·선 그래프·평균/최대(`Card.Footer`)를 그립니다. `buckets`, `axisLabel`, `bucketTitle`, `averageLabel` |
| `ShareChart` | 비율 도넛 그래프와 범례 (chart-pie-donut-text). 항목이 많으면 상위 4개 다음을 "기타"로 묶습니다 |
| `TagInput` | 값을 칩으로 쌓는 입력 (Enter·쉼표로 추가, 붙여 넣으면 여러 개). SSO 허용 사용자·그룹, 접근 정책·토큰, 클라이언트 차단의 제외 대상, 터널 포트 보호의 허용 범위에 씁니다 |
| `AddressSelect` | 공개 주소 하나를 고르는 `NativeSelect`. 등록된 공개 주소와 상위 도메인 와일드카드를 보여 주고, `taken`(다른 줄이 고른 주소)은 고를 수 없게 합니다. 접근 정책·접근 토큰·차단 규칙의 주소 줄에 씁니다 |
| `PaginationBar` | 목록 위·아래에 두는 줄. 왼쪽은 표시 범위, 오른쪽은 shadcn `Pagination`(문서의 Usage 구성 그대로). `total`, `perPage`, `bind:page`, `placement`. 페이지를 나누는 목록(터널·도메인·감사 로그·접근 기록·실패 요청·요청 기록)은 목록 위(`placement="top"`, 페이지가 둘 이상일 때만 보임)와 아래(범위는 한 페이지여도 보임)에 한 번씩 둡니다 |
| `data-table/*` | Data Table 문서 구성의 공통 부품. 목록은 모두 이 구성입니다(TanStack `createTable` + `DataTable` + `DataTablePagination`). `features.ts`(TanStack 기능 묶음), `DataTable`(표. `class`는 `<table>`에, 열의 `meta.class`는 머리글과 셀에 붙습니다), `SortHeader`(정렬 머리글. 오른쪽으로 맞춘 숫자 열은 `align="end"`), `DataTablePagination`(TanStack 페이지 상태를 `PaginationBar`에 연결, `placement`. 전체 수는 `table.getRowCount()`라서 서버가 나누는 표는 `rowCount` 옵션, 클라이언트 표는 페이지로 나누기 전의 행 수를 씁니다), `DataTableSearch`(검색 입력. 입력이 ~300ms 멈춘 뒤에 열의 필터 값에 반영하고, 값이 밖에서 바뀌면 따라갑니다), `urlTable.svelte.ts`(`urlTableState`: 필터·정렬·페이지를 주소 query에 담는 TanStack 제어 상태. 한 화면에 표가 둘 이상이면 `prefix`로 `page`·`sort`의 이름 앞에 붙이고, 필터의 `param`도 표마다 다르게 정합니다). 서버가 거르는 표(`LogCard`, `AuditLog`, `RequestTop`)는 `manualFiltering`·`manualPagination`과 서버 `total`을 준 `rowCount`, 필터는 보이지 않는 열로 둡니다. 서버가 정렬까지 하는 표(`RequestTop`)는 `manualSorting`이고 정렬 키는 서버의 `sort` query와 같습니다. 정책별 지표 표(`AccessMetrics`)는 클라이언트 표이며 판정 열을 정렬합니다 |
| `TrendChart` | 시간에 따른 선 그래프 (`Chart` + LayerChart `LineChart`). 단위가 다른 지표를 각자의 최댓값 기준으로 겹쳐 그리고, 툴팁에 실제 값을 보여줍니다. 시리즈에 `fractional`을 주면 툴팁 값을 반올림하지 않고 `format`에 넘기고(지연 시간처럼 소수 값), 점의 값이 `null`이면 그 구간에서 선이 끊기고 툴팁에는 —로 보입니다. 애니메이션은 쓰지 않습니다 |
| `PeriodControls` | 기간 탐색 줄: 자동 새로고침 스위치, 단위 선택, 이전·다음 버튼, 날짜 달력. `Card.Header` 안에서 `Card.Title` 다음에 두고(`class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3"`), `web/src/period.svelte.ts`의 `createPeriod()`가 만든 `period`를 넘깁니다. 단위·기준 날짜·자동 새로고침은 주소의 `unit`·`date`·`live`(분별은 시간의 시작 `start`)가 단일 출처이고, 같은 파일의 `trackPeriod()`가 기간이 바뀔 때와 5초마다 다시 불러옵니다. 트래픽 카드(`RequestTrend`)와 관찰의 지표 탭(`AccessMetrics`, `LatencyMetrics`)이 함께 씁니다 |
| `LatencyMetrics` | 관찰의 지표 탭에서 접근 판정 카드(`AccessMetrics`) 뒤에 오는 지연 시간 카드. `/api/v1/observe/latency`(`LatencySeries`)를 `createPeriod()`·`trackPeriod()`로 받고 기간은 접근 판정 카드와 같은 주소(`unit`·`date`·`live`)입니다. 두 카드가 정책별 표를 사이에 두고 떨어져 있어 이 카드에도 `PeriodControls`를 둡니다(어느 쪽에서 바꿔도 둘 다 따라갑니다). 머리글의 `InfoTooltip`이 두 측정(Outlet 처리: 요청을 받아 서비스로 보낼 연결을 확보하기까지, 업스트림 응답: 요청을 본문까지 보낸 뒤 최종 응답 헤더까지로 터널 왕복과 재시도 전에 실패한 서비스에 쓴 시간 포함)과 집계 방식을 설명합니다. 본문은 기간 전체의 Outlet 처리·업스트림 응답 평균과 p95를 `StatCard` 네 장으로 보여 주고(잰 요청이 없으면 —, 값은 `format.ts`의 `formatLatency`), 그 아래 `ToggleGroup`(한 가지만 선택, 기본 Outlet 처리)으로 고른 측정의 구간별 p95·평균 선을 `TrendChart`로 그립니다. 두 측정은 크기가 수백 배 달라 겹치지 않습니다. 잰 요청이 없는 구간은 선이 끊기고, 기간 전체에 아무것도 재지 않았으면 `Empty`를 보여 줍니다. 불러오는 동안은 `Skeleton`입니다 |
| `LogCard` | 요청 기록 카드(관찰의 접근 기록·실패 요청·요청 기록). 머리글에 사용 크기·CSV 내려받기(`CsvDownload`)·새로 고침·기록 설정·기록 스위치를 두고, 본문에 필터 줄(`DataTableSearch` 검색, `NativeSelect` 필터, 값이 있을 때만 보이는 칩(`chip` 필터), 초기화 `IconButton`, 오른쪽 끝에 기록하지 않고 세기만 한 반복 요청 수와 `InfoTooltip`)·`DataTable`·위아래 `DataTablePagination`을 그립니다. 기록은 서버가 거르고 나눈 페이지를 `/api/v1/observe/{kind}-log`로 받습니다. 검색어(`q`)·필터·`page`는 `urlTableState`로 주소 query가 단일 출처이고(페이지 크기 20), 필터가 바뀌면 `page`는 1로 돌아갑니다. 부모는 `kind`(`access`·`failure`·`request`), 시각 다음의 열(`columns`: `columnHelper` + `renderSnippet` 셀), 검색 외의 필터(`filters`: `components/logFilter.ts`의 `LogFilter`. 선택 상자는 `LogSelectFilter`이고 선택지 묶음은 `NativeSelect.OptGroup`. `LogChipFilter`(`chip: true`)는 선택 상자 없이 주소로 넘어온 값(요청 통계의 "기록 보기"가 넘기는 `path`·`source`)만 받아 `Badge` 칩과 해제 `IconButton`으로 보여 주고, 어떤 값이든 받으며 필터 초기화로도 지워집니다), 검색 이름(`searchLabel`)과 생략한 요청 설명(`suppressedTip`)을 채웁니다. CSV 내려받기 주소는 지금의 검색어·필터(페이지 제외)와 `utc_offset`을 `…/{kind}-log/export`에 붙인 것이라 조건에 맞는 모든 기록을 받습니다 |
| `LogSettingsDialog` | 기록 하나의 용량 상한(16–4096 MiB)과 보관 기간(1–365일)을 저장하는 대화상자. `LogCard`의 설정 버튼이 엽니다 |
| `CsvDownload` | 현재 필터에 해당하는 항목 전체를 CSV로 내려받는 아이콘 링크(`IconButton`의 `href`+`download`). 이동이 아닌 내려받기라서 fetch가 아닌 링크이고, 주소는 `format.ts`의 `csvHref(path, params)`로 만들어 `utc_offset`(브라우저 시간대, 분)을 붙입니다. `LogCard` 머리글, `AuditLog` 필터 줄, `RequestTop` 머리글이 씁니다 |
| `CauseBadge` | 요청의 실패·거부 원인 배지(이름은 배지, 뜻은 툴팁). 이름과 뜻은 `web/src/failureCauses.svelte.ts`의 `causeText`이고, 실패 요청과 요청 기록의 결과 열이 함께 씁니다 |
| `RequestLog` | 관찰의 요청 기록 탭. 위의 `ToggleGroup`(`?view=`의 `list`·`endpoints`·`clients`, 기본은 목록)으로 요청 기록(`LogCard` `kind="request"`: 시각·요청·결과·클라이언트 열, 상태 종류(`class`)·주소(`host`) 선택, 경로(`path`)·클라이언트 출처(`source`: IPv4 주소, IPv6 /56) 칩)와 통계(`RequestTop`)를 오갑니다. 보기를 바꾸면 검색어·필터·페이지를 지우고, 통계끼리는 기간과 정렬을 이어 갑니다. 클라이언트 열의 처리 시간에 마우스를 올리면(`Tooltip`) "Outlet 0.42 ms · 업스트림 118 ms"처럼 Outlet 처리(`outlet_us`)와 업스트림 응답(`upstream_us`)을 보여 주고, 전달하지 않아 0인 쪽은 빼며 둘 다 0이면 툴팁이 없습니다. 공개 주소(`addresses`)는 관찰 화면이 넘깁니다 |
| `RequestTop` | 요청 기록 통계 카드. `by`(`endpoint`·`client`)로 같은 구성을 엔드포인트(주소+경로)별과 클라이언트별로 씁니다. 요청 기록이 남아 있는 요청을 서버가 기간(`period`: 1h·24h·7d·30d·all)·주소(`host`, 엔드포인트만)·검색어(`q`)로 거르고 묶어 정렬하고 나눈 페이지를 `/api/v1/observe/request-log/top`에서 받습니다. 표는 `manualSorting`·`manualPagination`이고 정렬(`?sort=requests.desc`, 키는 `requests`·`client_errors`·`server_errors`·`distinct`·`last_at`)은 `urlTableState`의 `sortable`입니다. 줄의 "기록 보기"는 `?view=list`로 돌아가 `host`+`path` 또는 `source` 필터를 넘기고 통계 전용 query를 지웁니다. 화면의 줄 키는 쪽 안의 순서이고, 조건이 바뀌면 이전 요청은 `AbortController`로 취소합니다. 기록이 꺼져 있고 비어 있으면 기록 보기로 이동하는 버튼이 있는 `Empty`를 보여 줍니다 |
| `BannedClients` | 설정 > 보안의 차단된 클라이언트 카드. 클라이언트 차단 규칙(허니팟 등)에 해당해 차단된 클라이언트를 `/api/v1/security/client-bans`에서 서버가 거르고 나눈 페이지로 받아 표(클라이언트·규칙·요청·차단 시각/해제 예정·차단 횟수·해제 버튼)로 보여 줍니다. `LogCard`와 같은 서버 표 구성이고 검색어·페이지는 같은 탭의 다른 표와 겹치지 않게 `bans_q`·`bans_page`(`urlTableState`의 `prefix: 'bans_'`)이며 한 페이지는 10건입니다. 머리글에 새로 고침·차단 설정(`ClientBanSettingsDialog`)·모두 해제(확인 `AlertDialog`)를 두고, 규칙 열은 `web/src/blockRules.svelte.ts`가 차단 규칙 카드에서 받은 이름(기본 규칙은 화면 언어)으로 바꿔 보여 주며 지워진 규칙은 "삭제됨"입니다. 설정 탭의 `tab=security`일 때만 그립니다 |
| `ClientBanSettingsDialog` | 차단 시간(1–10080분)과 제외 대상(`TagInput`, IP·CIDR 50개 이하)을 저장하는 대화상자. 항목마다 `Item.Root` 상자 하나이고, 저장하면 바로 서버에 반영합니다. `BannedClients`의 설정 버튼이 엽니다 |
| `TunnelGuardSettings` | 설정 > 보안의 터널 포트 보호 카드. 방식(`RadioGroup` 한 줄, 선택지마다 호버 툴팁)과 허용 범위(`TagInput`)는 되돌리기·저장으로만 반영하고, 머리글의 연결 수 줄(활성 연결·허용·거부, 거부 내역은 툴팁)과 알려진 커넥터 표(IP·마지막 인증)는 읽기 전용이라 새로 고침 버튼으로 다시 받습니다 |

## 지켜야 할 것

- **CSP:** Outlet은 `style-src 'self'`, `script-src 'self'`를 씁니다. 템플릿에 고정된 `style="…"` 속성, 인라인 `<style>`·`<script>`를 만들면 안 됩니다. Svelte의 동적 `style` 값은 CSSOM으로 설정되어 괜찮습니다. 같은 이유로 `Chart`의 `config`에는 색을 넣지 않고 시리즈에 `var(--primary)`처럼 직접 지정하며, `mode-watcher` 대신 `web/src/theme.svelte.ts`가 다크 테마를 켭니다.
- **아이콘:** Fluent UI System Icons만 `web/src/icons.ts`에서 가져오고, 표시 크기에 맞는 크기 버전(12/16/20/24/…)을 씁니다. 줄여서 그리지 않습니다: text-xs 글자 옆의 작은 아이콘(탭, `InfoTooltip`, 배지, `size="sm"`·`xs` 버튼)은 12px 전용 버전(`Info12`, `Home12`, …)을 `size-3`으로 쓰고, 12px 버전이 없으면 16px 버전을 `size-4`로 씁니다. `size-*` 클래스가 없는 아이콘은 `app.css`가 만든 크기 그대로 그립니다.
- **탭:** `Tabs.Content`는 숨겨진 탭도 그려 두므로, 주소 query(page·필터·기간)를 읽거나 주기적으로 불러오는 컴포넌트는 `{#if tab === '…'}`로 고른 탭에서만 그립니다. 탭을 옮길 때는 이전 탭의 query를 함께 지웁니다(관찰 화면의 `setTab`).
- **문구:** 화면 문구는 `translations(en, ko)`로 정의합니다. 설명은 툴팁으로 빼고, 보이는 글은 이름·값·동작에 필요한 말로 줄입니다.
