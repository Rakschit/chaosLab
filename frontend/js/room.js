// A multiplayer room: lobby -> countdown -> race -> results.
//
// The server decides everything (who is in, when the clock starts, who is
// fastest). This page polls GET /rooms/{code} and draws what it says. The
// incident itself is only handed out once the race clock has started, and the
// page then opens it in the normal mission screen.
(function () {
  "use strict";

  var session = AuthAPI.requireLogin();
  if (!session) return;

  function $(id) { return document.getElementById(id); }

  var params = new URLSearchParams(window.location.search);
  var code = (params.get("code") || "").toUpperCase().replace(/[^A-Z0-9]/g, "");
  if (code.length !== 6) {
    window.location.replace("multiplayer.html");
    return;
  }

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
        return new Promise(function () {});
      }
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  var base = API.rooms + "/" + encodeURIComponent(code);
  var shownCode = code.slice(0, 4) + "-" + code.slice(4);

  function say(text, kind) {
    var node = $("msg");
    node.textContent = text || "";
    node.className = "msg" + (kind ? " " + kind : "");
  }

  function cap(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : s; }

  function mmss(total) {
    total = Math.max(0, Math.floor(total));
    var m = Math.floor(total / 60), s = total % 60;
    return (m < 10 ? "0" : "") + m + ":" + (s < 10 ? "0" : "") + s;
  }

  // ---- state ----
  var state = null;          // last response from the server
  var clockOffset = 0;       // server time minus this device's time, in ms
  var pollTimer = null;
  var lastPhase = "";
  var lastEndPoll = 0;       // stops the "time is up" refresh from repeating every tick
  var retryOpenAt = 0;       // after a failed open, wait before trying again
  var opening = false;       // already opening the mission
  var openedKey = "raceOpened:" + code;

  function now() { return Date.now() + clockOffset; }

  // ---- drawing ----
  function button(label, className, onClick) {
    var b = document.createElement("button");
    b.type = "button";
    b.className = className;
    b.textContent = label;
    b.addEventListener("click", onClick);
    return b;
  }

  function cell(text, className) {
    var td = document.createElement("td");
    td.textContent = text;
    if (className) td.className = className;
    return td;
  }

  function phase() {
    var room = state.room;
    if (room.status === "finished") return "finished";
    if (room.status === "playing") {
      return now() < Date.parse(room.started_at) ? "countdown" : "running";
    }
    return "waiting";
  }

  function renderBoard() {
    var room = state.room, ph = phase();
    var body = $("board");
    body.textContent = "";

    state.players.forEach(function (p, i) {
      var tr = document.createElement("tr");
      if (p.is_you) tr.className = "me";

      var started = ph === "running" || ph === "finished" || ph === "countdown";
      tr.appendChild(cell(started ? String(i + 1) : "-"));

      var name = cell(p.name || "player", p.is_you ? "you" : "");
      if (p.is_host) {
        var host = document.createElement("span");
        host.className = "tag";
        host.textContent = "host";
        name.appendChild(host);
      }
      if (p.is_you) {
        var you = document.createElement("span");
        you.className = "tag ok";
        you.textContent = "you";
        name.appendChild(you);
      }
      // The server only sends kick_id to the host, for players other than the host.
      if (state.is_host && p.kick_id && ph !== "finished") {
        var kick = button("Kick", "btn danger", function () { kickPlayer(p.kick_id, p.name); });
        kick.style.cssText = "margin-left:10px;padding:0 8px;font-size:11px;line-height:20px";
        name.appendChild(kick);
      }
      tr.appendChild(name);

      var status, cls = "";
      if (p.solved) {
        status = "Solved in " + mmss(p.solved_seconds || 0);
        cls = "ok";
      } else if (p.ended) {
        status = "Ended";
        cls = "bad";
      } else if (ph === "waiting") {
        status = "Ready";
      } else if (ph === "countdown") {
        status = "Get ready";
      } else if (ph === "running") {
        status = p.attempts ? "Working on it" : "Investigating";
      } else {
        status = "Did not solve";
        cls = "bad";
      }
      tr.appendChild(cell(status, cls));
      tr.appendChild(cell(p.attempts ? p.best_score + "/100" : "-"));
      tr.appendChild(cell(String(p.attempts)));
      body.appendChild(tr);
    });

    $("board-label").textContent = ph === "finished" ? "Final results" : ph === "waiting" ? "Players" : "Scoreboard";
    $("board-hint").textContent = ph === "waiting"
      ? state.players.length + " / " + room.max_players + " joined"
      : "fastest correct answer wins; ties go to the best score";
  }

  function meta() {
    var r = state.room;
    return cap(r.difficulty) + " " + r.environment + " incident - up to " + r.max_players + " players - " +
      Math.round(r.time_limit_seconds / 60) + " minute limit - hosted by " + (r.host_name || "a player");
  }

  function renderActions() {
    var ph = phase(), box = $("actions");
    box.textContent = "";
    var isHost = state.is_host;
    var solved = state.you && state.you.solved;
    var ended = state.you && state.you.ended;

    if (ph === "waiting") {
      if (isHost) {
        var enough = state.players.length >= 2;
        var start = button("Start race", "go", startRace);
        start.disabled = !enough;
        if (!enough) start.title = "At least 2 players are needed";
        box.appendChild(start);
        box.appendChild(button("Close room", "btn danger", leaveRoom));
      } else {
        box.appendChild(button("Leave room", "btn", leaveRoom));
      }
    } else if (ph === "running") {
      if (!solved && !ended) box.appendChild(button("Open mission", "go", openMission));
      if (isHost) box.appendChild(button("End race for everyone", "btn danger", endRace));
    } else if (ph === "countdown") {
      if (isHost) box.appendChild(button("Cancel and end", "btn danger", endRace));
    } else {
      var back = document.createElement("a");
      back.className = "btn";
      back.href = "multiplayer.html";
      back.textContent = "Back to multiplayer";
      box.appendChild(back);
    }
  }

  // The big text changes every second during the countdown and the race.
  function renderClock() {
    if (!state) return;
    var room = state.room, ph = phase();
    var big = $("big"), sub = $("bigsub");

    $("share").hidden = ph !== "waiting";

    if (ph === "waiting") {
      big.textContent = state.players.length < 2 ? "Waiting for players" : "Ready to start";
      big.className = "big";
      sub.textContent = state.is_host
        ? "Share the code below. You can start once at least 2 players have joined."
        : "Waiting for the host to start the race.";
    } else if (ph === "countdown") {
      var secs = Math.max(0, Math.ceil((Date.parse(room.started_at) - now()) / 1000));
      big.textContent = "Starting in " + secs;
      big.className = "big ok";
      sub.textContent = "Everyone gets the same incident. Be the first to submit a fully correct answer.";
    } else if (ph === "running") {
      var left = (Date.parse(room.ends_at) - now()) / 1000;
      big.textContent = mmss(left) + " left";
      big.className = "big ok";
      sub.textContent = state.you && state.you.solved
        ? "You solved it! Waiting for the others, or for time to run out."
        : state.you && state.you.ended
          ? "You ended your simulation. Waiting for the others, or for time to run out."
          : "The race is on. Open the mission and submit your answer.";
      if (left <= 0 && Date.now() - lastEndPoll > 1500) { // time is up: ask for the final result
        lastEndPoll = Date.now();
        refresh();
      }
    } else {
      var winner = state.players.filter(function (p) { return p.solved; })[0];
      big.className = "big";
      if (winner) {
        big.textContent = winner.is_you ? "You won!" : (winner.name || "A player") + " won";
        sub.textContent = "Fastest correct answer: " + mmss(winner.solved_seconds || 0) + ".";
      } else {
        big.textContent = "Race over";
        sub.textContent = "Nobody solved the incident this time.";
      }
    }
  }

  function render() {
    $("title").textContent = "Room " + shownCode;
    $("sub").textContent = meta();
    $("code").textContent = shownCode;
    $("banner").hidden = false;
    $("banner-name").textContent = phase() === "finished" ? "results" : phase() === "waiting" ? "lobby" : "race";
    lastPhase = phase();
    renderClock();
    renderActions();
    renderBoard();
    maybeAutoOpen();
  }

  // ---- race flow ----
  // When the clock starts, take everyone who has not solved it yet into the
  // mission, once. Coming back to this page afterwards stays here.
  function maybeAutoOpen() {
    if (opening || Date.now() < retryOpenAt || phase() !== "running" || (state.you && (state.you.solved || state.you.ended))) return;
    var seen = false;
    try { seen = sessionStorage.getItem(openedKey) === state.room.started_at; } catch (e) { /* storage unavailable */ }
    if (!seen) openMission();
  }

  function openMission() {
    if (opening) return;
    opening = true;
    say("Opening the mission...");
    call("GET", base + "/game").then(function (r) {
      if (!r.ok) {
        opening = false;
        retryOpenAt = Date.now() + 4000;
        say(r.data.error || "Could not open the mission.", "err");
        return;
      }
      try {
        sessionStorage.setItem("simulationResponse", JSON.stringify(r.data));
        sessionStorage.setItem(openedKey, state.room.started_at);
      } catch (e) {
        opening = false;
        say("Your browser blocked storage, which the mission needs.", "err");
        return;
      }
      window.location.href = "mission.html";
    }).catch(function () {
      opening = false;
      retryOpenAt = Date.now() + 4000;
      say("Could not reach the server.", "err");
    });
  }

  function startRace() {
    say("Starting...");
    call("POST", base + "/start").then(function (r) {
      if (!r.ok) say(r.data.error || "Could not start the race.", "err");
      else say("");
      refresh();
    }).catch(function () { say("Could not reach the server.", "err"); });
  }

  function endRace() {
    if (!window.confirm("End the race for everyone?")) return;
    call("POST", base + "/end").then(function (r) {
      if (!r.ok) say(r.data.error || "Could not end the race.", "err");
      refresh();
    }).catch(function () { say("Could not reach the server.", "err"); });
  }

  function kickPlayer(id, name) {
    if (!window.confirm("Remove " + (name || "this player") + " from the room? They will not be able to rejoin.")) return;
    call("POST", base + "/kick", { player: id }).then(function (r) {
      if (!r.ok) say(r.data.error || "Could not remove the player.", "err");
      else say((name || "Player") + " was removed.", "good");
      refresh();
    }).catch(function () { say("Could not reach the server.", "err"); });
  }

  // A removed player is sent back to the multiplayer page.
  function removedGoodbye(message) {
    say(message || "The host removed you from this room.", "err");
    $("sub").textContent = "You were removed from this room.";
    clearTimeout(pollTimer);
    setTimeout(function () { window.location.href = "multiplayer.html"; }, 3000);
  }

  function leaveRoom() {
    var closing = state && state.is_host;
    if (closing && !window.confirm("Close this room? Everyone will be sent out.")) return;
    call("POST", base + "/leave").then(function () {
      window.location.href = "multiplayer.html";
    }).catch(function () { say("Could not reach the server.", "err"); });
  }

  // ---- copy buttons ----
  function copy(text, label) {
    function done() { say(label + " copied.", "good"); }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { say("Copy failed - select it by hand.", "err"); });
    } else {
      say("Copy is not available here - select it by hand.", "err");
    }
  }
  $("copy-code").addEventListener("click", function () { copy(shownCode, "Code"); });
  $("copy-link").addEventListener("click", function () {
    copy(new URL("room.html?code=" + code, window.location.href).href, "Link");
  });

  // ---- polling ----
  var triedJoin = false;
  var polling = false;
  var again = false;         // a refresh was asked for while one was running

  function schedule() {
    clearTimeout(pollTimer);
    if (state && state.room.status === "finished") return; // nothing more will change
    pollTimer = setTimeout(refresh, document.hidden ? 6000 : 2000);
  }

  function refresh() {
    if (polling) { again = true; return Promise.resolve(); }
    polling = true;
    return call("GET", base).then(function (r) {
      if (again) { again = false; polling = false; return refresh(); } // an action happened meanwhile: this answer may be stale
      if (r.status === 403 && !triedJoin) {
        // Opened from a shared link: join first, then load.
        triedJoin = true;
        return call("POST", API.rooms + "/join", { code: code }).then(function (j) {
          polling = false;
          if (j.ok) return refresh();
          if (j.data.kicked) { removedGoodbye(j.data.error); return; }
          say(j.data.error || "Could not join this room.", "err");
          $("sub").textContent = "You are not in this room.";
        });
      }
      polling = false;
      if (!r.ok) {
        if (r.data.kicked) { removedGoodbye(r.data.error); return; }
        if (r.status === 404 || r.status === 403) {
          $("sub").textContent = r.status === 404 ? "This room does not exist." : "You are not in this room.";
          say(r.data.error || "", "err");
          return;
        }
        say(r.data.error || "Could not load the room. Retrying...", "err");
        schedule();
        return;
      }
      say("");
      state = r.data;
      clockOffset = Date.parse(state.server_time) - Date.now();
      render();
      schedule();
    }).catch(function () {
      polling = false;
      say("Connection lost. Retrying...", "err");
      schedule();
    });
  }

  document.addEventListener("visibilitychange", function () {
    if (!document.hidden) refresh();
  });

  // Once a second (a bit faster for a smooth countdown): redraw the clock, and
  // redraw everything when the phase changes (countdown -> race).
  setInterval(function () {
    if (!state || state.room.status === "finished") return;
    if (phase() !== lastPhase) return render();
    renderClock();
    maybeAutoOpen();
  }, 250);

  // Coming back to this page with the browser's back button restores it from
  // cache; poll again so it is never stale.
  window.addEventListener("pageshow", function (event) {
    if (event.persisted) { opening = false; refresh(); }
  });

  refresh();
})();