const THEME_STORAGE_KEY = "stg48:theme";
const INPUT_MODE_STORAGE_KEY = "stg48:input-mode";
const SESSION_STORAGE_KEY = "stg48:controller-session";
const TOKEN_REFRESH_MARGIN_MS = 10000;
const INPUT_MODES = {
  STICK: "stick",
  DPAD: "dpad",
};

document.addEventListener("DOMContentLoaded", () => {
  const statusEl = document.querySelector("[data-status]");
  const lampEl = document.querySelector("[data-lamp]");
  const userDisplayEl = document.querySelector("[data-user-display]");
  const infoMenu = document.getElementById("info-menu");
  const infoToggle = document.getElementById("info-toggle");
  const controller = document.querySelector(".controller");
  const stick = document.getElementById("stick");
  const thumb = document.getElementById("stick-thumb");
  const dpad = document.getElementById("dpad");
  const actionButtons = document.querySelectorAll("[data-btn]");
  const controllerScreen = document.getElementById("controller-screen");
  const sessionSection = document.getElementById("session-form");
  const sessionForm = document.querySelector("[data-session-form]");
  const sessionInput = document.querySelector("[data-session-input]");
  const sessionError = document.querySelector("[data-session-error]");
  const resetButton = document.querySelector("[data-session-reset]");
  const themeToggle = document.querySelector("[data-theme-toggle]");
  const controlToggle = document.querySelector("[data-control-toggle]");

  if (
    !statusEl ||
    !lampEl ||
    !infoMenu ||
    !infoToggle ||
    !controller ||
    !stick ||
    !thumb ||
    !dpad
  ) {
    return;
  }

  initTheme(themeToggle);
  initInfoMenu(infoMenu, infoToggle);

  const status = createStatusManager(statusEl, lampEl);

  let activeSession = readStoredSession();
  if (activeSession && isSessionExpired(activeSession)) {
    activeSession = null;
    clearStoredSession();
  }

  const fallbackControllerId = getControllerIdFromQuery();
  let controllerId = activeSession
    ? activeSession.slotId
    : fallbackControllerId || null;
  let refreshTimer = null;

  const connection = createConnection({
    getSession: () => activeSession,
    getControllerId: () => controllerId,
    updateStatus: status.set,
  });
  const state = createInputState(() => controllerId, connection);

  connection.onOpen(() => state.send(true));

  const stickControls = initStick(stick, thumb, state);
  const dpadControls = initDpad(dpad, state);
  initButtons(actionButtons, state);
  initControlMode({
    toggleButton: controlToggle,
    controller,
    stickElement: stick,
    dpadElement: dpad,
    stickControls,
    dpadControls,
  });

  const updateInfoPanel = () => {
    const displaySource = activeSession || (controllerId ? { userId: controllerId } : null);
    if (userDisplayEl) {
      userDisplayEl.textContent = formatUserDisplay(displaySource);
    }
  };

  const scheduleRefresh = (dueTime) => {
    if (refreshTimer) {
      window.clearTimeout(refreshTimer);
      refreshTimer = null;
    }
    if (!activeSession || !activeSession.expiresAt || !activeSession.userId) {
      return;
    }
    const now = Date.now();
    const targetTime =
      dueTime != null
        ? dueTime
        : activeSession.expiresAt - TOKEN_REFRESH_MARGIN_MS;
    const delay = Math.max(targetTime - now, 1000);
    refreshTimer = window.setTimeout(async () => {
      if (!activeSession || !activeSession.userId) {
        return;
      }
      try {
        const next = await requestControllerSession(activeSession.userId);
        applySession(next, { persist: true, announce: false });
      } catch (error) {
        console.warn("[controller] failed to refresh session token:", error);
        scheduleRefresh(Date.now() + 15000);
      }
    }, delay);
  };

  const applySession = (session, { persist = true, announce = true } = {}) => {
    activeSession = session;
    controllerId = session ? session.slotId : fallbackControllerId || null;
    if (persist) {
      if (session) {
        persistSession(session);
      } else {
        clearStoredSession();
      }
    }
    updateInfoPanel();
    setScreenVisibility({
      controllerScreen,
      sessionForm: sessionSection,
      showSessionForm: !controllerId,
    });
    if (session && announce) {
      status.set("接続準備中…");
    }
    scheduleRefresh();
    connection.connect();
    state.send(true);
  };

  const resetSession = ({ showForm = true } = {}) => {
    if (refreshTimer) {
      window.clearTimeout(refreshTimer);
      refreshTimer = null;
    }
    activeSession = null;
    controllerId = fallbackControllerId || null;
    clearStoredSession();
    updateInfoPanel();
    if (showForm) {
      setScreenVisibility({
        controllerScreen,
        sessionForm: sessionSection,
        showSessionForm: true,
      });
      status.set("未接続");
    }
    connection.disconnect();
  };

  initSessionForm({
    form: sessionForm,
    input: sessionInput,
    errorEl: sessionError,
    onSubmit: async (userId) => {
      const session = await requestControllerSession(userId);
      applySession(session, { persist: true, announce: true });
      if (sessionInput) {
        sessionInput.value = "";
      }
    },
  });

  if (resetButton) {
    resetButton.addEventListener("click", () => {
      resetSession({ showForm: true });
      if (sessionInput) {
        sessionInput.focus();
      }
    });
  }

  updateInfoPanel();
  setScreenVisibility({
    controllerScreen,
    sessionForm: sessionSection,
    showSessionForm: !controllerId,
  });

  if (activeSession) {
    applySession(activeSession, { persist: false, announce: false });
  } else if (controllerId) {
    connection.connect();
  }

  window.setInterval(() => state.send(true), 2500);
});

