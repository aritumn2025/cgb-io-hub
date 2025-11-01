const slotIds = ["1", "2", "3", "4"];

const elements = {
  status: document.querySelector("[data-status]"),
  output: document.querySelector("[data-output]"),
  gameId: document.querySelector("[data-game-id]"),
  lastUpdated: document.querySelector("[data-last-updated]"),
  fetchLobby: document.querySelector("[data-action='fetch-lobby']"),
  updateLobby: document.querySelector("[data-action='update-lobby']"),
  clearLobby: document.querySelector("[data-action='clear-lobby']"),
  startForm: document.querySelector("[data-start-form]"),
  slotInputs: new Map(),
  slotNames: new Map(),
  slotPersonalities: new Map(),
  slotCheckboxes: new Map(),
};

slotIds.forEach((id) => {
  const input = document.querySelector(`[data-slot-input='${id}']`);
  const nameCell = document.querySelector(`[data-slot-name='${id}']`);
  const personalityCell = document.querySelector(
    `[data-slot-personality='${id}']`
  );
  const checkbox = document.querySelector(`[data-slot-checkbox='${id}']`);

  if (input) {
    elements.slotInputs.set(id, input);
  }
  if (nameCell) {
    elements.slotNames.set(id, nameCell);
  }
  if (personalityCell) {
    elements.slotPersonalities.set(id, personalityCell);
  }
  if (checkbox) {
    elements.slotCheckboxes.set(id, checkbox);
  }
});

let currentGameId = "";

function setStatus(message, variant = "info") {
  if (!elements.status) {
    return;
  }
  elements.status.textContent = message;
  elements.status.classList.remove("is-success", "is-error");
  if (variant === "success") {
    elements.status.classList.add("is-success");
  } else if (variant === "error") {
    elements.status.classList.add("is-error");
  }
}

function showOutput(payload) {
  if (!elements.output) {
    return;
  }
  if (payload == null || (typeof payload === "string" && payload === "")) {
    elements.output.textContent = "--";
    return;
  }
  try {
    const json =
      typeof payload === "string"
        ? JSON.parse(payload)
        : JSON.parse(JSON.stringify(payload));
    elements.output.textContent = JSON.stringify(json, null, 2);
  } catch {
    elements.output.textContent =
      typeof payload === "string" ? payload : JSON.stringify(payload, null, 2);
  }
}

function renderLobby(data) {
  if (!data || typeof data !== "object") {
    return;
  }

  const { gameId, lobby } = data;
  currentGameId = typeof gameId === "string" ? gameId : "";

  if (elements.gameId) {
    elements.gameId.textContent = currentGameId || "-";
  }
  if (elements.lastUpdated) {
    const now = new Date();
    elements.lastUpdated.textContent = now.toLocaleString("ja-JP", {
      hour12: false,
    });
  }

  slotIds.forEach((id) => {
    const input = elements.slotInputs.get(id);
    const nameCell = elements.slotNames.get(id);
    const personalityCell = elements.slotPersonalities.get(id);

    const slotInfo =
      lobby && typeof lobby === "object" ? lobby[id] || null : null;

    if (input) {
      input.value = slotInfo && typeof slotInfo.id === "string" ? slotInfo.id : "";
    }
    if (nameCell) {
      nameCell.textContent =
        slotInfo && typeof slotInfo.name === "string" ? slotInfo.name : "-";
    }
    if (personalityCell) {
      personalityCell.textContent =
        slotInfo && typeof slotInfo.personality === "string"
          ? slotInfo.personality
          : "-";
    }
  });
}

async function sendJSON(url, { method = "GET", body } = {}) {
  const options = {
    method,
    headers: {},
    credentials: "same-origin",
  };
  if (body !== undefined) {
    options.headers["Content-Type"] = "application/json";
    options.body = JSON.stringify(body);
  }
  const response = await fetch(url, options);
  const text = await response.text();
  let parsed = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = text;
    }
  }
  if (!response.ok) {
    const detail =
      (parsed && parsed.error) ||
      (parsed && parsed.message) ||
      response.statusText ||
      "Unknown error";
    const error = new Error(detail);
    error.payload = parsed;
    error.status = response.status;
    throw error;
  }
  return parsed;
}

