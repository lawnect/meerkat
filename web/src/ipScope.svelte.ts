import { translations } from './i18n.svelte';
import type { IpTagUse } from './types';

// 접근 정책의 접속 IP와 관리 콘솔 접근 제한은 IP 주소록의 태그와 IP·CIDR을 한 입력칸(TagInput)에 함께 받습니다.
// 서버는 태그가 IP·CIDR로 읽히지 않게 막으므로, IP·CIDR 모양인 값은 대역으로, 나머지는 태그로 나눕니다.
// 서버의 addressLike(internal/control/ipaddresses.go)와 같은 모양입니다. 틀린 대역(예: 1.2.3.4/)도 IP로 보아 서버가 형식 오류로 알립니다.
const ipv4 = /^(\d{1,3}\.){3}\d{1,3}(\/\d*)?$/;
const ipv6 = /^[0-9a-f:.]*:[0-9a-f:.]*(\/\d*)?$/i;

export const isIpRange = (value: string): boolean => ipv4.test(value.trim()) || ipv6.test(value.trim());

export function splitScope(values: string[]): { tags: string[]; ranges: string[] } {
  return { tags: values.filter((value) => !isIpRange(value)), ranges: values.filter(isIpRange) };
}

export const joinScope = (tags: string[], ranges: string[]): string[] => [...tags, ...ranges];

const t = translations({ count: (n: number) => `${n} IP${n === 1 ? '' : 's'}` }, { count: (n: number) => `IP ${n}개` });

// TagInput의 제안: 주소록의 태그와 그 태그가 붙은 IP 수.
export const tagSuggestions = (tags: IpTagUse[] | null): { value: string; hint: string }[] =>
  (tags ?? []).map((tag) => ({ value: tag.name, hint: t().count(tag.count) }));

// 주소록에 없는 태그(대소문자 무시). 저장 전에 알려 줍니다.
export const unknownTags = (tags: string[], known: IpTagUse[] | null): string[] =>
  tags.filter((tag) => !(known ?? []).some((item) => item.name.toLowerCase() === tag.toLowerCase()));
