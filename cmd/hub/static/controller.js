document.addEventListener("DOMContentLoaded", () => {
  const statusEl = document.querySelector("[data-status]");
  const lampEl = document.querySelector("[data-lamp]");
  const idEl = document.querySelector("[data-id]");
  const infoMenu = document.getElementById("info-menu");
  const infoToggle = document.getElementById("info-toggle");
  const stick = document.getElementById("stick");
  const thumb = document.getElementById("stick-thumb");
  const actionButtons = document.querySelectorAll("[data-btn]");
  const controllerScreen = document.getElementById("controller-screen");
  const playerPicker = document.getElementById("player-picker");

  initPlayerPicker(playerPicker);

  const controllerId = getControllerId();

  setScreenVisibility({ controllerScreen, playerPicker, showPicker: !controllerId });

  if (!controllerId) {
    return;
  }

  if (!statusEl || !lampEl || !idEl || !infoMenu || !infoToggle || !stick || !thumb) {
    return;
  }

  initInfoMenu(infoMenu, infoToggle);

  idEl.textContent = formatDisplayId(controllerId);

  const status = createStatusManager(statusEl, lampEl);
  const connection = createConnection(controllerId, status.set);
  const state = createInputState(controllerId, connection);

  connection.onOpen(() => state.send(true));
  connection.connect();

  initStick(stick, thumb, state);
  initButtons(actionButtons, state);

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
  let activePointer = null;

  const updateThumb = (x, y) => {
    thumb.style.left = `${(x + 1) * 50}%`;
    thumb.style.top = `${(y + 1) * 50}%`;
  };

  const resetStick = () => {
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
      if (stick.hasPointerCapture(event.pointerId)) {
        stick.releasePointerCapture(event.pointerId);
      }
      resetStick();
    });
  });
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
  const id = params.get("id") || "p1";
  return id.toLowerCase();
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

function isValidPlayerId(id) {
  return id === "p1" || id === "p2" || id === "p3" || id === "p4";
}
