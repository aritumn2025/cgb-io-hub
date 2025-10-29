const THEME_STORAGE_KEY = "stg48:theme";
const INPUT_MODE_STORAGE_KEY = "stg48:input-mode";
const INPUT_MODES = {
  STICK: "stick",
  DPAD: "dpad",
};

document.addEventListener("DOMContentLoaded", () => {
  const statusEl = document.querySelector("[data-status]");
  const lampEl = document.querySelector("[data-lamp]");
  const idEl = document.querySelector("[data-id]");
  const infoMenu = document.getElementById("info-menu");
  const infoToggle = document.getElementById("info-toggle");
  const controller = document.querySelector(".controller");
  const stick = document.getElementById("stick");
  const thumb = document.getElementById("stick-thumb");
  const dpad = document.getElementById("dpad");
  const actionButtons = document.querySelectorAll("[data-btn]");
  const controllerScreen = document.getElementById("controller-screen");
  const playerPicker = document.getElementById("player-picker");
  const themeToggle = document.querySelector("[data-theme-toggle]");
  const controlToggle = document.querySelector("[data-control-toggle]");

  initTheme(themeToggle);

  initPlayerPicker(playerPicker);

  const controllerId = getControllerId();

  setScreenVisibility({ controllerScreen, playerPicker, showPicker: !controllerId });

  if (!controllerId) {
    return;
  }

  if (!
    statusEl ||
    !lampEl ||
    !idEl ||
    !infoMenu ||
    !infoToggle ||
    !controller ||
    !stick ||
    !thumb ||
    !dpad
  ) {
    return;
  }

  initInfoMenu(infoMenu, infoToggle);

  idEl.textContent = formatDisplayId(controllerId);

  const status = createStatusManager(statusEl, lampEl);
  const connection = createConnection(controllerId, status.set);
  const state = createInputState(controllerId, connection);

  connection.onOpen(() => state.send(true));
  connection.connect();

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

function createConnection(controllerId, updateStatus) {
  let ws = null;
  let backoff = 800;
  let reconnectTimer = null;
  const openCallbacks = new Set();

  const connectionURL = () => {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${window.location.host}/ws`;
  };

  const scheduleReconnect = () => {
    if (reconnectTimer) {
      window.clearTimeout(reconnectTimer);
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

    updateStatus("接続中…");
    ws = new WebSocket(connectionURL());

    ws.onopen = () => {
      backoff = 800;
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      updateStatus("接続済み");
      ws.send(JSON.stringify({ role: "controller", id: controllerId }));
      openCallbacks.forEach((callback) => callback());
    };

    ws.onclose = () => {
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

  const onOpen = (callback) => {
    openCallbacks.add(callback);
  };

  return { connect, send, onOpen };
}

function createInputState(controllerId, connection) {
  const axes = { x: 0, y: 0 };
  const btn = { a: false };
  let lastSent = "";

  const send = (force = false) => {
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

function initPlayerPicker(playerPicker) {
  if (!playerPicker) {
    return;
  }

  const buttons = playerPicker.querySelectorAll("[data-player-id]");
  buttons.forEach((button) => {
    button.addEventListener("click", () => {
      const playerId = button.dataset.playerId;
      if (!playerId) {
        return;
      }
      const normalized = playerId.toLowerCase();
      if (!isValidPlayerId(normalized)) {
        return;
      }
      const params = new URLSearchParams(window.location.search);
      params.set("id", normalized);
      const query = params.toString();
      const nextUrl = query ? `${window.location.pathname}?${query}` : window.location.pathname;
      window.location.assign(nextUrl);
    });
  });
}

function setScreenVisibility({ controllerScreen, playerPicker, showPicker }) {
  if (controllerScreen) {
    controllerScreen.classList.toggle("is-hidden", Boolean(showPicker));
  }
  if (playerPicker) {
    playerPicker.classList.toggle("is-hidden", !showPicker);
  }
}

function getControllerId() {
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

function formatDisplayId(id) {
  if (id.startsWith("p")) {
    const suffix = id.slice(1);
    return suffix ? `プレイヤー${toFullWidth(suffix)}` : "プレイヤー";
  }
  return toFullWidth(id);
}

function toFullWidth(str) {
  return str.replace(/[0-9a-z]/gi, (char) => {
    const code = char.charCodeAt(0);
    if (code >= 0x30 && code <= 0x39) {
      return String.fromCharCode(0xff10 + (code - 0x30));
    }
    if (code >= 0x41 && code <= 0x5a) {
      return String.fromCharCode(0xff21 + (code - 0x41));
    }
    if (code >= 0x61 && code <= 0x7a) {
      return String.fromCharCode(0xff41 + (code - 0x61));
    }
    return char;
  });
}

function clamp(value) {
  return Math.max(-1, Math.min(1, value));
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
        normalized === INPUT_MODES.DPAD ? "true" : "false",
      );
    }
    if (dpadElement) {
      dpadElement.setAttribute(
        "aria-hidden",
        normalized === INPUT_MODES.DPAD ? "false" : "true",
      );
    }

    const isDpad = normalized === INPUT_MODES.DPAD;
    if (toggleButton) {
      const nextLabel = isDpad ? "スティックに切り替え" : "十字キーに切り替え";
      toggleButton.textContent = nextLabel;
      toggleButton.setAttribute("aria-pressed", isDpad ? "true" : "false");
      const ariaLabel = isDpad ? "スティック操作に切り替え" : "十字キー操作に切り替え";
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
      currentMode = currentMode === INPUT_MODES.DPAD ? INPUT_MODES.STICK : INPUT_MODES.DPAD;
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
  return INPUT_MODES.STICK;
}

function persistInputMode(mode) {
  try {
    window.localStorage.setItem(INPUT_MODE_STORAGE_KEY, normalizeInputMode(mode));
  } catch (_) {
    // ignore storage write issues
  }
}

function normalizeInputMode(mode) {
  return mode === INPUT_MODES.DPAD ? INPUT_MODES.DPAD : INPUT_MODES.STICK;
}

function initTheme(toggleButton) {
  let currentTheme = readStoredTheme() || detectSystemTheme();
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
    text.textContent = targetTheme === "light" ? "ライトモード" : "ダークモード";
  }
  toggleButton.setAttribute("aria-label", `${targetTheme === "light" ? "ライトモード" : "ダークモード"}で表示`);
  toggleButton.setAttribute("title", `${targetTheme === "light" ? "ライトモード" : "ダークモード"}に切り替え`);
  toggleButton.setAttribute("aria-pressed", normalized === "light" ? "true" : "false");
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

function detectSystemTheme() {
  if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) {
    return "light";
  }
  return "dark";
}

function normalizeTheme(theme) {
  return theme === "light" ? "light" : "dark";
}
