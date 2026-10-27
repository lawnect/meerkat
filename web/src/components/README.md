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
| `IconButton` | 아이콘만 있는 버튼. `label`이 접근성 이름과 툴팁으로 함께 쓰입니다 |
| `CopyText` | 값을 눌러 복사하고 결과를 토스트로 알립니다 |
| `TruncateText` | 한 줄로 자르고, 잘렸을 때만 전체 문구를 툴팁으로 보여줍니다 |
| `InfoTooltip` | 제목·입력 항목 옆 도움말 |
| `ConfirmNameDialog` | 삭제·토큰 교체를 대상 이름 입력으로 확인 (`AlertDialog`) |
| `Status` | 연결 상태 배지 |
| `PageHead` | 페이지 제목과 브레드크럼 |
| `StatCard` | 지표 카드 (dashboard-01의 섹션 카드). `label`, `value` 또는 `children`, `sub`, `hint`+`hintId`, `action` |
| `TrafficChart` | 트래픽 본문. `Card.Header` 다음에 넣으면 지표 선택(`ToggleGroup`)·선 그래프·평균/최대(`Card.Footer`)를 그립니다. `buckets`, `axisLabel`, `bucketTitle`, `averageLabel` |
| `ShareChart` | 비율 도넛 그래프와 범례 (chart-pie-donut-text). 항목이 많으면 상위 4개 다음을 "기타"로 묶습니다 |
| `TagInput` | 값을 칩으로 쌓는 입력 (Enter·쉼표로 추가, 붙여 넣으면 여러 개). SSO 허용 사용자·그룹에 씁니다 |
| `AddressSelect` | 공개 주소 하나를 고르는 `NativeSelect`. 등록된 공개 주소와 상위 도메인 와일드카드를 보여 주고, `taken`(다른 줄이 고른 주소)은 고를 수 없게 합니다. 접근 정책·접근 토큰·차단 규칙의 주소 줄에 씁니다 |
| `PaginationBar` | 목록 위·아래에 두는 줄. 왼쪽은 표시 범위, 오른쪽은 shadcn `Pagination`(문서의 Usage 구성 그대로). `total`, `perPage`, `bind:page`, `placement`. 페이지를 나누는 목록(터널·도메인·감사 로그·접근 기록·실패 요청)은 목록 위(`placement="top"`, 페이지가 둘 이상일 때만 보임)와 아래(범위는 한 페이지여도 보임)에 한 번씩 둡니다 |
| `data-table/*` | Data Table 문서 구성의 공통 부품. 목록은 모두 이 구성입니다(TanStack `createTable` + `DataTable` + `DataTablePagination`). `features.ts`(TanStack 기능 묶음), `DataTable`(표. `class`는 `<table>`에, 열의 `meta.class`는 머리글과 셀에 붙습니다), `SortHeader`(정렬 머리글. 오른쪽으로 맞춘 숫자 열은 `align="end"`), `DataTablePagination`(TanStack 페이지 상태를 `PaginationBar`에 연결, `placement`. 전체 수는 `table.getRowCount()`라서 서버가 나누는 표는 `rowCount` 옵션, 클라이언트 표는 페이지로 나누기 전의 행 수를 씁니다), `DataTableSearch`(검색 입력. 입력이 ~300ms 멈춘 뒤에 열의 필터 값에 반영하고, 값이 밖에서 바뀌면 따라갑니다), `urlTable.svelte.ts`(`urlTableState`: 필터·정렬·페이지를 주소 query에 담는 TanStack 제어 상태). 서버가 거르는 표(`LogCard`, `AuditLog`)는 `manualFiltering`·`manualPagination`과 서버 `total`을 준 `rowCount`, 필터는 보이지 않는 열로 둡니다. 정책별 지표 표(`AccessMetrics`)는 클라이언트 표이며 판정 열을 정렬합니다 |
| `TrendChart` | 시간에 따른 선 그래프 (`Chart` + LayerChart `LineChart`). 단위가 다른 지표를 각자의 최댓값 기준으로 겹쳐 그리고, 툴팁에 실제 값을 보여줍니다. 애니메이션은 쓰지 않습니다 |
| `PeriodControls` | 기간 탐색 줄: 자동 새로고침 스위치, 단위 선택, 이전·다음 버튼, 날짜 달력. `Card.Header` 안에서 `Card.Title` 다음에 두고(`class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3"`), `web/src/period.svelte.ts`의 `createPeriod()`가 만든 `period`를 넘깁니다. 단위·기준 날짜·자동 새로고침은 주소의 `unit`·`date`·`live`가 단일 출처이고, 같은 파일의 `trackPeriod()`가 기간이 바뀔 때와 5초마다 다시 불러옵니다. 트래픽 카드(`RequestTrend`)와 관찰의 지표 탭(`AccessMetrics`)이 함께 씁니다 |
| `LogCard` | 요청 기록 카드(관찰의 접근 기록·실패 요청). 머리글에 사용 크기·새로 고침·기록 설정·기록 스위치를 두고, 본문에 필터 줄(`DataTableSearch` 검색, `NativeSelect` 필터, 초기화 `IconButton`, 오른쪽 끝에 기록하지 않고 세기만 한 반복 요청 수와 `InfoTooltip`)·`DataTable`·위아래 `DataTablePagination`을 그립니다. 기록은 서버가 거르고 나눈 페이지를 `/api/v1/observe/{kind}-log`로 받습니다. 검색어(`q`)·필터·`page`는 `urlTableState`로 주소 query가 단일 출처이고(페이지 크기 20), 필터가 바뀌면 `page`는 1로 돌아갑니다. 부모는 `kind`(`access`·`failure`), 시각 다음의 열(`columns`: `columnHelper` + `renderSnippet` 셀), 검색 외의 선택 필터(`filters`: `components/logFilter.ts`의 `LogFilter`. 선택지 묶음은 `NativeSelect.OptGroup`), 검색 이름(`searchLabel`)과 생략한 요청 설명(`suppressedTip`)을 채웁니다 |
| `LogSettingsDialog` | 기록 하나의 용량 상한(16–4096 MiB)과 보관 기간(1–365일)을 저장하는 대화상자. `LogCard`의 설정 버튼이 엽니다 |

