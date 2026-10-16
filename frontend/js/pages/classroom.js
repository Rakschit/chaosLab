(function () {
  "use strict";

  // Logged-in users only; requireLogin() brings them back here afterwards.
  var session = AuthAPI.requireLogin();
  if (!session) return;

  var msg = document.getElementById("msg");
  var faculty = document.getElementById("faculty");
  var ownedBox = document.getElementById("owned");
  var joinedBox = document.getElementById("joined");
  var allowBox = document.getElementById("allow");
  var busy = false;

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // never innerHTML: names are user input
    return node;
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin();
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();
    var done = function () { AuthAPI.clearSession(); window.location.href = "login.html"; };
    AuthAPI.logout(session.access_token).then(done, done);
  });

  // Calls core-api and resolves to { ok, status, data }.
  function call(method, path, body) {
    var opts = {
      method: method,
      headers: { "Authorization": "Bearer " + session.access_token }
    };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(API.classrooms + path, opts).then(function (res) {
      if (res.status === 401) { expired(); return new Promise(function () {}); }
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  function errText(res, fallback) {
    return (res.data && res.data.error) || fallback;
  }

  function cap(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

  function seedLabel(room) {
    if (room.seed_mode === "same") return "same incident for all";
    if (room.seed_mode === "pool") return "pool of " + room.pool_size + " incidents";
    return "random incident each";
  }

  var SEED_HINTS = {
    random: "Each student gets a different random incident, so scores are not directly comparable.",
    same: "Everyone gets the exact same incident, so scores and times compare fairly. Students can share answers. One game each.",
    pool2: "Each student gets one of 2 incidents at random. Scores are roughly comparable.",
    pool3: "Each student gets one of 3 incidents at random. Scores are roughly comparable."
  };

  function syncSeedForm() {
    var mode = document.getElementById("c-seed").value;
    var attempts = document.getElementById("c-attempts");
    document.getElementById("seed-hint").textContent = SEED_HINTS[mode] || "";
    if (mode === "same") attempts.value = 1;
    attempts.disabled = mode === "same";
    var max = mode === "pool2" ? 2 : mode === "pool3" ? 3 : 10;
    attempts.max = max;
    if (parseInt(attempts.value, 10) > max) attempts.value = max;
  }

  // ---- faculty: allowance ----
  function renderAllowance(data) {
    allowBox.textContent = "";
    var allowance = data.allowance || {};
    ["easy", "medium", "hard"].forEach(function (level) {
      var s = allowance[level];
      if (!s) return;
      var box = el("div", s.remaining <= 0 ? "out" : "");
      box.appendChild(el("b", "", s.remaining + " / " + s.limit));
      box.appendChild(document.createTextNode(cap(level) + " games left"));
      allowBox.appendChild(box);
    });
    var hint = document.getElementById("reset-hint");
    hint.textContent = data.resets_at
      ? "resets " + new Date(data.resets_at).toLocaleDateString()
      : "";
    if (data.usage_known === false) hint.textContent += " (usage could not be read)";
  }

  // ---- faculty: owned rooms ----
  function copyCode(code, button) {
    var done = function () {
      var old = button.textContent;
      button.textContent = "Copied";
      setTimeout(function () { button.textContent = old; }, 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(code).then(done, function () { show("Code: " + code); });
    } else {
      show("Code: " + code);
    }
  }

  function renderOwned(rooms) {
    ownedBox.textContent = "";
    document.getElementById("owned-hint").textContent = rooms.length + " room" + (rooms.length === 1 ? "" : "s");
    if (!rooms.length) {
      ownedBox.appendChild(el("div", "msg", "No classrooms yet. Create one above."));
      return;
    }
    rooms.forEach(function (room) {
      var card = el("div", "card");
      var title = el("h3", "", room.name);
      if (room.archived) title.appendChild(el("span", "pill warn", "archived"));
      card.appendChild(title);

      var info = el("p", "dim");
      info.appendChild(document.createTextNode(
        room.difficulty + " · " + seedLabel(room) + " · " + room.max_attempts + " game" + (room.max_attempts === 1 ? "" : "s") +
        " per student · " + room.students + " student" + (room.students === 1 ? "" : "s") + " · code "));
      info.appendChild(el("span", "code", room.join_code));
      card.appendChild(info);

      var actions = el("div", "actions");
      var copy = el("button", "btn", "Copy code");
      copy.type = "button";
      copy.addEventListener("click", function () { copyCode(room.join_code, copy); });
      var results = el("a", "btn", "Results");
      results.href = "classroom-results.html?id=" + encodeURIComponent(room.id);
      var archive = el("button", "btn", room.archived ? "Reopen" : "Archive");
      archive.type = "button";
      archive.addEventListener("click", function () { setArchived(room, !room.archived, archive); });
      actions.appendChild(copy);
      actions.appendChild(results);
      actions.appendChild(archive);
      card.appendChild(actions);
      ownedBox.appendChild(card);
    });
  }

  function setArchived(room, archived, button) {
    button.disabled = true;
    call("POST", "/" + encodeURIComponent(room.id) + "/archive", { archived: archived }).then(function (res) {
      if (!res.ok) {
        button.disabled = false;
        show(errText(res, "Could not update the classroom."), "err");
        return;
      }
      load();
    }, function () {
      button.disabled = false;
      show("Could not reach the server.", "err");
    });
  }

  // ---- everyone: joined rooms ----
  function renderJoined(rooms) {
    joinedBox.textContent = "";
    document.getElementById("joined-hint").textContent = rooms.length ? rooms.length + " joined" : "";
    if (!rooms.length) {
      joinedBox.appendChild(el("div", "msg", "You haven't joined a classroom yet."));
      return;
    }
    rooms.forEach(function (room) {
      var left = room.max_attempts - room.attempts_used;
      var card = el("div", "card");
      var title = el("h3", "", room.name);
      if (room.archived) title.appendChild(el("span", "pill warn", "archived"));
      card.appendChild(title);
      card.appendChild(el("p", "dim",
        (room.owner_name ? "by " + room.owner_name + " · " : "") + room.difficulty + " · " +
        room.attempts_used + " of " + room.max_attempts + " game" + (room.max_attempts === 1 ? "" : "s") + " used"));

      var actions = el("div", "actions");
      var start = el("button", "btn", left > 0 ? "Start" : "Done");
      start.type = "button";
      start.disabled = room.archived || left <= 0;
      start.addEventListener("click", function () { startGame(room, start); });
      actions.appendChild(start);
      card.appendChild(actions);
      joinedBox.appendChild(card);
    });
  }

  function startGame(room, button) {
    if (busy) return;
    busy = true;
    button.disabled = true;
    show("Starting…");
    call("POST", "/" + encodeURIComponent(room.id) + "/start").then(function (res) {
      if (!res.ok) {
        busy = false;
        button.disabled = false;
        show(errText(res, "Could not start the game."), "err");
        return;
      }
      // Same hand-off as singleplayer; the response carries classroom_id, so
      // the mission page submits the answer to this room.
      sessionStorage.setItem("simulationResponse", JSON.stringify(res.data));
      window.location.href = "mission.html";
    }, function () {
      busy = false;
      button.disabled = false;
      show("Could not reach the server.", "err");
    });
  }

  // ---- load ----
  function load() {
    return call("GET", "").then(function (res) {
      if (res.status === 403) {
        show(errText(res, "Classrooms are only available to institution accounts."), "err");
        document.getElementById("join-form").style.display = "none";
        document.querySelectorAll(".sec").forEach(function (n) { n.style.display = "none"; });
        return;
      }
      if (!res.ok) {
        show(errText(res, "Could not load classrooms."), "err");
        return;
      }
      show("");
      var data = res.data;
      if (data.member_role === "faculty") {
        faculty.style.display = "";
        renderAllowance(data);
        renderOwned(data.owned || []);
      }
      renderJoined(data.joined || []);
    }, function () {
      show("Could not reach the server.", "err");
    });
  }

  document.getElementById("c-create").addEventListener("click", function () {
    var button = this;
    var name = document.getElementById("c-name").value.trim();
    if (name.length < 2) { show("Give the classroom a name.", "err"); return; }
    button.disabled = true;
    var seedChoice = document.getElementById("c-seed").value;
    call("POST", "", {
      name: name,
      difficulty: document.getElementById("c-diff").value,
      max_attempts: parseInt(document.getElementById("c-attempts").value, 10) || 1,
      seed_mode: seedChoice.indexOf("pool") === 0 ? "pool" : seedChoice,
      pool_size: seedChoice === "pool2" ? 2 : seedChoice === "pool3" ? 3 : 0
    }).then(function (res) {
      button.disabled = false;
      if (!res.ok) { show(errText(res, "Could not create the classroom."), "err"); return; }
      document.getElementById("c-name").value = "";
      show("Created. Share the code " + res.data.join_code + " with your students.", "good");
      load().then(function () { show("Created. Share the code " + res.data.join_code + " with your students.", "good"); });
    }, function () {
      button.disabled = false;
      show("Could not reach the server.", "err");
    });
  });

  function join() {
    var button = document.getElementById("j-join");
    var code = document.getElementById("j-code").value.trim().toUpperCase();
    if (!/^[A-Z0-9]{8}$/.test(code)) { show("A join code is 8 letters or digits.", "err"); return; }
    button.disabled = true;
    call("POST", "/join", { code: code }).then(function (res) {
      button.disabled = false;
      if (!res.ok) { show(errText(res, "Could not join the classroom."), "err"); return; }
      document.getElementById("j-code").value = "";
      load().then(function () { show("Joined " + res.data.name + ".", "good"); });
    }, function () {
      button.disabled = false;
      show("Could not reach the server.", "err");
    });
  }

  document.getElementById("j-join").addEventListener("click", join);
  document.getElementById("j-code").addEventListener("keydown", function (e) {
    if (e.key === "Enter") join();
  });

  document.getElementById("c-seed").addEventListener("change", syncSeedForm);
  syncSeedForm();
  load();
})();
