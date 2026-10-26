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
| `PaginationBar` | 목록 위·아래에 두는 줄. 왼쪽은 표시 범위, 오른쪽은 shadcn `Pagination`(문서의 Usage 구성 그대로). `total`, `perPage`, `bind:page`, `placement`. 페이지를 나누는 목록(터널·도메인·감사 로그)은 목록 위(`placement="top"`, 페이지가 둘 이상일 때만 보임)와 아래(범위는 한 페이지여도 보임)에 한 번씩 둡니다 |
| `data-table/*` | Data Table 문서 구성의 공통 부품. `features.ts`(TanStack 기능 묶음), `DataTable`(표), `SortHeader`(정렬 머리글), `DataTablePagination`(TanStack 페이지 상태를 `PaginationBar`에 연결, `placement`) |
| `TrendChart` | 시간에 따른 선 그래프 (`Chart` + LayerChart `LineChart`). 단위가 다른 지표를 각자의 최댓값 기준으로 겹쳐 그리고, 툴팁에 실제 값을 보여줍니다. 애니메이션은 쓰지 않습니다 |

## 지켜야 할 것

- **CSP:** Outlet은 `style-src 'self'`, `script-src 'self'`를 씁니다. 템플릿에 고정된 `style="…"` 속성, 인라인 `<style>`·`<script>`를 만들면 안 됩니다. Svelte의 동적 `style` 값은 CSSOM으로 설정되어 괜찮습니다. 같은 이유로 `Chart`의 `config`에는 색을 넣지 않고 시리즈에 `var(--primary)`처럼 직접 지정하며, `mode-watcher` 대신 `web/src/theme.svelte.ts`가 다크 테마를 켭니다.
- **아이콘:** Fluent UI System Icons만 `web/src/icons.ts`에서 가져오고, 표시 크기에 맞는 크기 버전(12/16/20/24/…)을 씁니다. 줄여서 그리지 않습니다: text-xs 글자 옆의 작은 아이콘(탭, `InfoTooltip`, 배지, `size="sm"`·`xs` 버튼)은 12px 전용 버전(`Info12`, `Home12`, …)을 `size-3`으로 쓰고, 12px 버전이 없으면 16px 버전을 `size-4`로 씁니다. `size-*` 클래스가 없는 아이콘은 `app.css`가 만든 크기 그대로 그립니다.
- **문구:** 화면 문구는 `translations(en, ko)`로 정의합니다. 설명은 툴팁으로 빼고, 보이는 글은 이름·값·동작에 필요한 말로 줄입니다.
