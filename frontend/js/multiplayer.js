// Multiplayer lobby: create a room, join with a code, or join a public room.
//
// Joining is free. Creating a room is limited by plan (the server enforces the
// limits; this page only shows them and disables options your plan cannot use).
(function () {
  "use strict";

  var session = AuthAPI.requireLogin();
  if (!session) return;

  function $(id) { return document.getElementById(id); }

  // One place for every API call: adds the token, handles an expired login.
  function call(method, url, body) {
    var init = { method: method, headers: { "Authorization": "Bearer " + session.access_token } };
    if (body !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body);
    }
    return fetch(url, init).then(function (res) {
      if (res.status === 401) {
        AuthAPI.clearSession();
        AuthAPI.requireLogin();
        return new Promise(function () {}); // the page is leaving
      }
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  function say(node, text, kind) {
    node.textContent = text || "";
    node.className = "msg" + (kind ? " " + kind : "");
  }

  function formatCode(code) { return code.slice(0, 4) + "-" + code.slice(4); }

  function cap(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : s; }

  // ---- Environment dropdown (kept in the front end, like singleplayer.js) ----
  var environments = ["nginx"];
  var envSelect = $("env");
  envSelect.innerHTML = "";
  environments.forEach(function (name) {
    var option = document.createElement("option");
    option.value = name;
    option.textContent = name;
    envSelect.appendChild(option);
  });

  // ---- Your room limits ----
  var limits = null;

  function box(label, value, out) {
    var div = document.createElement("div");
    if (out) div.className = "out";
    var b = document.createElement("b");
    b.textContent = value;
    div.appendChild(b);
    div.appendChild(document.createTextNode(label));
    return div;
  }

  function renderLimits() {
    var allow = $("allow");
    allow.textContent = "";
    if (!limits) return;

    var m = limits.monthly, h = limits.hourly, a = limits.active_rooms;
    allow.appendChild(box("rooms left this month", m.remaining + " / " + m.limit, m.remaining === 0));
    allow.appendChild(box("rooms left this hour", h.remaining + " / " + h.limit, h.remaining === 0));
    allow.appendChild(box("open rooms you host", a.used + " / " + a.limit, a.used >= a.limit));
    allow.appendChild(box("max players per room", String(limits.max_players), false));

    // Disable room sizes the plan cannot create.
    var max = $("max");
    Array.prototype.forEach.call(max.options, function (opt) {
      var n = Number(opt.value);
      opt.disabled = n > limits.max_players;
      opt.textContent = n + (opt.disabled ? " (Pro)" : "");
      if (opt.disabled && opt.selected) max.value = String(Math.min(4, limits.max_players));
    });

    var plan = limits.plan === "free" ? "Free" : limits.plan === "pro" ? "Pro" : cap(limits.plan);
    $("plan-note").textContent = "";
    $("plan-note").appendChild(document.createTextNode("Joining a room is "));
    var always = document.createElement("b");
    always.textContent = "always free";
    $("plan-note").appendChild(always);
    $("plan-note").appendChild(document.createTextNode(". Only creating a room counts toward your limits (you are on the " + plan + " plan"));
    if (limits.plan === "free" && limits.pro_limits) {
      var p = limits.pro_limits;
      $("plan-note").appendChild(document.createTextNode("; Pro allows " + p.per_month + " rooms a month, " + p.max_active +
        " open at once and up to " + p.max_players + " players"));
    }
    $("plan-note").appendChild(document.createTextNode(").  Rooms reset on the 1st at 00:00 UTC."));
  }

  function loadLimits() {
    return call("GET", API.rooms + "/limits").then(function (r) {
      if (r.ok) { limits = r.data; renderLimits(); }
    }).catch(function () { /* the server still enforces the limits */ });
  }

  // ---- Create a room ----
  var creating = false;
  $("create-form").addEventListener("submit", function (event) {
    event.preventDefault();
    if (creating) return;
    creating = true;
    $("create-btn").disabled = true;
    say($("create-msg"), "Creating room...");

    call("POST", API.rooms, {
      environment: envSelect.value,
      difficulty: $("diff").value,
      max_players: Number($("max").value),
      public: $("vis").value === "public"
    }).then(function (r) {
      if (r.ok && r.data.code) {
        window.location.href = "room.html?code=" + encodeURIComponent(r.data.code);
        return;
      }
      say($("create-msg"), r.data.error || "Could not create the room.", "err");
      loadLimits();
    }).catch(function () {
      say($("create-msg"), "Could not reach the server.", "err");
    }).then(function () {
      creating = false;
      $("create-btn").disabled = false;
    });
  });

  // ---- Join with a code ----
  function join(code, msgNode, button) {
    if (button) button.disabled = true;
    say(msgNode, "Joining...");
    return call("POST", API.rooms + "/join", { code: code }).then(function (r) {
      if (r.ok && r.data.code) {
        window.location.href = "room.html?code=" + encodeURIComponent(r.data.code);
        return;
      }
      say(msgNode, r.data.error || "Could not join the room.", "err");
      if (button) button.disabled = false;
    }).catch(function () {
      say(msgNode, "Could not reach the server.", "err");
      if (button) button.disabled = false;
    });
  }

  $("join-form").addEventListener("submit", function (event) {
    event.preventDefault();
    var code = $("code").value.toUpperCase().replace(/[^A-Z0-9]/g, "");
    if (code.length !== 6) {
      say($("join-msg"), "A room code is 6 letters or digits, like K7Q2-X9.", "err");
      return;
    }
    join(code, $("join-msg"), $("join-btn"));
  });

  // ---- Open (public) rooms ----
  function cell(text, className) {
    var td = document.createElement("td");
    td.textContent = text;
    if (className) td.className = className;
    return td;
  }

  function renderRooms(rooms) {
    var body = $("rooms-body");
    body.textContent = "";
    if (!rooms.length) {
      var tr = document.createElement("tr");
      var td = cell("No public rooms right now. Create one!", "empty");
      td.colSpan = 6;
      tr.appendChild(td);
      body.appendChild(tr);
      return;
    }
    rooms.forEach(function (room) {
      var full = room.player_count >= room.max_players;
      var tr = document.createElement("tr");
      tr.appendChild(cell(formatCode(room.code)));
      tr.appendChild(cell(room.host_name || "-"));
      tr.appendChild(cell(room.environment));
      tr.appendChild(cell(cap(room.difficulty)));
      tr.appendChild(cell(room.player_count + " / " + room.max_players, full ? "bad" : "ok"));

      var td = document.createElement("td");
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "btn";
      btn.textContent = full ? "Full" : "Join";
      btn.disabled = full;
      btn.addEventListener("click", function () { join(room.code, $("join-msg"), btn); });
      td.appendChild(btn);
      tr.appendChild(td);
      body.appendChild(tr);
    });
  }

  function loadRooms() {
    return call("GET", API.rooms).then(function (r) {
      if (r.ok) {
        renderRooms(r.data.rooms || []);
      } else {
        $("rooms-body").textContent = "";
        var tr = document.createElement("tr");
        var td = cell(r.data.error || "Could not load rooms.", "empty");
        td.colSpan = 6;
        tr.appendChild(td);
        $("rooms-body").appendChild(tr);
      }
    }).catch(function () { /* keep what is shown */ });
  }

  $("refresh").addEventListener("click", function (event) {
    event.preventDefault();
    loadRooms();
    loadLimits();
  });

  loadLimits();
  loadRooms();
})();
