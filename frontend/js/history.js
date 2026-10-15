(function () {
  "use strict";

  var session = AuthAPI.requireLogin();
  if (!session) return;

  var PAGE = 20;
  var msg = document.getElementById("msg");
  var list = document.getElementById("list");
  var summary = document.getElementById("summary");
  var moreButton = document.getElementById("more");
  var offset = 0;
  var shown = 0;
  var best = 0;
  var solved = 0;

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin();
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // never innerHTML
    return node;
  }

  function formatDuration(seconds) {
    if (seconds === null || seconds === undefined) return "-";
    var m = Math.floor(seconds / 60);
    var s = seconds % 60;
    return m + ":" + (s < 10 ? "0" : "") + s;
  }

  function flag(ok) { return ok ? "✓" : "✗"; }

  function renderFindings(findings) {
    var box = el("div", "dim");
    box.style.marginTop = "10px";
    if (!findings || !findings.length) {
      box.textContent = "No findings saved.";
      return box;
    }
    findings.forEach(function (f, i) {
      var line = el("p", "",
        "#" + (i + 1) + "  " +
        flag(f.source_ip_ok) + " IP " + f.source_ip + "   " +
        flag(f.endpoint_ok) + " Endpoint " + f.endpoint + "   " +
        flag(f.anomaly_type_ok) + " Type " + f.anomaly_type);
      line.style.margin = "4px 0";
      line.style.wordBreak = "break-word";
      box.appendChild(line);
    });
    return box;
  }

  function renderItem(it) {
    var card = el("div", "card");
    var official = it.attempt_number === 1;
    var room = it.room_code ? it.room_code.slice(0, 4) + "-" + it.room_code.slice(4) : "";

    card.appendChild(el("h3", "",
      (room ? "Multiplayer  ·  " : "") +
      it.score + "/100  ·  " + it.matched + "/" + it.total + " found  ·  " +
      it.difficulty + " " + it.environment));
    card.appendChild(el("p", "dim",
      new Date(it.created_at).toLocaleString() +
      (room ? "  ·  room " + room + "  ·  attempt " + it.attempt_number
            : "  ·  attempt " + it.attempt_number + (official ? " (official)" : " (practice)")) +
      "  ·  time " + formatDuration(it.duration_seconds) +
      "  ·  seed " + it.seed +
      (it.correct ? "  ·  solved" : "")));

    var details = renderFindings(it.findings);
    details.style.display = "none";
    var toggle = el("button", "btn", "Show answers I submitted");
    toggle.type = "button";
    toggle.addEventListener("click", function () {
      var open = details.style.display === "none";
      details.style.display = open ? "" : "none";
      toggle.textContent = open ? "Hide answers I submitted" : "Show answers I submitted";
    });
    card.appendChild(toggle);
    card.appendChild(details);
    return card;
  }

  function updateSummary() {
    summary.textContent = shown
      ? shown + " loaded · best " + best + " · solved " + solved
      : "";
  }

  // History lives on core-api, not auth-api, so it has its own small fetch.
  function fetchPage() {
    moreButton.disabled = true;
    var url = API.singleplayerHistory + "?limit=" + PAGE + "&offset=" + offset;

    fetch(url, { headers: { "Authorization": "Bearer " + session.access_token } })
      .then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (data) {
          return { ok: res.ok, status: res.status, data: data };
        });
      })
      .then(function (res) {
        if (res.status === 401) { expired(); return; }
        if (!res.ok) {
          show(res.data.error || "Could not load history.", "err");
          moreButton.disabled = false;
          return;
        }
        var items = res.data.results || [];
        items.forEach(function (it) {
          list.appendChild(renderItem(it));
          shown++;
          if (it.score > best) best = it.score;
          if (it.correct) solved++;
        });
        offset += items.length;
        show(shown ? "" : "No games yet. Finish a mission and submit an answer to see it here.");
        updateSummary();
        moreButton.style.display = res.data.has_more ? "" : "none";
        moreButton.disabled = false;
      })
      .catch(function () {
        show("Could not reach the server.", "err");
        moreButton.disabled = false;
      });
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();
    var done = function () {
      AuthAPI.clearSession();
      window.location.href = "login.html";
    };
    AuthAPI.logout(session.access_token).then(done, done);
  });

  moreButton.addEventListener("click", fetchPage);
  fetchPage();
})();