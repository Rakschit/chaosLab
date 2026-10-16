(function () {
  "use strict";

  // Dashboard is for logged-in users only; requireLogin() sends everyone else
  // to login.html and brings them back here afterwards.
  var session = AuthAPI.requireLogin();
  if (!session) return;

  var LEVELS = ["easy", "medium", "hard"];

  var who = document.getElementById("who");
  var welcome = document.getElementById("welcome");
  var planTag = document.getElementById("plan-tag");
  var adminRow = document.getElementById("admin-row");
  var classroomRow = document.getElementById("classroom-row");
  var limitsMsg = document.getElementById("limits-msg");
  var limitsBox = document.getElementById("limits");
  var resetHint = document.getElementById("reset-hint");
  var upgrade = document.getElementById("upgrade");
  var roomsMsg = document.getElementById("rooms-msg");
  var roomsBox = document.getElementById("rooms-limits");
  var proHints = { sp: "", mp: "" }; // filled in by each limits request

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // textContent only: names are user input
    return node;
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin();
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();
    var done = function () {
      AuthAPI.clearSession();
      window.location.href = "login.html";
    };
    AuthAPI.logout(session.access_token).then(done, done);
  });

  function planName(plan) {
    if (plan === "institution") return "Institution plan";
    return plan === "pro" ? "Pro plan" : "Free plan";
  }

  function renderUser(user) {
    var name = (user && user.display_name) || "";
    who.textContent = name || "account";
    welcome.textContent = name ? "Welcome back, " + name + "." : "Welcome back.";
    if (user && user.plan) {
      planTag.textContent = planName(user.plan);
      planTag.className = "tag" + (user.plan === "pro" || user.plan === "institution" ? " ok" : "");
    }
    // The server checks the role on every admin call; this only shows the link.
    adminRow.style.display = user && user.role === "admin" ? "" : "none";
    // Classrooms belong to institutions; the server checks this on every call.
    classroomRow.style.display = user && user.plan === "institution" ? "" : "none";
  }

  // Show what we have right away, then confirm with the server.
  renderUser(session.user);

  AuthAPI.me(session.access_token).then(function (res) {
    if (res.ok && res.data.user) {
      AuthAPI.updateUser(res.data.user);
      renderUser(res.data.user);
    } else if (res.status === 401) {
      expired();
    }
  }).catch(function () { /* offline: keep the cached view */ });

  function capitalize(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

  // One card: title, how many are left, "x of y used", and a meter.
  function limitCard(title, s, known, usedText) {
    var card = el("div", "card");
    card.appendChild(el("div", "lvl", title));

    var left = el("div", "left", known ? String(s.remaining) : "\u2013");
    left.appendChild(el("small", "", known ? "left" : "usage unavailable"));
    card.appendChild(left);

    card.appendChild(el("p", "",
      known ? s.used + " of " + s.limit + " " + usedText : s.limit + " per month"));

    var meter = el("div", "meter");
    var fill = el("span");
    var pct = known && s.limit > 0 ? Math.round((s.remaining / s.limit) * 100) : 100;
    fill.style.width = pct + "%";
    if (known && s.remaining === 0) meter.className = "meter out";
    else if (known && pct <= 25) meter.className = "meter low";
    meter.appendChild(fill);
    card.appendChild(meter);
    return card;
  }

  // Free users see what Pro would give them (numbers come from the server).
  function updateUpgrade(plan) {
    var parts = [];
    if (proHints.sp) parts.push(proHints.sp);
    if (proHints.mp) parts.push(proHints.mp);
    if (plan === "free" && parts.length) {
      upgrade.textContent = "Need more? Pro allows " + parts.join(" and ") + " per month. ";
      var link = el("a", "", "Request Pro from your profile");
      link.href = "profile.html";
      upgrade.appendChild(link);
      upgrade.appendChild(document.createTextNode("."));
      upgrade.style.display = "";
    } else {
      upgrade.style.display = "none";
    }
  }

  function renderLimits(data) {
    limitsBox.textContent = "";
    var known = data.usage_known !== false;

    LEVELS.forEach(function (level) {
      var s = (data.limits || {})[level];
      if (!s) return;
      limitsBox.appendChild(limitCard(capitalize(level), s, known, "used this month"));
    });

    if (data.resets_at) {
      var t = new Date(data.resets_at);
      if (!isNaN(t.getTime())) {
        resetHint.textContent = "resets " + t.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
      }
    }

    if (data.plan === "free" && data.pro_limits) {
      var p = data.pro_limits;
      proHints.sp = p.easy + " easy, " + p.medium + " medium and " + p.hard + " hard singleplayer games";
    }
    updateUpgrade(data.plan);
  }

  function renderRooms(data) {
    roomsBox.textContent = "";
    var m = data.monthly;
    if (!m) return;
    roomsBox.appendChild(limitCard("Rooms created", m, data.usage_known !== false, "created this month"));

    if (data.plan === "free" && data.pro_limits) {
      proHints.mp = data.pro_limits.per_month + " multiplayer rooms";
    }
    updateUpgrade(data.plan);
  }

  // Each card loads on its own, so one failed request never blanks the other.
  function loadLimits(url, msg, onData, what) {
    fetch(url, {
      headers: { "Authorization": "Bearer " + session.access_token }
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    }).then(function (res) {
      if (res.status === 401) { expired(); return; }
      if (!res.ok) {
        msg.textContent = res.data.error || "Could not load your " + what + " limits.";
        msg.className = "msg err";
        return;
      }
      msg.textContent = res.data.usage_known === false
        ? "Usage counters are temporarily unavailable. Your limits are shown." : "";
      msg.className = "msg";
      onData(res.data);
    }).catch(function () {
      msg.textContent = "Could not reach the server.";
      msg.className = "msg err";
    });
  }

  loadLimits(API.singleplayerLimits, limitsMsg, renderLimits, "singleplayer");
  loadLimits(API.rooms + "/limits", roomsMsg, renderRooms, "multiplayer");
})();