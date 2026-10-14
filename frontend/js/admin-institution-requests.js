(function () {
  "use strict";

  // Only logged-in users may see this page. The server checks the admin role
  // on every call; this only keeps other users out of the UI.
  var session = AuthAPI.requireLogin();
  if (!session) return;

  var msg = document.getElementById("inst-msg");
  var list = document.getElementById("inst-list");
  var count = document.getElementById("inst-count");

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
    if (!n) show("No pending institution requests.");
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text; // never innerHTML: values come from users
    return node;
  }

  function field(labelText, input) {
    var f = el("div", "field");
    f.appendChild(el("label", "", labelText));
    f.appendChild(input);
    return f;
  }

  function numberInput(value, max) {
    var input = document.createElement("input");
    input.type = "number";
    input.min = "0";
    input.max = String(max);
    input.value = String(value);
    return input;
  }

  // One year from today, as YYYY-MM-DD (the server default for a new license).
  function defaultExpiry() {
    var d = new Date();
    d.setFullYear(d.getFullYear() + 1);
    return d.toISOString().slice(0, 10);
  }

  function wholeNumber(input, label) {
    var n = Number(input.value);
    if (input.value.trim() === "" || !Number.isInteger(n) || n < 0) {
      return { error: label + " must be a whole number, 0 or more." };
    }
    return { value: n };
  }

  function decide(card, request, body, buttons, status) {
    buttons.forEach(function (b) { b.disabled = true; });
    status.textContent = "Saving…";
    status.className = "msg";

    body.id = request.id;
    AuthAPI.authedPost("/admin/institution-requests/decision", body, session.access_token).then(function (res) {
      if (res.ok) {
        card.remove();
        updateCount();
        show(res.data.message || "Saved.", "good");
        return;
      }
      if (res.status === 401) { expired(); return; }
      status.textContent = res.data.error || "Could not save the decision.";
      status.className = "msg err";
      // 409 can also mean a domain or code clash, so let the admin edit and retry.
      buttons.forEach(function (b) { b.disabled = false; });
    }).catch(function () {
      status.textContent = "Could not reach the server. Please try again.";
      status.className = "msg err";
      buttons.forEach(function (b) { b.disabled = false; });
    });
  }

  function renderCard(request) {
    var card = el("div", "card");
    card.appendChild(el("h3", "", request.institution_name));
    card.appendChild(el("p", "dim", (request.display_name || "(no name)") + " · " + request.email +
      " · " + new Date(request.created_at).toLocaleString()));
    card.appendChild(el("p", "", "Domains: " + (request.domains || []).join(", ")));
    card.appendChild(el("p", "dim", "Seats wanted: " + (request.seats_wanted > 0 ? request.seats_wanted : "not specified")));

    var body = el("p", "", request.message || "(no message)");
    body.style.whiteSpace = "pre-wrap";
    body.style.margin = "12px 0";
    card.appendChild(body);

    var slug = document.createElement("input");
    slug.type = "text";
    slug.maxLength = 40;
    slug.spellcheck = false;
    slug.placeholder = "leave empty to generate from the name";
    card.appendChild(field("Institution code (slug)", slug));

    var seats = numberInput(request.seats_wanted || 0, 1000000);
    card.appendChild(field("Seats (0 = unlimited)", seats));

    var easy = numberInput(200, 100000);
    var medium = numberInput(150, 100000);
    var hard = numberInput(100, 100000);
    var limits = el("div", "split");
    limits.appendChild(field("Easy / month", easy));
    limits.appendChild(field("Medium / month", medium));
    limits.appendChild(field("Hard / month", hard));
    card.appendChild(limits);

    var expiry = document.createElement("input");
    expiry.type = "date";
    expiry.value = defaultExpiry();
    card.appendChild(field("License valid until", expiry));

    var note = document.createElement("textarea");
    note.rows = 2;
    note.maxLength = 500;
    card.appendChild(field("Note to the requester (optional, max 500 characters)", note));

    var status = el("div", "msg");
    var approve = el("button", "btn sub", "Approve");
    var reject = el("button", "btn abort", "Reject");
    approve.type = "button";
    reject.type = "button";
    var buttons = [approve, reject];

    approve.addEventListener("click", function () {
      var s = wholeNumber(seats, "Seats");
      var e = wholeNumber(easy, "Easy limit");
      var m = wholeNumber(medium, "Medium limit");
      var h = wholeNumber(hard, "Hard limit");
      var bad = s.error || e.error || m.error || h.error;
      if (bad) { status.textContent = bad; status.className = "msg err"; return; }
      if (!expiry.value) { status.textContent = "Pick the license end date."; status.className = "msg err"; return; }

      decide(card, request, {
        decision: "approve",
        note: note.value.trim(),
        slug: slug.value.trim(),
        max_seats: s.value,
        monthly_limits: { easy: e.value, medium: m.value, hard: h.value },
        license_expires_at: expiry.value
      }, buttons, status);
    });
    reject.addEventListener("click", function () {
      decide(card, request, { decision: "reject", note: note.value.trim() }, buttons, status);
    });

    var row = el("div", "split");
    row.appendChild(approve);
    row.appendChild(reject);
    card.appendChild(status);
    card.appendChild(row);
    return card;
  }

  AuthAPI.authedGet("/admin/institution-requests", session.access_token).then(function (res) {
    if (res.status === 401) { expired(); return; }
    if (res.status === 403) { return; } // admin.js sends non-admins away
    if (!res.ok) {
      show(res.data.error || "Could not load institution requests.", "err");
      return;
    }
    show("");
    (res.data.requests || []).forEach(function (request) {
      list.appendChild(renderCard(request));
    });
    updateCount();
  }).catch(function () {
    show("Could not reach the server.", "err");
  });
})();