async function fetchLobby() {
  setStatus("ロビーを取得しています…");
  try {
    const data = await sendJSON("/api/game/lobby");
    renderLobby(data);
    showOutput(data);
    setStatus("ロビーを取得しました。", "success");
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    showOutput(error.payload || { error: detail });
    setStatus(`ロビー取得に失敗しました: ${detail}`, "error");
  }
}

function collectLobbyPayload() {
  const lobby = {
    1: null,
    2: null,
    3: null,
    4: null,
  };

  slotIds.forEach((id) => {
    const input = elements.slotInputs.get(id);
    if (!input) {
      return;
    }
    const value = input.value.trim();
    lobby[id] = value === "" ? null : value;
  });

  const payload = { lobby };
  if (currentGameId) {
    payload.gameId = currentGameId;
  }
  return payload;
}

async function updateLobby() {
  setStatus("ロビーを更新しています…");
  const payload = collectLobbyPayload();
  try {
    const data = await sendJSON("/api/game/lobby", {
      method: "POST",
      body: payload,
    });
    renderLobby(data);
    showOutput(data);
    setStatus("ロビーを更新しました。", "success");
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    showOutput(error.payload || { error: detail });
    setStatus(`ロビー更新に失敗しました: ${detail}`, "error");
  }
}

async function clearLobby() {
  const confirmClear = window.confirm(
    "ロビーを全て空欄 (null) にします。実行してよろしいですか？"
  );
  if (!confirmClear) {
    return;
  }
  setStatus("ロビーを全消去しています…");
  const payload = {
    lobby: {
      1: null,
      2: null,
      3: null,
      4: null,
    },
  };
  if (currentGameId) {
    payload.gameId = currentGameId;
  }
  try {
    const data = await sendJSON("/api/game/lobby", {
      method: "POST",
      body: payload,
    });
    renderLobby(data);
    showOutput(data);
    setStatus("ロビーを全て空にしました。", "success");
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    showOutput(error.payload || { error: detail });
    setStatus(`ロビー全消去に失敗しました: ${detail}`, "error");
  }
}

function selectedSlots() {
  const slots = [];
  elements.slotCheckboxes.forEach((checkbox) => {
    if (checkbox.checked && checkbox.value) {
      slots.push(checkbox.value.trim());
    }
  });
  return slots;
}

async function requestGameStart(event) {
  event.preventDefault();
  const slots = selectedSlots();
  const hasSelection = slots.length > 0;

  setStatus("ゲーム開始リクエストを送信しています…");
  try {
    const payload = hasSelection ? { slots } : undefined;
    const data = await sendJSON("/api/game/start", {
      method: "POST",
      body: payload,
    });
    showOutput(data);
    let statusMessage = "ゲーム開始リクエストを送信しました。";
    let statusVariant = "success";
    if (data && typeof data === "object") {
      const forced = Boolean(data.forced);
      const notified = Boolean(data.notified);
      const connected =
        typeof data.connected === "number" ? data.connected : null;
      const required =
        typeof data.required === "number" ? data.required : null;
      const count = typeof data.count === "number" ? data.count : null;
      if (forced) {
        if (notified) {
          if (connected != null && required != null) {
            statusMessage = `人数不足 (${connected}/${required}) ですがゲーム開始を強制しました。`;
          } else {
            statusMessage = "人数不足でしたがゲーム開始を強制しました。";
          }
        } else {
          statusMessage =
            "人数不足ですがゲーム開始信号を送信できませんでした（ゲーム画面未接続の可能性があります）。";
          statusVariant = "error";
        }
      } else if (count === 0) {
        statusMessage =
          "対象のプレイヤーが見つからなかったため処理を行いませんでした。";
        statusVariant = "error";
      }
    }
    setStatus(statusMessage, statusVariant);
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    showOutput(error.payload || { error: detail });
    setStatus(`ゲーム開始リクエストに失敗しました: ${detail}`, "error");
  }
}

if (elements.fetchLobby) {
  elements.fetchLobby.addEventListener("click", () => {
    fetchLobby();
  });
}

if (elements.updateLobby) {
  elements.updateLobby.addEventListener("click", () => {
    updateLobby();
  });
}

if (elements.clearLobby) {
  elements.clearLobby.addEventListener("click", () => {
    clearLobby();
  });
}

if (elements.startForm) {
  elements.startForm.addEventListener("submit", requestGameStart);
}

window.addEventListener("pageshow", () => {
  fetchLobby();
});
