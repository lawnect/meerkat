# UI 기본 컴포넌트

이 폴더의 컴포넌트는 `web/src/app.css`의 공통 스타일을 사용합니다.

화면을 작업하기 전에 아래 목록과 해당 컴포넌트의 props를 확인하세요. 필요한 모양이 비슷하다면 기존 아톰의 옵션을 확장하고, 여러 화면에서 반복될 새 기본 요소라면 이 폴더에 추가한 뒤 목록을 갱신하세요.

| 컴포넌트 | 용도 | 옵션 |
| --- | --- | --- |
| `Button` | 클릭, 제출 등 화면 안의 동작 | `variant`: `primary`, `secondary`, `danger`, `text`, `plain`; `size`: `sm`, `md` |
| `LinkButton` | URL로 이동하는 링크 | `href` 필수; `variant`: `primary`, `secondary`, `text`; `size`: `sm`, `md` |
| `Card` | 정보를 담는 표면 | `as`: `div`, `section`, `article`; `variant`: `soft`(기본), `glass`, `solid`; `padding`: `none`, `sm`, `md` |

`Button`은 기본적으로 `type="button"`입니다. 폼 제출에는 `type="submit"`을 지정하세요. `LinkButton`은 실제 `<a>` 요소이므로 외부 링크에 새 창을 사용할 때는 `target="_blank" rel="noopener noreferrer"`를 함께 지정하세요.

기본 `soft` 카드는 은은하게 반투명하며, 배경 위에 떠 있는 카드에는 더 투명한 `glass`를 사용합니다. 두 변형 모두 `backdrop-filter`로 카드 뒤의 내용을 흐리게 보여줍니다. 완전히 불투명한 표면이 필요한 경우에만 `solid`를 지정합니다. 각 변형의 투명도와 흐림 값은 `web/src/app.css`에서 일관되게 관리합니다.

`padding="none"`은 카드 자체의 기본 여백을 지정하지 않습니다. 연결 지도처럼 사용처에서 여백을 정해야 할 때 사용하며, 해당 영역의 CSS에서 `padding`을 지정하세요.

기본 버튼 높이와 카드 여백은 운영 화면의 밀도에 맞춰 컴팩트하게 설정되어 있습니다. 화면별로 큰 여백을 다시 지정하기보다 공통 크기를 우선 사용하세요.
