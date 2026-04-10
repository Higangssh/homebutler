## unified notification config and dispatcher — Requirements

### 목표
- provider 설정과 watch 알림 발송 경로를 공통 notify 모듈로 통합한다.
- 기존 alerts.yaml 기반 provider 설정은 계속 읽히도록 유지한다.
- watch는 config.yaml 기반 notify/flapping 설정을 우선 사용하고, 설정이 없으면 기존 watch config.json과 alerts notify 설정으로 동작한다.

### 완료 기준 (DoD)
- [ ] `internal/notify`가 공통 provider 전송 + dispatcher cooldown 로직을 제공한다.
- [ ] `internal/config/config.go`에 공통 `notify` 설정과 `watch.notify`, `watch.flapping` 설정이 추가된다.
- [ ] watch 알림이 더 이상 자체 cooldown map + alerts.NotifyAll에 직접 의존하지 않는다.
- [ ] provider 설정이 없거나 channel 대상이 없으면 no-op 한다.
- [ ] 기존 `alerts.yaml` provider 설정은 하위 호환으로 계속 동작한다.
- [ ] watch/config.json도 하위 호환 입력으로 계속 읽는다.
- [ ] 테스트 통과 (`go test ./...`) 및 gofmt clean.

### 금지 조건
- alerts rule 포맷 전체 마이그레이션을 이번 단계에서 강제하지 않는다.
- 기존 사용자 설정 파일을 자동 삭제/수정하지 않는다.
- 외부 라이브러리 추가 금지.

### 작업 범위
- `internal/notify/*`
- `internal/config/config.go`
- `internal/watch/*`
- 필요 시 `internal/alerts/*` 호환 레이어 정리
- 문서/샘플 설정 최소 보정
