(function () {
  "use strict";

  // Only logged-in users may see this page. The server still checks the admin
  // role on every call; the check below only keeps other users out of the UI.
  var session = AuthAPI.requireLogin();
  if (!session) return;

  var msg = document.getElementById("msg");
  var list = document.getElementById("list");
  var count = document.getElementById("count");
  var feedbackList = document.getElementById("feedback-list");
  var feedbackCount = document.getElementById("feedback-count");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  function expired() {
    AuthAPI.clearSession();
    AuthAPI.requireLogin();
  }

  function updateCount() {
    var n = list.children.length;
    count.textContent = n ? n + " pending" : "";
    if (!n) show("No pending requests.");
  }

  document.getElementById("logout").addEventListener("click", function (event) {
    event.preventDefault();
    var done = function () {
      AuthAPI.clearSession();
      window.location.href = "login.html";
    };
    AuthAPI.logout(session.access_token).then(done, done);
  });

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // never innerHTML: values come from users
    return node;
  }

  function decide(card, request, decision, note, buttons, status) {
    buttons.forEach(function (b) { b.disabled = true; });
    status.textContent = "Saving…";
    status.className = "msg";

    AuthAPI.authedPost("/admin/plan-requests/decision",
      { id: request.id, decision: decision, note: note }, session.access_token).then(function (res) {
      if (res.ok) {
        card.remove();
        updateCount();
        show(res.data.message || "Saved.", "good");
        return;
      }
      if (res.status === 401) { expired(); return; }
      status.textContent = res.data.error || "Could not save the decision.";
      status.className = "msg err";
      // 409 means someone already decided it; leave the card disabled.
      if (res.status !== 409) buttons.forEach(function (b) { b.disabled = false; });
    }).catch(function () {
      status.textContent = "Could not reach the server. Please try again.";
      status.className = "msg err";
      buttons.forEach(function (b) { b.disabled = false; });
    });
  }

  function renderCard(request) {
    var card = el("div", "card");
    card.appendChild(el("h3", "", (request.display_name || "(no name)") + " · " + request.email));
    card.appendChild(el("p", "dim", new Date(request.created_at).toLocaleString()));

    var body = el("p", "", request.message || "(no message)");
    body.style.whiteSpace = "pre-wrap";
    body.style.margin = "12px 0";
    card.appendChild(body);

    var field = el("div", "field");
    var label = el("label", "", "Note to the user (optional, max 500 characters)");
    var note = document.createElement("textarea");
    note.rows = 2;
    note.maxLength = 500;
    field.appendChild(label);
    field.appendChild(note);
    card.appendChild(field);

    var status = el("div", "msg");
    var approve = el("button", "btn sub", "Approve");
    var reject = el("button", "btn abort", "Reject");
    approve.type = "button";
    reject.type = "button";
    var buttons = [approve, reject];

    approve.addEventListener("click", function () {
      decide(card, request, "approve", note.value.trim(), buttons, status);
    });
    reject.addEventListener("click", function () {
      decide(card, request, "reject", note.value.trim(), buttons, status);
    });

    var row = el("div", "split");
    row.appendChild(approve);
    row.appendChild(reject);
    card.appendChild(status);
    card.appendChild(row);
    return card;
  }

  function renderFeedback(item) {
    var card = el("div", "card feedback-card");
    card.appendChild(el("h3", "", (item.display_name || "(no name)") + " · " + item.email));
    card.appendChild(el("p", "dim", new Date(item.created_at).toLocaleString()));
    card.appendChild(el("p", "", "Rating: " + item.rating + "/5" + (item.score === null || item.score === undefined ? "" : " · Score: " + item.score + "/100")));
    card.appendChild(el("p", "", "Difficulty: " + (item.difficulty || "unknown") + " · Seed: " + (item.seed || "unknown")));
    var comment = el("p", "", item.comment || "(no comment)");
    comment.style.whiteSpace = "pre-wrap";
    card.appendChild(comment);
    return card;
  }

  function loadFeedback() {
    AuthAPI.authedGet("/admin/feedback", session.access_token).then(function (res) {
      if (res.status === 401) { expired(); return; }
      if (res.status === 403) { window.location.replace("profile.html"); return; }
      if (!res.ok) { feedbackCount.textContent = "Could not load"; return; }
      var items = res.data.feedback || [];
      feedbackCount.textContent = items.length + (items.length === 1 ? " submission" : " submissions");
      items.forEach(function (item) { feedbackList.appendChild(renderFeedback(item)); });
      if (!items.length) feedbackList.appendChild(el("div", "msg", "No feedback submissions yet."));
    }).catch(function () { feedbackCount.textContent = "Could not load"; });
  }

  AuthAPI.authedGet("/admin/plan-requests", session.access_token).then(function (res) {
    if (res.status === 401) { expired(); return; }
    if (res.status === 403) { window.location.replace("profile.html"); return; }
    if (!res.ok) {
      show(res.data.error || "Could not load requests.", "err");
      return;
    }
    show("");
    (res.data.requests || []).forEach(function (request) {
      list.appendChild(renderCard(request));
    });
    updateCount();
    loadFeedback();
  }).catch(function () {
    show("Could not reach the server.", "err");
  });
})();
