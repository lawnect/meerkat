// 요청 기록 카드(LogCard)의 필터 선택 하나. param은 주소 query와 서버 query의 이름이자 표의 숨은 열 id입니다.
export type LogFilterOption = { value: string; label: string };

export type LogFilter = {
  param: string;
  label: string; // 접근성 이름
  class: string; // 너비(sm 이상). 좁은 화면에서는 한 줄을 차지합니다
  options: Array<LogFilterOption | { group: string; options: LogFilterOption[] }>; // 첫 항목(value '')은 필터 없음
  free?: boolean; // 선택지에 없는 값(지워진 정책, 주소에서 온 값)도 받습니다. 아니면 선택지의 값만 받습니다
};
