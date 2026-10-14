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

  function renderLimits(data) {
    limitsBox.textContent = "";
    var known = data.usage_known !== false;

    LEVELS.forEach(function (level) {
      var s = (data.limits || {})[level];
      if (!s) return;

      var card = el("div", "card");
      card.appendChild(el("div", "lvl", capitalize(level)));

      var left = el("div", "left", known ? String(s.remaining) : "–");
      left.appendChild(el("small", "", known ? "left" : "usage unavailable"));
      card.appendChild(left);

      card.appendChild(el("p", "",
        known ? s.used + " of " + s.limit + " used this month" : s.limit + " per month"));

      var meter = el("div", "meter");
      var fill = el("span");
      var pct = known && s.limit > 0 ? Math.round((s.remaining / s.limit) * 100) : 100;
      fill.style.width = pct + "%";
      if (known && s.remaining === 0) meter.className = "meter out";
      else if (known && pct <= 25) meter.className = "meter low";
      meter.appendChild(fill);
      card.appendChild(meter);

      limitsBox.appendChild(card);
    });

    if (data.resets_at) {
      var t = new Date(data.resets_at);
      if (!isNaN(t.getTime())) {
        resetHint.textContent = "resets " + t.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
      }
    }

    // Free users see what Pro would give them (numbers come from the server).
    if (data.plan === "free" && data.pro_limits) {
      var p = data.pro_limits;
      upgrade.textContent = "Need more? Pro allows " + p.easy + " easy, " + p.medium +
        " medium and " + p.hard + " hard games per month. ";
      var link = el("a", "", "Request Pro from your profile");
      link.href = "profile.html";
      upgrade.appendChild(link);
      upgrade.appendChild(document.createTextNode("."));
      upgrade.style.display = "";
    } else {
      upgrade.style.display = "none";
    }
  }

  fetch(API.singleplayerLimits, {
    headers: { "Authorization": "Bearer " + session.access_token }
  }).then(function (res) {
    return res.json().catch(function () { return {}; }).then(function (data) {
      return { ok: res.ok, status: res.status, data: data };
    });
  }).then(function (res) {
    if (res.status === 401) { expired(); return; }
    if (!res.ok) {
      limitsMsg.textContent = res.data.error || "Could not load your daily limits.";
      limitsMsg.className = "msg err";
      return;
    }
    limitsMsg.textContent = res.data.usage_known === false
      ? "Usage counters are temporarily unavailable. Your daily limits are shown." : "";
    limitsMsg.className = "msg";
    renderLimits(res.data);
  }).catch(function () {
    limitsMsg.textContent = "Could not reach the server.";
    limitsMsg.className = "msg err";
  });
})();