## Notifier — Requirements

### 목표
- alerts 알림을 Telegram/Slack/Discord/Generic Webhook으로 직접 전송

### 완료 기준 (DoD)
- [ ] `alerts.yaml`에 notify 섹션으로 provider별 설정 가능
- [ ] Telegram Bot API `sendMessage` 직접 호출
- [ ] Slack incoming webhook 포맷 지원
- [ ] Discord webhook embed 포맷 지원
- [ ] Generic webhook (기존 동작 유지)
- [ ] 여러 provider 동시 설정 가능 (Telegram + Slack 등)
- [ ] `homebutler alerts test-notify` 명령으로 테스트 발송
- [ ] 유닛 테스트 + 빌드 통과
- [ ] gofmt clean

### YAML 설계
```yaml
notify:
  telegram:
    bot_token: "your-bot-token"
    chat_id: "your-chat-id"
  slack:
    webhook_url: "https://hooks.slack.com/services/XXX"
  discord:
    webhook_url: "https://discord.com/api/webhooks/XXX"
  webhook:
    url: "https://custom.server/alert"
```

### 금지 조건
- 기존 webhook 동작 깨지면 안 됨 (하위 호환)
- 외부 라이브러리 추가 금지 (net/http로 충분)
- bot_token 등 민감 정보는 코드에 하드코딩 금지
- 문서 예시는 모두 샘플값 사용 (`your-bot-token`, `your-chat-id`)

### 작업 범위
- `internal/alerts/notify.go` — 새 파일, NotifyAll() + provider별 Send
- `internal/alerts/notify_test.go` — 테스트
- `internal/alerts/rules.go` — AlertsConfig에 Notify 섹션 추가
- `internal/alerts/watcher.go` — SendWebhook → NotifyAll 교체
- `cmd/alerts.go` — test-notify 서브커맨드 추가
