# Go ハブ動作確認テストチェックリスト

## 事前準備

- [ ] Go 1.24+ がセットアップされている
- [ ] `cmd/hub` バイナリをビルドするか `go run ./cmd/hub` を利用できる
- [ ] テスト端末として Game 用 PC と Controller 用スマホ/ブラウザを同一 LAN に接続する
- [ ] 必要に応じて `ADDR` / `ORIGINS` / `MAX_CLIENTS` / `RATE_HZ` を設定し、
      想定する運用値に合わせる
- [ ] CLI でテストするために [vi/websocat](https://github.com/vi/websocat) を用いた

## HTTP / 基本動作

- [ ] Hub を起動すると指定または既定の `:8765` で Listen する
- [ ] `GET http://<addr>/healthz` に対して 200 OK と `{"ok":true}` が返る

  ```bash
  curl -i http://localhost:8765/healthz
  ```

  ```
  HTTP/1.1 200 OK
  Content-Type: application/json
  Date: Tue, 28 Oct 2025 18:34:36 GMT
  Content-Length: 11

  {"ok":true}
  ```

- [ ] `GET http://<addr>/` で埋め込み静的ファイルが配信される

  ```bash
  curl -i http://localhost:8765/
  ```

  ```
  HTTP/1.1 200 OK
  Accept-Ranges: bytes
  Content-Length: 8101
  Content-Type: text/html; charset=utf-8
  Date: Tue, 28 Oct 2025 18:35:14 GMT

  <!DOCTYPE html>
  <html lang="ja">
  （以下省略）
  ```

- [ ] HTTP アクセス時にサーバー側に JSON ログが出力され、
      `remote_ip` / `status` / `duration_ms` が含まれる
  ```
  {"time":"2025-10-29T03:36:13.368083782+09:00","level":"INFO","msg":"http_request","method":"GET","path":"/","status":200,"duration_ms":0,"remote_ip":"::1"}
  ```

## `vi/websocat` を使う注意事項

- https://github.com/vi/websocat
- 対話型のツールであるため、次を実行して接続する
  ```
  websocat -vvE ws://localhost:8765/ws
  ```
  - `-v` でログの詳細を表示できる
  - `-vv` で更に詳しいログになり、これを使うと close code と reason text が見れる
  - `-E` で close message を受け取ると終了できる ( `--exit-on-eof` )
- 以降の例で、実行例から接続（例: `websocat -vE ws://localhost:8765/ws`）は省略している
- 以降の例で、サーバ側のログ例から disconnected などは省略している

## WebSocket 登録シーケンス

- [ ] Game クライアントが `/ws` に接続し、最初の Text フレームで `{"role":"game"}`
      を送ると 101 Switching Protocols が確立し、ログに `role=game` の接続情報が出力される
  ```json
  { "role": "game" }
  ```
  ```
  {"time":"2025-10-29T04:41:33.389013157+09:00","level":"INFO","msg":"connected","component":"hub","role":"game","id":"","remote_ip":"::1"}
  ```
- [ ] Controller クライアントが `/ws` に接続し、
      `{"role":"controller","id":"p1"}` など許可された ID で登録できる
  ```json
  { "role": "controller", "id": "p1" }
  ```
  ```
  {"time":"2025-10-29T04:47:15.892273947+09:00","level":"INFO","msg":"connected","component":"hub","role":"controller","id":"p1","remote_ip":"::1"}
  ```
- [ ] Controller 登録で ID を省略または正規表現 `^[a-z0-9_-]{1,32}$`
      に一致しない値を送ると、ログに `register_invalid_id` が出力される
  - またこの時 Hub が 1008 Policy Violation で切断を送信する
  ```json
  { "role": "controller", "id": "ほげ" }
  ```
  ```
  {"time":"2025-10-29T06:14:39.721379148+09:00","level":"WARN","msg":"register_invalid_id","component":"hub","role":"controller","id":"ほげ","remote_ip":"::1"}
  ```

## Controller 中継動作

- [ ] Controller が送信する JSON の `id` が自身の登録 ID と一致する場合、
      Game 側で同一 payload を受信できる（内容が改変されない）
  ```json
  { "role": "controller", "id": "p2" }
  { "type": "state", "id": "p2" }
  ```
- [ ] `id` フィールドに異なる値を入れて送信すると、Hub が 1008 Policy Violation
      で切断しログに `payload_invalid`・`id mismatch` が出力される
  ```json
  { "role": "controller", "id": "p1" }
  { "type": "state", "id": "p2" }
  ```
- [ ] Game 未接続時に Controller が送信しても Hub
      はエラーを返さず受信し続ける（Game 側には届かない）
- [ ] Controller が Text 以外のフレーム（Binary/Ping/Pong 以外）を送ると
      1003 Unsupported Data で切断される （要検証）
  ```bash
  # これだと `text frame required` で切断される
  printf '\x01\x02' | websocat -b - ws://localhost:8765/ws
  ```
  ```
  {"time":"2025-10-29T06:20:10.123456789+09:00","level":"WARN","msg":"register_invalid_type","component":"hub","role":"","id":"","remote_ip":"::1"}
  ```

## Game セッション管理

- [ ] 同時に 2 本目の Game 接続を行うと、先行セッションが 1008 Policy Violation
      (`"game replaced"`) で切断される
- [ ] Game が切断されると `role=game` のログに `status=1000` などの終了情報が出力され、
      Hub 内部状態からゲームセッションが解除される

## Controller セッション管理

- [ ] 既定 `MAX_CLIENTS=4` の状態で 5 台目の Controller を接続すると、
      新規セッションが `controller limit reached` で拒否される
- [ ] 同じ ID で Controller を再接続すると、新しい接続が受理され、
      旧接続は `controller replaced` で切断される
- [ ] Controller 接続中はログに `role=controller`、`id=<controller id>`、`remote_ip`
      が含まれる

## バックプレッシャーとキュー

- [ ] Game 側の受信処理を意図的に遅らせると、Hub ログに `queue_drop_oldest` または
      `queue_drop_latest` が出力され、古い入力がドロップされる
- [ ] Game 送信キューが詰まり続けると、Hub が `write_failed` ログとともに
      Game セッションを 1011 Internal Error (`"relay failed"`) で閉じる

## 設定パラメータの確認

- [ ] `--addr` フラグまたは `ADDR` 環境変数でリッスンアドレスを変更できる
- [ ] `--origins` フラグまたは `ORIGINS` 環境変数に特定の Origin を設定すると、
      許可リスト外 Origin からの WebSocket アップグレードが拒否される
  ```bash
  websocat -H 'Origin: https://forbidden.example' ws://localhost:8765/ws
  ```
  ```
  {"time":"2025-10-29T06:25:42.987654321+09:00","level":"INFO","msg":"http_request","method":"GET","path":"/ws","status":403,"duration_ms":1,"remote_ip":"::1"}
  ```
- [ ] `--max-clients`（`MAX_CLIENTS`）で Controller 接続上限を変更できる
- [ ] `--rate-hz`（`RATE_HZ`）を変更すると `RelayQueueSize = rateHz * 2` が反映され、
      バックプレッシャー挙動が変化する

## シャットダウンと耐障害性

- [ ] SIGINT/SIGTERM を送ると Hub が `shutdown_signal` をログに記録し、
      Controller/Game 両接続へ 1000 Normal Closure (`"server shutdown"`) を送って終了する
  ```bash
  pkill -2 hub
  ```
  ```
  {"level":"INFO","msg":"shutdown_signal","reason":"context canceled"}
  {"level":"INFO","msg":"shutdown_complete"}
  ```
- [ ] シャットダウン完了時に `shutdown_complete` がログに出力され、プロセスが終了する
- [ ] シャットダウン後に再起動しても `/healthz` と WebSocket 接続が正常に復旧する
