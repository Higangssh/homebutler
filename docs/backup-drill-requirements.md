# Backup Drill — Requirements

## 목표
- `homebutler backup drill` 명령어로 백업 복구를 자동 검증
- "백업 있다" → "복구 된다"를 증명하는 유일한 셀프호스팅 CLI

## 유저 스토리

### 기본 시나리오
```bash
# 특정 앱 백업 검증
homebutler backup drill nginx-proxy-manager

# 모든 앱 한 번에 검증
homebutler backup drill --all

# 가장 최근 백업으로 검증 (기본값)
# 특정 백업 파일 지정도 가능
homebutler backup drill nginx-proxy-manager --archive ./backup_2026-04-04.tar.gz
```

### 출력 예시 (성공)
```
🔍 Backup Drill — nginx-proxy-manager

  📦 Backup: ~/.homebutler/backups/backup_2026-04-04_1630.tar.gz
  📏 Size: 12.3 MB
  🔐 Integrity: ✅ tar valid (847 files)

  🚀 Boot: ✅ container started in 8s
  🌐 Health: ✅ HTTP 200 on port 81
  ⏱️ Total: 23s

  ✅ DRILL PASSED
```

### 출력 예시 (실패)
```
🔍 Backup Drill — vaultwarden

  📦 Backup: ~/.homebutler/backups/backup_2026-04-04_1630.tar.gz
  📏 Size: 45.1 MB
  🔐 Integrity: ✅ tar valid (1,203 files)

  🚀 Boot: ✅ container started in 12s
  🌐 Health: ❌ HTTP 503 on port 80
  📋 Logs: "database disk image is malformed"
  ⏱️ Total: 35s

  ❌ DRILL FAILED
  💡 DB 파일 손상 가능성. homebutler backup 재실행 권장
```

### --all 출력 예시
```
🔍 Backup Drill — all apps

  nginx-proxy-manager  ✅ PASSED  (23s)
  vaultwarden          ✅ PASSED  (18s)
  pi-hole              ❌ FAILED  (HTTP 503)
  uptime-kuma          ✅ PASSED  (15s)

  📊 Result: 3/4 passed
  ❌ Failed: pi-hole — "database locked"
  💡 Run: homebutler backup --service pi-hole
```

### JSON 출력
```bash
homebutler backup drill --all --json
```
→ MCP/에이전트 연동용

## 완료 기준 (DoD)

- [ ] `homebutler backup drill <app>` — 단일 앱 검증 동작
- [ ] `homebutler backup drill --all` — 전체 앱 검증 동작
- [ ] `homebutler backup drill --json` — JSON 출력 지원
- [ ] 격리 환경에서 실행 (원본 컨테이너/네트워크에 영향 0)
- [ ] 드릴 완료 후 임시 리소스 자동 정리
- [ ] 앱별 헬스체크 정의 (최소 5개 앱)
- [ ] 백업 파일 없을 때 친절한 에러 메시지
- [ ] 테스트 5개 이상
- [ ] macOS + Linux arm64 빌드 확인

## 기술 설계

### 드릴 파이프라인 (5단계)

```
1. Locate    → 백업 파일 찾기 (최신 or 지정)
2. Verify    → tar 무결성 + 핵심 파일 존재 확인
3. Isolate   → 임시 네트워크 + 임시 볼륨 생성
4. Boot      → 격리 컨테이너 실행 + 백업 데이터 마운트
5. Prove     → 앱별 헬스체크 실행 → 결과 리포트 → 정리
```

### 앱별 헬스체크 정의

각 앱에 `HealthCheck` 필드 추가:

```go
type HealthCheck struct {
    // HTTP 체크
    Path       string // "/", "/health", "/alive", "/admin"
    ExpectCode int    // 200, 301, 302
    
    // 타임아웃
    BootTimeout   time.Duration // 컨테이너 시작 대기
    HealthTimeout time.Duration // 헬스체크 대기
}
```

앱별 매핑 (MVP):
- nginx-proxy-manager: GET / → 200/301
- vaultwarden: GET /alive → 200
- uptime-kuma: GET / → 200
- pi-hole: GET /admin → 200
- gitea: GET / → 200
- jellyfin: GET /health → 200
- plex: GET /web → 200
- portainer: GET / → 200
- homepage: GET / → 200
- adguard-home: GET / → 200

### 격리 전략

```
원본: nginx-proxy-manager (port 81, network: bridge)
드릴: drill-nginx-xxx (port 랜덤, network: drill-net-xxx)
→ 완전 격리, 원본에 영향 0
→ 드릴 끝나면 컨테이너 + 네트워크 + 볼륨 전부 삭제
```

### 파일 구조

```
internal/backup/
├── backup.go          (기존)
├── restore.go         (기존)
├── drill.go           (신규 — 드릴 파이프라인)
├── drill_test.go      (신규 — 테스트)
└── health.go          (신규 — 앱별 헬스체크 정의)

cmd/
└── backup.go          (기존 — drill 서브커맨드 추가)
```

## 금지 조건

- 원본 컨테이너/네트워크/볼륨 절대 건드리지 않음
- 드릴 실패해도 원본에 영향 없음 (panic recovery 포함)
- 외부 서비스 의존 없음 (Docker만 있으면 동작)
- return nil 스텁 금지

## 작업 범위

### 수정 대상
- `cmd/backup.go` — drill 서브커맨드 추가
- `internal/install/install.go` — App 구조체에 HealthCheck 추가

### 신규 파일
- `internal/backup/drill.go` — 드릴 파이프라인 핵심 로직
- `internal/backup/drill_test.go` — 테스트
- `internal/backup/health.go` — 앱별 헬스체크 정의

## 개발 순서

### Step 1: 헬스체크 정의 + 구조체
→ App에 HealthCheck 필드 추가
→ 10개 앱 헬스체크 매핑

### Step 2: 드릴 파이프라인 (Locate → Verify)
→ 백업 파일 찾기 + tar 무결성 검증
→ 매니페스트에서 서비스 정보 읽기

### Step 3: 격리 환경 (Isolate → Boot)
→ 임시 네트워크/볼륨 생성
→ 백업 데이터 복원
→ 격리 컨테이너 실행

### Step 4: 검증 + 정리 (Prove)
→ 헬스체크 실행
→ 리포트 출력
→ 임시 리소스 정리 (defer)

### Step 5: CLI 연결 + --all + --json
→ cmd/backup.go에 drill 서브커맨드
→ --all 플래그
→ --json 출력

### Step 6: 테스트 + 실제 동작 확인
→ 유닛 테스트
→ macOS에서 실제 앱 설치 → 백업 → 드릴 e2e 테스트
