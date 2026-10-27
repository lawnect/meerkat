// 요청 기록 카드(LogCard)의 필터 하나. param은 주소 query와 서버 query의 이름이자 표의 숨은 열 id입니다.
export type LogFilterOption = { value: string; label: string };

// 선택 상자로 고르는 필터.
export type LogSelectFilter = {
  param: string;
  label: string; // 접근성 이름
  class: string; // 너비(sm 이상). 좁은 화면에서는 한 줄을 차지합니다
  options: Array<LogFilterOption | { group: string; options: LogFilterOption[] }>; // 첫 항목(value '')은 필터 없음
  free?: boolean; // 선택지에 없는 값(지워진 정책, 주소에서 온 값)도 받습니다. 아니면 선택지의 값만 받습니다
};

// 선택 상자 없이, 주소로 넘어온 값(다른 화면의 "기록 보기")만 받는 필터. 값이 있을 때 칩으로 보여 주고 칩의 해제 버튼으로 지웁니다.
// 어떤 값이든 받으며(free), 필터 초기화로도 지워집니다.
export type LogChipFilter = {
  param: string;
  label: string; // 칩 앞에 붙는 이름이자 해제 버튼의 접근성 이름
  chip: true;
};

export type LogFilter = LogSelectFilter | LogChipFilter;

export const isChipFilter = (filter: LogFilter): filter is LogChipFilter => 'chip' in filter;
