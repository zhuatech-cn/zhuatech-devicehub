# API

- `GET /api/dashboard`
- `POST /api/devices`
- `PUT /api/devices/{id}/desired`
- `POST /api/devices/{id}/heartbeat`
- `POST /api/devices/{id}/telemetry`
- `POST /api/devices/{id}/commands`
- `POST /api/commands/{id}/complete`
- `POST /api/rollouts`
- `POST /api/rollouts/{id}/next`

所有接口使用 JSON 与 `x-api-key`。设备密钥只在注册成功时返回一次。