function initInfoMenu(menu, toggle) {
  const close = () => {
    if (!menu.classList.contains("open")) {
      return;
    }
    menu.classList.remove("open");
    toggle.setAttribute("aria-expanded", "false");
  };

  toggle.addEventListener("click", (event) => {
    event.stopPropagation();
    const isOpen = menu.classList.toggle("open");
    toggle.setAttribute("aria-expanded", String(isOpen));
  });

  document.addEventListener("click", (event) => {
    if (!menu.contains(event.target)) {
      close();
    }
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      const wasOpen = menu.classList.contains("open");
      close();
      if (wasOpen) {
        toggle.focus();
      }
    }
  });
}

function createStatusManager(statusEl, lampEl) {
  const set = (text) => {
    statusEl.textContent = text;
    const normalized = text.trim();
    let color = "#666";
    if (/接続済み/.test(normalized)) {
      color = "#4caf50";
    } else if (/再試行中/.test(normalized) || /接続中/.test(normalized)) {
      color = "#f1c40f";
    } else if (/未接続/.test(normalized)) {
      color = "#e74c3c";
    }
    lampEl.style.background = color;
  };

  return { set };
}

function createConnection({ getSession, getControllerId, updateStatus }) {
  let ws = null;
  let backoff = 800;
  let reconnectTimer = null;
  const openCallbacks = new Set();
  let manualClose = false;

  const connectionURL = () => {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${window.location.host}/ws`;
  };

  const shouldConnect = () => {
    const session = typeof getSession === "function" ? getSession() : null;
    const id = typeof getControllerId === "function" ? getControllerId() : null;
    return Boolean((session && session.token) || id);
  };

  const scheduleReconnect = () => {
    if (reconnectTimer) {
      window.clearTimeout(reconnectTimer);
    }
    if (!shouldConnect()) {
      return;
    }
    const wait = backoff;
    backoff = Math.min(Math.round(backoff * 1.5), 3000);
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, wait);
  };

  const connect = () => {
    if (ws) {
      try {
        ws.close();
      } catch (_) {
        // noop
      }
    }

    if (!shouldConnect()) {
      updateStatus("未接続");
      return;
    }
    updateStatus("接続中…");
    ws = new WebSocket(connectionURL());

    ws.onopen = () => {
      backoff = 800;
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      const session = typeof getSession === "function" ? getSession() : null;
      const controllerId =
        typeof getControllerId === "function" ? getControllerId() : null;
      const payload =
        session && session.token
          ? { role: "controller", token: session.token }
          : controllerId
          ? { role: "controller", id: controllerId }
          : null;

      if (!payload) {
        updateStatus("未接続");
        return;
      }

      updateStatus("接続済み");
      ws.send(JSON.stringify(payload));
      openCallbacks.forEach((callback) => callback());
    };

    ws.onclose = () => {
      if (manualClose) {
        manualClose = false;
        updateStatus("未接続");
        return;
      }
      updateStatus("未接続（再試行中）");
      scheduleReconnect();
    };

    ws.onerror = () => {
      try {
        ws.close();
      } catch (_) {
        // noop
      }
    };
  };

  const send = (serialized) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return false;
    }
    ws.send(serialized);
    return true;
  };

  const disconnect = () => {
    if (reconnectTimer) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (ws) {
      manualClose = true;
      try {
        ws.close();
      } catch (_) {
        // noop
      }
      ws = null;
    }
  };

  const onOpen = (callback) => {
    openCallbacks.add(callback);
  };

  return { connect, send, onOpen, disconnect };
}

function createInputState(getControllerId, connection) {
  const axes = { x: 0, y: 0 };
  const btn = { a: false };
  let lastSent = "";

  const send = (force = false) => {
    const controllerId =
      typeof getControllerId === "function" ? getControllerId() : null;
    if (!controllerId) {
      return;
    }
    const payload = {
      type: "state",
      id: controllerId,
      axes: { ...axes },
      btn: { ...btn },
      t: Date.now(),
    };

    const serialized = JSON.stringify(payload);
    if (!force && serialized === lastSent) {
      return;
    }

    if (connection.send(serialized)) {
      lastSent = serialized;
    }
  };

  return { axes, btn, send };
}

function initStick(stick, thumb, state) {
  if (!stick || !thumb) {
    return { reset() {} };
  }

  let activePointer = null;

  const updateThumb = (x, y) => {
    thumb.style.left = `${(x + 1) * 50}%`;
    thumb.style.top = `${(y + 1) * 50}%`;
  };

  const releasePointer = () => {
    if (activePointer !== null && stick.hasPointerCapture(activePointer)) {
      stick.releasePointerCapture(activePointer);
    }
  };

  const resetStick = () => {
    releasePointer();
    activePointer = null;
    state.axes.x = 0;
    state.axes.y = 0;
    updateThumb(0, 0);
    state.send();
  };

  const updateStick = (event) => {
    const rect = stick.getBoundingClientRect();
    const relX = (event.clientX - rect.left) / rect.width;
    const relY = (event.clientY - rect.top) / rect.height;
    const x = clamp(relX * 2 - 1);
    const y = clamp(relY * 2 - 1);
    state.axes.x = parseFloat(x.toFixed(3));
    state.axes.y = parseFloat((-y).toFixed(3));
    updateThumb(x, y);
    state.send();
  };

  stick.addEventListener("pointerdown", (event) => {
    stick.setPointerCapture(event.pointerId);
    activePointer = event.pointerId;
    updateStick(event);
  });

  stick.addEventListener("pointermove", (event) => {
    if (activePointer !== event.pointerId) {
      return;
    }
    updateStick(event);
  });

  ["pointerup", "pointercancel", "pointerleave"].forEach((type) => {
    stick.addEventListener(type, (event) => {
      if (activePointer === null || activePointer !== event.pointerId) {
        return;
      }
      resetStick();
    });
  });

  return { reset: resetStick };
}

function initDpad(dpad, state) {
  if (!dpad) {
    return { reset() {} };
  }

  const buttons = Array.from(dpad.querySelectorAll("[data-dpad]"));
  const pointerDirections = new Map();
  const pointerTargets = new Map();
  const activeDirections = new Set();

  const updateAxes = () => {
    let x = 0;
    let y = 0;
    if (activeDirections.has("left")) {
      x -= 1;
    }
    if (activeDirections.has("right")) {
      x += 1;
    }
    if (activeDirections.has("up")) {
      y += 1;
    }
    if (activeDirections.has("down")) {
      y -= 1;
    }
    state.axes.x = clamp(x);
    state.axes.y = clamp(y);
    state.send();
  };

  const activate = (direction) => {
    if (!direction) {
      return;
    }
    activeDirections.add(direction);
    updateAxes();
  };

  const release = (direction) => {
    if (!direction) {
      return;
    }
    activeDirections.delete(direction);
    updateAxes();
  };

  buttons.forEach((button) => {
    const direction = button.dataset.dpad;
    if (!direction) {
      return;
    }

    button.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      button.setPointerCapture(event.pointerId);
      pointerDirections.set(event.pointerId, direction);
      pointerTargets.set(event.pointerId, button);
      button.classList.add("active");
      activate(direction);
    });

    const releaseHandler = (event) => {
      const storedDirection = pointerDirections.get(event.pointerId);
      if (storedDirection) {
        pointerDirections.delete(event.pointerId);
        pointerTargets.delete(event.pointerId);
        button.classList.remove("active");
        release(storedDirection);
      }
      if (button.hasPointerCapture(event.pointerId)) {
        button.releasePointerCapture(event.pointerId);
      }
    };

    ["pointerup", "pointercancel", "pointerleave"].forEach((type) => {
      button.addEventListener(type, releaseHandler);
    });
  });

  const reset = () => {
    pointerTargets.forEach((target, pointerId) => {
      if (target && target.hasPointerCapture(pointerId)) {
        target.releasePointerCapture(pointerId);
      }
    });
    pointerTargets.clear();
    pointerDirections.clear();
    activeDirections.clear();
    buttons.forEach((button) => button.classList.remove("active"));
    state.axes.x = 0;
    state.axes.y = 0;
    state.send();
  };

  return { reset };
}

function initButtons(buttons, state) {
  buttons.forEach((button) => {
    const key = button.dataset.btn;
    if (!key) {
      return;
    }

    button.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      button.setPointerCapture(event.pointerId);
      state.btn[key] = true;
      button.classList.add("active");
      state.send();
    });

    const release = (event) => {
      if (button.hasPointerCapture(event.pointerId)) {
        button.releasePointerCapture(event.pointerId);
      }
      if (!state.btn[key]) {
        return;
      }
      state.btn[key] = false;
      button.classList.remove("active");
      state.send();
    };

    ["pointerup", "pointercancel", "pointerleave"].forEach((type) => {
      button.addEventListener(type, release);
    });
  });
}

function initSessionForm({ form, input, errorEl, onSubmit }) {
  if (!form || !input || typeof onSubmit !== "function") {
    return;
  }

  const submitButton = form.querySelector("[data-session-submit]");

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const userId = (input.value || "").trim();
    if (!userId) {
      if (errorEl) {
        errorEl.textContent = "ユーザーIDを入力してください";
      }
      input.focus();
      return;
    }

    if (errorEl) {
      errorEl.textContent = "";
    }
    if (submitButton) {
      submitButton.disabled = true;
    }
    form.classList.add("is-loading");

    try {
      await onSubmit(userId);
      if (errorEl) {
        errorEl.textContent = "";
      }
    } catch (error) {
      const message =
        error && typeof error.message === "string" && error.message.trim()
          ? error.message
          : "セッションの作成に失敗しました";
      if (errorEl) {
        errorEl.textContent = message;
      }
      console.error("[controller] session request failed:", error);
    } finally {
      form.classList.remove("is-loading");
      if (submitButton) {
        submitButton.disabled = false;
      }
    }
  });
}

function setScreenVisibility({
  controllerScreen,
  sessionForm,
  showSessionForm,
}) {
  if (controllerScreen) {
    controllerScreen.classList.toggle("is-hidden", Boolean(showSessionForm));
  }
  if (sessionForm) {
    sessionForm.classList.toggle("is-hidden", !showSessionForm);
  }
}

function getControllerIdFromQuery() {
  const params = new URLSearchParams(window.location.search);
  if (!params.has("id")) {
    return null;
  }
  const id = (params.get("id") || "").toLowerCase();
  if (!isValidPlayerId(id)) {
    return null;
  }
  return id;
}

function clamp(value) {
  return Math.max(-1, Math.min(1, value));
}

async function requestControllerSession(userId) {
  const payload = { userId };
  const response = await fetch("/api/controller/session", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
    cache: "no-store",
  });

  let data = null;
  try {
    data = await response.json();
  } catch (_) {
    // ignore parse errors; handled below
  }

  if (!response.ok) {
    const message =
      data && typeof data.error === "string" && data.error.trim()
        ? data.error.trim()
        : `サーバーエラー (${response.status})`;
    throw new Error(message);
  }

  return normalizeSessionResponse(data, userId);
}

function normalizeSessionResponse(data, fallbackUserId) {
  const slotId =
    typeof data.slotId === "string" ? data.slotId.toLowerCase() : "";
  const token = typeof data.token === "string" ? data.token : "";
  const user = data && typeof data.user === "object" ? data.user : {};
  const rawUserId =
    typeof user.id === "string" && user.id.trim()
      ? user.id.trim()
      : fallbackUserId;
  const userId = rawUserId || "";
  const userName = typeof user.name === "string" ? user.name.trim() : "";
  const personality =
    typeof user.personality === "string" ? user.personality.trim() : "";

  if (!slotId || !isValidPlayerId(slotId) || !token) {
    throw new Error("セッション情報が不完全です");
  }

  const ttlSecondsRaw =
    typeof data.ttl === "number" ? data.ttl : Number.parseFloat(data.ttl);
  const ttlSeconds =
    Number.isFinite(ttlSecondsRaw) && ttlSecondsRaw > 0 ? ttlSecondsRaw : 60;
  const ttlMs = ttlSeconds * 1000;

  let expiresAt = Date.now() + ttlMs;
  if (typeof data.expiresAt === "string") {
    const parsed = Date.parse(data.expiresAt);
    if (!Number.isNaN(parsed)) {
      expiresAt = parsed;
    }
  }

  return {
    slotId,
    token,
    userId,
    userName,
    personality,
    ttlMs,
    expiresAt,
    issuedAt: Date.now(),
    gameId: typeof data.gameId === "string" ? data.gameId : "",
  };
}

function readStoredSession() {
  try {
    const raw = window.sessionStorage.getItem(SESSION_STORAGE_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw);
    if (
      !parsed ||
      typeof parsed.slotId !== "string" ||
      typeof parsed.token !== "string"
    ) {
      return null;
    }
    return parsed;
  } catch (_) {
    return null;
  }
}

function persistSession(session) {
  try {
    const minimal = {
      slotId: session.slotId,
      token: session.token,
      userId: session.userId,
      userName: session.userName,
      personality: session.personality,
      ttlMs: session.ttlMs,
      expiresAt: session.expiresAt,
      issuedAt: session.issuedAt,
      gameId: session.gameId,
    };
    window.sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(minimal));
  } catch (_) {
    // ignore storage write issues
  }
}

function clearStoredSession() {
  try {
    window.sessionStorage.removeItem(SESSION_STORAGE_KEY);
  } catch (_) {
    // ignore storage write issues
  }
}

function isSessionExpired(session) {
  if (!session || !session.expiresAt) {
    return true;
  }
  return session.expiresAt <= Date.now();
}

function formatUserDisplay(session) {
  if (!session) {
    return "ゲスト";
  }
  const rawName =
    session && typeof session.userName === "string" ? session.userName.trim() : "";
  const rawId =
    session && typeof session.userId === "string" ? session.userId.trim() : "";
  if (rawName && rawId) {
    return `${rawName} (${rawId})`;
  }
  if (rawName) {
    return rawName;
  }
  if (rawId) {
    return `ID: ${rawId}`;
  }
  return "ゲスト";
}

function initControlMode({
  toggleButton,
  controller,
  stickElement,
  dpadElement,
  stickControls,
  dpadControls,
}) {
  const applyMode = (nextMode, { persist = false } = {}) => {
    const normalized = normalizeInputMode(nextMode);
    if (persist) {
      persistInputMode(normalized);
    }

    if (controller) {
      controller.dataset.mode = normalized;
    }
    if (stickElement) {
      stickElement.setAttribute(
        "aria-hidden",
        normalized === INPUT_MODES.DPAD ? "true" : "false"
      );
    }
    if (dpadElement) {
      dpadElement.setAttribute(
        "aria-hidden",
        normalized === INPUT_MODES.DPAD ? "false" : "true"
      );
    }

    const isDpad = normalized === INPUT_MODES.DPAD;
    if (toggleButton) {
      const nextLabel = isDpad ? "スティックに切り替え" : "十字キーに切り替え";
      toggleButton.textContent = nextLabel;
      toggleButton.setAttribute("aria-pressed", isDpad ? "true" : "false");
      const ariaLabel = isDpad
        ? "スティック操作に切り替え"
        : "十字キー操作に切り替え";
      toggleButton.setAttribute("aria-label", ariaLabel);
      toggleButton.setAttribute("title", ariaLabel);
    }

    if (isDpad) {
      if (stickControls && typeof stickControls.reset === "function") {
        stickControls.reset();
      }
      if (dpadControls && typeof dpadControls.reset === "function") {
        dpadControls.reset();
      }
    } else {
      if (dpadControls && typeof dpadControls.reset === "function") {
        dpadControls.reset();
      }
      if (stickControls && typeof stickControls.reset === "function") {
        stickControls.reset();
      }
    }

    return normalized;
  };

  let currentMode = applyMode(readStoredInputMode());

  if (toggleButton) {
    toggleButton.addEventListener("click", () => {
      currentMode =
        currentMode === INPUT_MODES.DPAD ? INPUT_MODES.STICK : INPUT_MODES.DPAD;
      applyMode(currentMode, { persist: true });
    });
  }
}

function isValidPlayerId(id) {
  return id === "p1" || id === "p2" || id === "p3" || id === "p4";
}

function readStoredInputMode() {
  try {
    const stored = window.localStorage.getItem(INPUT_MODE_STORAGE_KEY);
    if (stored === INPUT_MODES.STICK || stored === INPUT_MODES.DPAD) {
      return stored;
    }
  } catch (_) {
    // ignore storage access issues
  }
  return INPUT_MODES.DPAD;
}

function persistInputMode(mode) {
  try {
    window.localStorage.setItem(
      INPUT_MODE_STORAGE_KEY,
      normalizeInputMode(mode)
    );
  } catch (_) {
    // ignore storage write issues
  }
}

function normalizeInputMode(mode) {
  return mode === INPUT_MODES.STICK ? INPUT_MODES.STICK : INPUT_MODES.DPAD;
}

function initTheme(toggleButton) {
  let currentTheme = readStoredTheme();
  if (!currentTheme) {
    currentTheme = "light";
  }
  currentTheme = normalizeTheme(currentTheme);
  applyTheme(currentTheme, toggleButton);

  if (!toggleButton) {
    return;
  }

  toggleButton.addEventListener("click", () => {
    currentTheme = currentTheme === "light" ? "dark" : "light";
    applyTheme(currentTheme, toggleButton);
    persistThemePreference(currentTheme);
  });
}

function applyTheme(theme, toggleButton) {
  const normalized = normalizeTheme(theme);
  const body = document.body;
  if (!body) {
    return;
  }

  body.classList.toggle("theme-light", normalized === "light");
  body.classList.toggle("theme-dark", normalized === "dark");

  if (!toggleButton) {
    return;
  }

  const targetTheme = normalized === "light" ? "dark" : "light";
  const icon = toggleButton.querySelector("[data-theme-icon]");
  const text = toggleButton.querySelector("[data-theme-text]");
  if (icon) {
    icon.textContent = targetTheme === "light" ? "☀" : "☾";
  }
  if (text) {
    text.textContent =
      targetTheme === "light" ? "ライトモード" : "ダークモード";
  }
  toggleButton.setAttribute(
    "aria-label",
    `${targetTheme === "light" ? "ライトモード" : "ダークモード"}で表示`
  );
  toggleButton.setAttribute(
    "title",
    `${targetTheme === "light" ? "ライトモード" : "ダークモード"}に切り替え`
  );
  toggleButton.setAttribute(
    "aria-pressed",
    normalized === "light" ? "true" : "false"
  );
  toggleButton.dataset.targetTheme = targetTheme;
}

function readStoredTheme() {
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (stored === "light" || stored === "dark") {
      return stored;
    }
  } catch (_) {
    // ignore storage access issues
  }
  return null;
}

function persistThemePreference(theme) {
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, normalizeTheme(theme));
  } catch (_) {
    // ignore storage write issues
  }
}

function normalizeTheme(theme) {
  return theme === "light" ? "light" : "dark";
}
