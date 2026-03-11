# homebutler backup — Requirements

## 목표
Docker 볼륨 + compose + env를 CLI 한 줄로 백업/복원하는 기능 추가

## 완료 기준 (DoD)
- [ ] `homebutler backup` — 전체 Docker 서비스 백업
- [ ] `homebutler backup --service <name>` — 특정 서비스만 백업
- [ ] `homebutler backup --to <path>` — 목적지 지정
- [ ] `homebutler backup list` — 백업 목록 조회
- [ ] `homebutler restore <archive>` — 백업에서 복원
- [ ] `homebutler restore <archive> --service <name>` — 특정 서비스만 복원
- [ ] --json 출력 지원 (기존 패턴 따라감)
- [ ] 테스트 최소 5개 이상
- [ ] 동작 확인: Mac Mini Docker 환경에서 실제 백업+복원

## CLI 인터페이스
```
homebutler backup                          # 전체 백업
homebutler backup --service jellyfin       # 특정 서비스만
homebutler backup --to /nas/backup/        # 목적지 지정
homebutler backup list                     # 백업 목록
homebutler restore ./backup.tar.gz         # 복원
homebutler restore ./backup.tar.gz --service postgres  # 특정 서비스만 복원
```

## 동작 방식
1. `docker compose ls`로 compose 프로젝트 탐지
2. `docker inspect`로 각 컨테이너의 볼륨(Named + Bind) 식별
3. Named Volume: 임시 alpine 컨테이너로 tar.gz 백업 (Docker 공식 패턴)
4. Bind Mount: 호스트 경로 직접 tar.gz
5. compose 파일 + .env 복사
6. manifest.json 생성 (메타데이터)
7. 전체를 하나의 .tar.gz로 아카이브

## 아카이브 구조
```
backup_2026-03-11_1830/
├── manifest.json
├── compose/
│   ├── docker-compose.yml
│   └── .env
└── volumes/
    ├── volume_name_1.tar.gz
    └── volume_name_2.tar.gz
```

## 기본 백업 경로
- homebutler.yml에 `backup.dir` 설정 있으면 그 경로
- 없으면 `~/.homebutler/backups/`

## 금지 조건
- 컨테이너 자동 pause/unpause 하지 않음 (사용자 책임)
- DB dump (pg_dump 등) 하지 않음
- 실시간 데이터 정합성 보장 안 함 (README에 명시)
- 기존 cmd/root.go 구조 패턴 유지 (switch case 추가)
- 기존 internal/ 패키지 수정 금지 (새 패키지만 추가)

## 작업 범위
- 새로 만들 파일:
  - internal/backup/backup.go
  - internal/backup/restore.go
  - internal/backup/backup_test.go
- 수정할 파일:
  - cmd/root.go (case "backup", case "restore" 추가)
  - internal/config/config.go (backup.dir 설정 추가)
