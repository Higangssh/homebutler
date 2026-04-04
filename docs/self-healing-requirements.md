# Self-Healing MVP — Requirements

## 목표
- `homebutler alerts --watch` 데몬 모드 확장: YAML 기반 룰 + 자동 대응(playbook)
- "서버 죽으면 알려주고 살려놓는" CLI

## 현재 상태
- `homebutler alerts` — CPU/메모리/디스크 임계값 체크 (1회성)
- `homebutler alerts --watch` — 30초 간격 반복 체크 + 터미널 출력
- 없는 것: 컨테이너 상태 체크, webhook/Telegram 알림, 자동 대응, YAML 룰

## 유저 스토리

### 기본 시나리오
```bash
# YAML 룰 파일 생성
homebutler alerts init

# 데몬 모드 (백그라운드)
homebutler alerts --watch --config ~/.homebutler/alerts.yaml

# 룰 목록 확인
homebutler alerts rules

# 알림 히스토리
homebutler alerts history
```

### alerts.yaml 예시
```yaml
rules:
  - name: disk-full
    metric: disk
    threshold: 85
    action: notify
    notify: webhook

  - name: container-down
    metric: container
    watch:
      - nginx-proxy-manager
      - vaultwarden
      - uptime-kuma
    action: restart
    notify: webhook
    cooldown: 5m

  - name: cpu-spike
    metric: cpu
    threshold: 90
    duration: 5m
    action: notify
    notify: webhook

  - name: memory-high
    metric: memory
    threshold: 85
    action: notify
    notify: webhook

webhook:
  url: ""  # 사용자가 Telegram bot URL, Slack webhook 등 설정
```

### 자동 대응 (Playbook)
```yaml
rules:
  - name: disk-full
    metric: disk
    threshold: 85
    action: exec
    exec: "docker system prune -f"
    notify: webhook

  - name: container-down
    metric: container
    watch: [nginx-proxy-manager]
    action: restart  # docker compose up -d
    notify: webhook
    max_retries: 3
    cooldown: 5m
```

### 출력 예시 (--watch)
```
🛡️ Self-Healing active — watching 4 rules

  ⏱️  20:48:30  ✅ All clear (cpu 12%, mem 45%, disk 62%)
  ⏱️  20:49:00  ⚠️  disk-full triggered (disk 86%)
                 → Executing: docker system prune -f
                 → Reclaimed 2.3 GB
                 → ✅ Resolved (disk 71%)
  ⏱️  20:49:30  🔴 container-down: nginx-proxy-manager is stopped
                 → Executing: docker compose up -d
                 → ✅ Container restarted (healthy in 8s)
```

### webhook 알림 페이로드
```json
{
  "rule": "container-down",
  "status": "triggered",
  "details": "nginx-proxy-manager is stopped",
  "action_taken": "restart",
  "action_result": "success",
  "timestamp": "2026-04-04T20:49:30+09:00"
}
```

## 완료 기준 (DoD)

### MVP (오늘)
- [ ] YAML 기반 alerts 룰 파싱
- [ ] `homebutler alerts init` — 템플릿 YAML 생성
- [ ] 컨테이너 상태 체크 (docker ps 기반)
- [ ] 자동 대응: restart (docker compose up -d)
- [ ] 자동 대응: exec (임의 명령 실행)
- [ ] webhook 알림 전송 (HTTP POST)
- [ ] cooldown (같은 룰 반복 발동 억제)
- [ ] `homebutler alerts history` — 최근 알림/대응 타임라인
- [ ] 테스트 5개 이상
- [ ] go build + go test 통과

### 후속 (나중)
- [ ] `homebutler alerts rules` — 활성 룰 목록
- [ ] Telegram/Slack 네이티브 알림
- [ ] incident 묶기 (같은 문제 반복 시 하나로)
- [ ] max_retries 초과 시 에스컬레이션
- [ ] MCP 도구 추가

## 기술 설계

### 파일 구조
```
internal/alerts/
├── alerts.go          (기존 — CPU/메모리/디스크 체크)
├── rules.go           (신규 — YAML 룰 파싱 + 매칭)
├── rules_test.go      (신규 — 테스트)
├── playbook.go        (신규 — 자동 대응 액션)
├── playbook_test.go   (신규 — 테스트)
├── webhook.go         (신규 — webhook 전송)
├── history.go         (신규 — 알림 히스토리)
└── container.go       (신규 — 컨테이너 상태 체크)

cmd/
└── alerts.go          (기존 — init, rules, history 서브커맨드 추가)
```

### 워치 루프 확장
```go
// 기존: CPU/메모리/디스크만 체크
// 확장: YAML 룰 기반 체크 + 컨테이너 + 자동 대응

func watchLoop(rules []Rule, interval time.Duration) {
    for {
        for _, rule := range rules {
            result := evaluate(rule)
            if result.Triggered && !rule.InCooldown() {
                action := executeAction(rule)
                sendWebhook(rule, result, action)
                recordHistory(rule, result, action)
                rule.StartCooldown()
            }
        }
        time.Sleep(interval)
    }
}
```

### 룰 평가
```go
type Rule struct {
    Name      string
    Metric    string   // "cpu", "memory", "disk", "container"
    Threshold float64  // 메트릭용
    Duration  time.Duration
    Watch     []string // 컨테이너 이름 목록
    Action    string   // "notify", "restart", "exec"
    Exec      string   // 실행할 명령
    Notify    string   // "webhook"
    Cooldown  time.Duration
    MaxRetries int
}
```

## 금지 조건
- exec 액션에서 위험 명령 필터링 (rm -rf / 등)
- playbook 실패 시 원본 서비스에 추가 피해 없어야 함
- webhook URL 없으면 알림 스킵 (에러 아님)
- return nil 스텁 금지

## 작업 범위

### 수정 대상
- `cmd/alerts.go` — init, history 서브커맨드 + YAML 로딩
- `internal/alerts/alerts.go` — watch 루프 확장

### 신규 파일
- `internal/alerts/rules.go` — YAML 룰 정의 + 파싱
- `internal/alerts/rules_test.go`
- `internal/alerts/playbook.go` — 액션 실행 (restart, exec, notify)
- `internal/alerts/playbook_test.go`
- `internal/alerts/webhook.go` — HTTP POST 전송
- `internal/alerts/history.go` — 히스토리 저장/조회
- `internal/alerts/container.go` — 컨테이너 상태 체크
