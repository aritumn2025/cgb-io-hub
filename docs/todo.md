• 構成案 (Persona 改修なし)

  - Persona 既存 API GET /api/games/lobby/:game_id（PersonaGo-backend/GetGamesLobby/
    GetGamesLobby.go:18）を利用し、ユーザー ID → スロット番号（1〜4）を取得。Hub 側は slot
    1→p1 のマッピングルールだけあればよい。
  - コントローラー UI では id=p1..p4 選択の代わりに、すでに Persona で発行済みの
    userId（Users.id）またはフロントで渡される短いコードを入力/QR 読み込みさせる。QR を発行す
    る仕組みも Persona 側には既存データで対応可能。
  - Hub に薄いブリッジ機能を追加
      1. POST /api/controller/session（新規）で {"userId":"k764-3yjp"} を受け取る。
      2. Hub は Persona API へ GetGamesLobby を呼び、該当ユーザーがどの slot にいるか確認。
      3. 見つかれば {"slotId":"p2","token":"...","ttl":60} を返す。token は Hub 内で署名した短
         期トークン。
      4. コントローラー UI は token を持って WebSocket を開き、
         {"role":"controller","token":"..."} を送信。
      5. Hub は token を検証し、セッション ID を p2 に固定する（internal/hub/hub.go の
         registerPayload 拡張）。
  - Persona 側は既存のロビー運用（GameLobby テーブル更新）だけで済み、追加開発不要。Hub への
    HTTP 通信は Persona からも行わないため、責務は分離されたまま。

  利点

  - 既存データ（UserId/slot）を使うので Persona への改修依頼が不要。
  - プレイヤーは自分の ID を入力するだけでスロットが自動確定し、p1~p4 の誤選択がなくなる。
  - Hub から Persona API へのアクセスは読み取りのみ。失敗時は従来の手動選択フローへフォール
    バック可能。

  導入手順イメージ

  1. Hub に Persona API 呼び出し用クライアント（例: internal/persona/client.go）を追加。
  2. Hub に新規エンドポイントと token 管理を実装。
  3. controller.js を修正し、ID 入力フォームと API 呼び出しロジックを追加。
  4. フロントスタッフ向けに QR/コードの運用手順を整備（既存 Persona 管理画面でユーザー ID 表示
     する等）。
  5. 段階的にロールアウトし、問題があれば従来 UI に戻せるようフラグ制御。

  次のアクション

  1. Persona の現行運用で利用者が取得できる ID/コードの種類を確認。
  2. Hub から Persona API を叩くための認証（ネットワーク範囲や API キー）の有無を整理。
  3. Hub 側の token 生成＆登録フローの詳細設計→実装着手。
