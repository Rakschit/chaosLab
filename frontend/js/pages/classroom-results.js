(function () {
  "use strict";

  var session = AuthAPI.requireLogin();
  if (!session) return;

  var id = new URLSearchParams(window.location.search).get("id") || "";
  var msg = document.getElementById("msg");
  var data = null;

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // never innerHTML
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

  function duration(seconds) {
    if (seconds === null || seconds === undefined) return "-";
    var m = Math.floor(seconds / 60);
    var s = seconds % 60;
    return m + ":" + (s < 10 ? "0" : "") + s;
  }

  function when(iso) {
    if (!iso) return "-";
    var d = new Date(iso);
    return isNaN(d) ? "-" : d.toLocaleString();
  }

  function row(tbody, cells) {
    var tr = el("tr");
    cells.forEach(function (c) {
      var td = el("td", c.cls || "", c.text);
      tr.appendChild(td);
    });
    tbody.appendChild(tr);
  }

  function render() {
    var room = data.classroom;
    document.getElementById("title").textContent = room.name;
    document.getElementById("subtitle").textContent =
      room.difficulty + " · " +
      (room.seed_mode === "same" ? "same incident for all" :
       room.seed_mode === "pool" ? "pool of " + room.pool_size + " incidents" : "random incident each") +
      " · " + room.max_attempts + " game" + (room.max_attempts === 1 ? "" : "s") +
      " per student · code " + room.join_code + (room.archived ? " · archived" : "");

    var students = document.getElementById("students");
    students.textContent = "";
    if (!data.students.length) {
      row(students, [{ text: "No students have joined yet.", cls: "dim" }]);
    }
    data.students.forEach(function (s, i) {
      var played = s.best_score !== null && s.best_score !== undefined;
      row(students, [
        { text: String(i + 1), cls: "dim" },
        { text: s.display_name || "-" },
        { text: s.email || "-", cls: "dim" },
        { text: played ? s.best_score + "/100" : "-", cls: "num " + (played ? (s.best_correct ? "ok" : "") : "dim") },
        { text: duration(s.best_duration_seconds), cls: "num" },
        { text: String(s.games_started), cls: "num" },
        { text: String(s.submissions), cls: "num" },
        { text: when(s.last_submitted_at), cls: "dim" }
      ]);
    });
    document.getElementById("students-hint").textContent =
      data.students.length + " student" + (data.students.length === 1 ? "" : "s") + " · best score, then shortest time";

    var subs = document.getElementById("subs");
    subs.textContent = "";
    if (!data.submissions.length) {
      row(subs, [{ text: "No submissions yet.", cls: "dim" }]);
    }
    data.submissions.forEach(function (s) {
      row(subs, [
        { text: s.display_name || s.email || "-" },
        { text: String(s.attempt_number), cls: "num" },
        { text: s.score + "/100", cls: "num" },
        { text: s.matched + "/" + s.total, cls: "num" },
        { text: s.correct ? "correct" : "incorrect", cls: s.correct ? "ok" : "bad" },
        { text: duration(s.duration_seconds), cls: "num" },
        { text: when(s.created_at), cls: "dim" }
      ]);
    });
    document.getElementById("subs-hint").textContent =
      data.submissions.length >= 500 ? "latest 500" : data.submissions.length + " total";
  }

  function load() {
    show("Loading…");
    fetch(API.classrooms + "/" + encodeURIComponent(id) + "/results", {
      headers: { "Authorization": "Bearer " + session.access_token }
    }).then(function (res) {
      if (res.status === 401) { expired(); return null; }
      return res.json().catch(function () { return {}; }).then(function (body) {
        return { ok: res.ok, body: body };
      });
    }).then(function (res) {
      if (!res) return;
      if (!res.ok) {
        show((res.body && res.body.error) || "Could not load results.", "err");
        return;
      }
      data = res.body;
      show("");
      document.getElementById("body").style.display = "";
      render();
    }, function () {
      show("Could not reach the server.", "err");
    });
  }

  // A cell that starts with = + - @ could run as a formula in a spreadsheet.
  function csvCell(v) {
    var s = v === null || v === undefined ? "" : String(v);
    if (/^[=+\-@\t\r]/.test(s)) s = "'" + s;
    return '"' + s.replace(/"/g, '""') + '"';
  }

  function downloadCsv() {
    if (!data) return;
    var lines = [["Student", "Email", "Best score", "Time taken (s)", "Games started", "Submissions", "Last submission"]];
    data.students.forEach(function (s) {
      lines.push([s.display_name, s.email, s.best_score, s.best_duration_seconds,
        s.games_started, s.submissions, s.last_submitted_at]);
    });
    var text = lines.map(function (l) { return l.map(csvCell).join(","); }).join("\r\n");
    var blob = new Blob([text], { type: "text/csv;charset=utf-8" });
    var a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = (data.classroom.name || "classroom").replace(/[^\w.-]+/g, "_") + "-results.csv";
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
  }

  document.getElementById("refresh").addEventListener("click", load);
  document.getElementById("csv").addEventListener("click", downloadCsv);

  if (!id) { show("No classroom selected.", "err"); return; }
  load();
})();
