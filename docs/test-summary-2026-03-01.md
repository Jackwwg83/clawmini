# Pre-collected Test Results (2026-03-01)

## go test -race
- internal/client: PASS (10.3s)
- internal/openclaw: PASS (1.0s)
- internal/protocol: PASS (1.0s)
- internal/server: PASS (82.9s)
- cmd/client: no test files
- cmd/server: 93/94 tests PASS, 1 test hangs: TestDataIntegrity_ForeignKeyCascade_DeleteDevice causes timeout (deadlock/infinite loop)

## go vet
- PASS (no issues)

## Frontend (web/)
- npm run build: PASS (chunk size warning: >500kB)
- npm run lint: 6 errors, 4 warnings
  - 4x react-hooks/set-state-in-effect (DeviceDetailPage, IMConfigPage)
  - 2x react-hooks/exhaustive-deps (UserDetailPage, UserManagementPage)
