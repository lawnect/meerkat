# UI 기본 컴포넌트

이 폴더의 컴포넌트는 `web/src/app.css`의 공통 스타일을 사용합니다.

화면을 작업하기 전에 아래 목록과 해당 컴포넌트의 props를 확인하세요. 필요한 모양이 비슷하다면 기존 아톰의 옵션을 확장하고, 여러 화면에서 반복될 새 기본 요소라면 이 폴더에 추가한 뒤 목록을 갱신하세요.

| 컴포넌트 | 용도 | 옵션 |
| --- | --- | --- |
| `Button` | 클릭, 제출 등 화면 안의 동작 | `variant`: `primary`, `secondary`, `danger`, `text`, `plain`; `size`: `sm`, `md` |
| `LinkButton` | URL로 이동하는 링크 | `href` 필수; `variant`: `primary`, `secondary`, `text`; `size`: `sm`, `md` |
| `Card` | 정보를 담는 표면 | `as`: `div`, `section`, `article`; `variant`: `solid`, `glass`; `padding`: `none`, `sm`, `md` |

`Button`은 기본적으로 `type="button"`입니다. 폼 제출에는 `type="submit"`을 지정하세요. `LinkButton`은 실제 `<a>` 요소이므로 외부 링크에 새 창을 사용할 때는 `target="_blank" rel="noopener noreferrer"`를 함께 지정하세요.

반투명 카드가 필요한 영역에는 `Card variant="glass"`를 사용합니다. 투명도와 흐림 값은 모든 카드에 같은 스타일이 적용되도록 공통 CSS에서 관리합니다.
