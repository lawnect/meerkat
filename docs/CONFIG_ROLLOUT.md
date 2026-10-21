# 설정 적용과 롤백

이 문서는 하나의 Connector 터널에 여러 서비스를 연결하는 모델의 설정 변경에 적용한다. 현재 서비스 추가·수정·삭제는 Rathole의 서비스별 설정 갱신을 사용하고, 변경되지 않은 서비스의 공개 경로와 연결 상태를 유지한다. HTTP 공개 주소는 Outlet이 요청마다 조회하므로 추가·삭제 시 Caddy 설정을 다시 읽지 않는다. 아래의 준비·롤백 절차는 후속 구현 계획이다.

## 1. 원칙

1. Outlet 하나에는 설정 적용 작업을 한 번에 하나만 실행한다.
2. DB에 desired state와 revision을 먼저 기록한다.
3. 공개 HTTP 경로는 고정된 Caddy 설정과 Outlet의 동적 조회로 적용한다. HAProxy 설정 변경은 후보 파일로 생성하고 문법 검사 후 적용한다.
4. Connector에는 전체 설정이 아니라 해당 Connector가 허용받은 서비스 설정만 전달한다.
5. 새 경로가 실제로 연결된 것을 확인하기 전에는 외부 Route를 활성화하지 않는다.
6. 마지막 정상 revision과 후보 revision의 digest를 저장한다.
7. 자격 증명·인증서 개인키는 설정 번들에 넣지 않는다.

## 2. 서비스 또는 Route 추가

1. 관리자가 Service나 Route를 생성한다.
2. Outlet이 도메인·포트·Connector 권한·ACL 충돌을 검사한다.
3. Connector에 후보 대상 설정을 전달한다.
4. Connector가 대상 서비스에 연결 가능한지 확인하고 prepared를 보고한다.
5. Outlet이 Connector 장기 연결과 대상 health를 확인한다.
6. HTTP Route는 Outlet 조회 결과를 갱신한다. TCP Route는 HAProxy 후보 설정을 생성·검사한다.
7. 필요한 TCP 프록시만 reload하고 실제 HTTP/TCP probe를 수행한다.
8. 성공하면 revision을 applied로 확정하고 이전 후보를 정리한다.

Connector가 준비되지 않으면 외부 Route를 공개하지 않는다.

## 3. 기존 대상 교체

1. 새 Connector 또는 새 대상 설정을 별도로 준비한다.
2. 새 Connector의 연결과 health를 확인한다.
3. 프록시 후보 설정을 새 Service로 만든다.
4. 프록시를 전환한다.
5. 새 Route의 실제 응답을 확인한다.
6. drain deadline 이후 이전 Connector 스트림을 종료한다.
7. 이전 설정을 제거하고 applied revision을 갱신한다.

## 4. 삭제와 폐기

1. Outlet에서 해당 Route의 새 연결을 먼저 차단한다.
2. HTTP는 hostname Route를 비활성화하고, TCP는 공개 포트 ACL을 차단한다.
3. 연결 종료 또는 drain timeout을 기다린다.
4. Connector에 더 이상 필요하지 않은 전달 규칙을 제거한다.
5. Connector identity와 등록 자격 증명을 폐기한다.
6. 관련 Service와 Route를 disabled 또는 revoked 상태로 표시한다.

부분 실패 시 새 접근은 차단된 상태를 유지하고, 삭제 작업을 성공으로 표시하지 않는다.

## 5. 실패 처리

| 실패 지점 | 상태 | 처리 |
| --- | --- | --- |
| Connector 등록 실패 | failed: enrollment | 토큰을 폐기하고 새 토큰 발급 |
| Connector 설정 적용 실패 | failed: connector_apply | 기존 Route 유지, 후보 설정 정리 |
| 대상 health 실패 | failed: health | Route 공개 금지 |
| 초기 Caddy 설정 적용 실패 | failed: caddy_config | 기존 Caddy 설정 유지 |
| HAProxy reload 실패 | failed: haproxy_reload | 기존 HAProxy worker와 설정 유지 |
| Probe 실패 | rolling_back | 후보 Route 차단 후 마지막 정상 revision 복원 |
| Connector 연결 끊김 | degraded | 해당 Route만 실패 처리, 다른 Route 유지 |

## 6. 재시작 복구

Outlet 시작 시 다음을 비교한다.

- SQLite의 마지막 정상 revision
- Caddy의 활성 설정
- HAProxy의 활성 설정과 worker 상태
- Connector의 reported revision과 heartbeat
- Connector의 실제 stream 상태

값이 불일치하면 새 작업을 받기 전에 마지막 정상 프록시 설정을 재적용한다. Connector는 재연결 후 현재 설정 revision을 다시 받고, 허용되지 않은 후보 대상은 정리한다.

## 7. 상태 전이

Operation은 다음 상태를 사용한다.

~~~
pending
  -> preparing
  -> activating
  -> verifying
  -> applied

preparing / activating / verifying
  -> rolling_back
  -> failed

재시작 후 실제 상태 미확인
  -> recovering
  -> applied 또는 degraded
~~~

같은 command ID의 결과 재보고는 허용한다. 서로 다른 결과는 충돌로 처리한다. 모든 작업에는 request ID와 operation ID를 기록한다.

## 8. 보안 주의사항

- Connector 등록 토큰은 일회성·단기 만료로 만든다.
- Connector별 자격 증명을 분리하고 폐기할 수 있어야 한다.
- Connector가 연결할 수 있는 대상 host와 port를 allowlist로 제한한다.
- 사용자 입력을 shell 명령이나 임의 프록시 설정으로 직접 삽입하지 않는다.
- 로그에 토큰, 개인키, 인증서 원문, DB 비밀번호를 남기지 않는다.
- TCP Route는 기본 비활성화하고 ACL 없이는 활성화하지 않는다.
