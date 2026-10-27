import { translations } from './i18n.svelte';
import type { BlockRule } from './types';

// 차단 규칙 카드와 차단된 클라이언트 카드가 함께 쓰는 규칙 이름입니다.
// 기본 규칙의 이름은 영어로 저장되어 있어, 이름을 바꾸지 않은 기본 규칙만 화면 언어로 보여줍니다.
const storedNames: Record<string, string> = {
  honeypot: 'Honeypot',
  'hidden-files': 'Hidden files',
  scanners: 'Vulnerability scanners',
  'unused-paths': 'Paths this site does not use',
};
const presets = translations(storedNames, {
  honeypot: '허니팟',
  'hidden-files': '숨김 파일',
  scanners: '취약점 스캐너',
  'unused-paths': '미사용 경로',
});

export type RuleName = Pick<BlockRule, 'id' | 'name'>;

export const displayName = (rule: RuleName): string => (storedNames[rule.id] === rule.name ? presets()[rule.id] ?? rule.name : rule.name);

// 차단 규칙 카드가 규칙을 불러오거나 저장할 때마다 이름을 넘겨 두면, 차단된 클라이언트의 규칙 열이 id 대신 이름을 보여줍니다.
// null이면 아직 불러오지 못한 것입니다.
export const knownRules = $state<{ rules: RuleName[] | null }>({ rules: null });

export function shareRules(rules: readonly RuleName[]): void {
  knownRules.rules = rules.map(({ id, name }) => ({ id, name }));
}
