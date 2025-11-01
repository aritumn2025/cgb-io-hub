# 開始前

## プレイヤーを４人選ぶ（ロビーに追加）

```bash
curl -X POST https://db.rayfiyo.com/api/games/lobby/shooting \
  -H "Content-Type: application/json" \
  -d '{
    "gameId": "shooting",
    "lobby": {
      "1": "6ji4-xvzu",
      "2": "qdxp-15eg",
      "3": "3w07-f97m",
      "4": "nc8c-7ek5"
    }
  }'
```

## ロビーの状態を取得する

```bash
curl https://db.rayfiyo.com/api/games/lobby/shooting
```

- 未設定の場合
  ```json
  {
    "gameId": "shooting",
    "lobby": {
      "1": null,
      "2": null,
      "3": null,
      "4": null
    }
  }
  ```
- 設定済みの場合
  ```json
  {
    "gameId": "shooting",
    "lobby": {
      "1": {
        "id": "6ji4-xvzu",
        "name": "taro",
        "personality": "3"
      },
      "2": {
        "id": "qdxp-15eg",
        "name": "Jiro",
        "personality": "10"
      },
      "3": {
        "id": "3w07-f97m",
        "name": "Haruka S",
        "personality": "7"
      },
      "4": {
        "id": "nc8c-7ek5",
        "name": "Kenji T",
        "personality": "4"
      }
    }
  }
  ```

## ランキング

```bash
curl https://db.rayfiyo.com/api/games/result/summary/shooting?limit=
```
