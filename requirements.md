## unified config.yaml UX for notifications/alerts/watch — Requirements

### 목표
- 설정 진입점을 `config.yaml` 하나로 통합하되, 초보 사용자도 바로 읽고 수정할 수 있게 단순화한다.
- `watch.notify_on` 같은 쉬운 사용자용 필드를 제공하고, 내부 bool 조합으로 자연스럽게 변환한다.
- `alerts.yaml`과 `watch/config.json`은 레거시 fallback으로만 유지한다.

### 완료 기준 (DoD)
- [ ] `config.yaml`에서 `notify`, `watch`, `alerts.rules`를 모두 설정 가능
- [ ] `watch.notify_on` 지원 (`off|incident|flapping|all`)
- [ ] `watch.enabled: true` + `notify_on`만으로 기본 사용 가능
- [ ] 고급 `watch.flapping` 설정은 선택사항으로 유지
- [ ] `alerts.yaml`은 fallback 로드 + deprecation warning 출력
- [ ] `watch/config.json`도 fallback 로드 유지
- [ ] `alerts init`는 사용자 친화적 `config.yaml` 템플릿 생성
- [ ] README 예시가 새 UX 기준으로 갱신됨
- [ ] `go test ./...` 통과

### 금지 조건
- 사용자에게 dispatcher/provider 내부 개념을 config에 노출하지 않는다.
- provider별 필수 아닌 토글을 늘리지 않는다.
- 기존 alerts rule 동작을 깨지 않는다.

### 사용자 중심 원칙
- 최상위 섹션은 `notify`, `watch`, `alerts` 중심으로 단순하게 유지
- 값이 있으면 활성화, 없으면 비활성화
- 대부분 사용자는 몇 줄만 수정하면 되게 설계
- 고급 설정은 선택사항으로 뒤로 숨김
