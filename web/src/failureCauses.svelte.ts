import { translations } from './i18n.svelte';
import type { FailureCause } from './types';

// 실패 요청과 요청 기록이 함께 쓰는 원인의 이름(배지·선택지)과 뜻(배지 툴팁)입니다.
export const causeText = translations({
  causes: {
    app_error: 'App error', unreachable: 'Unreachable', timeout: 'Timeout', no_service: 'No service', rate_limited: 'Rate limited',
    blocked: 'Blocked', trapped: 'Honeypot hit', banned: 'Banned client', too_long: 'Path too long', too_large: 'Body too large', busy: 'Connection limit',
  } as Record<FailureCause, string>,
  meanings: {
    app_error: 'The app answered with a 5xx.',
    unreachable: 'The Outlet couldn\'t reach the service through its tunnel.',
    timeout: 'The service didn\'t answer in time.',
    no_service: 'The address has no live service.',
    rate_limited: 'Refused by a rate limit.',
    blocked: 'Refused by a block rule.',
    trapped: 'Matched a honeypot rule, so the client was banned.',
    banned: 'Refused because the client is banned.',
    too_long: 'The path was over 8,192 bytes.',
    too_large: 'The request body was over the upload limit.',
    busy: 'Refused by a connection limit.',
  } as Record<FailureCause, string>,
}, {
  causes: {
    app_error: '앱 오류', unreachable: '연결 실패', timeout: '응답 시간 초과', no_service: '서비스 없음', rate_limited: '요청 제한',
    blocked: '차단', trapped: '허니팟 감지', banned: '차단된 클라이언트', too_long: '경로 길이 초과', too_large: '본문 크기 초과', busy: '동시 연결 제한',
  },
  meanings: {
    app_error: '앱이 5xx로 응답했습니다.',
    unreachable: 'Outlet이 터널을 통해 서비스에 연결하지 못했습니다.',
    timeout: '서비스가 제한 시간 내에 응답하지 않았습니다.',
    no_service: '이 주소에 연결된 활성 서비스가 없습니다.',
    rate_limited: '요청 제한으로 거부되었습니다.',
    blocked: '차단 규칙으로 거부되었습니다.',
    trapped: '허니팟 규칙에 해당하여 클라이언트를 차단했습니다.',
    banned: '차단된 클라이언트의 요청을 거부했습니다.',
    too_long: '경로가 8,192바이트를 초과했습니다.',
    too_large: '요청 본문이 업로드 제한을 초과했습니다.',
    busy: '연결 제한으로 거부되었습니다.',
  },
});