## 지켜야 할 것

- **CSP:** Outlet은 `style-src 'self'`, `script-src 'self'`를 씁니다. 템플릿에 고정된 `style="…"` 속성, 인라인 `<style>`·`<script>`를 만들면 안 됩니다. Svelte의 동적 `style` 값은 CSSOM으로 설정되어 괜찮습니다. 같은 이유로 `Chart`의 `config`에는 색을 넣지 않고 시리즈에 `var(--primary)`처럼 직접 지정하며, `mode-watcher` 대신 `web/src/theme.svelte.ts`가 다크 테마를 켭니다.
- **아이콘:** Fluent UI System Icons만 `web/src/icons.ts`에서 가져오고, 표시 크기에 맞는 크기 버전(12/16/20/24/…)을 씁니다. 줄여서 그리지 않습니다: text-xs 글자 옆의 작은 아이콘(탭, `InfoTooltip`, 배지, `size="sm"`·`xs` 버튼)은 12px 전용 버전(`Info12`, `Home12`, …)을 `size-3`으로 쓰고, 12px 버전이 없으면 16px 버전을 `size-4`로 씁니다. `size-*` 클래스가 없는 아이콘은 `app.css`가 만든 크기 그대로 그립니다.
- **탭:** `Tabs.Content`는 숨겨진 탭도 그려 두므로, 주소 query(page·필터·기간)를 읽거나 주기적으로 불러오는 컴포넌트는 `{#if tab === '…'}`로 고른 탭에서만 그립니다. 탭을 옮길 때는 이전 탭의 query를 함께 지웁니다(관찰 화면의 `setTab`).
- **문구:** 화면 문구는 `translations(en, ko)`로 정의합니다. 설명은 툴팁으로 빼고, 보이는 글은 이름·값·동작에 필요한 말로 줄입니다.
